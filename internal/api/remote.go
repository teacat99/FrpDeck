package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/remotemgmt"
	"github.com/teacat99/FrpDeck/internal/remoteops"
)

// AuthModeRequiredCode is the stable error code returned when a remote
// management endpoint is hit while the server runs in a non-password
// auth mode. The frontend uses this to grey the entrypoint and surface
// the precondition.
const AuthModeRequiredCode = "auth_mode_required"

// remoteCreateInviteReq is the payload accepted by /api/remote/invitations.
//
// The operator picks an existing endpoint that will play the role of
// "transit frps"; we generate the rest (sk, mgmt_token, RemoteNode row,
// stcp tunnel) so a successful POST is a one-stop op.
type remoteCreateInviteReq struct {
	EndpointID uint   `json:"endpoint_id"`
	NodeName   string `json:"node_name"`
	UIScheme   string `json:"ui_scheme,omitempty"`
}

// remoteRedeemInviteReq is the body for /api/remote/redeem (B side, paste
// invitation).
type remoteRedeemInviteReq struct {
	Invitation string `json:"invitation"`
	NodeName   string `json:"node_name,omitempty"`
}

// remoteRedeemTokenReq is the body for /api/auth/remote-redeem (mgmt
// token exchange — A side, no auth header required because the token
// itself is the credential).
type remoteRedeemTokenReq struct {
	MgmtToken string `json:"mgmt_token"`
}

// remoteAuthGuard rejects every /api/remote/* call when the server runs
// in a mode where mgmt_token semantics make no sense (none) or where a
// network operator could trivially forge identity (ipwhitelist with
// CIDR overlap). See plan.md §11 "P5-A 强制 password 模式".
func (s *Server) remoteAuthGuard(c *gin.Context) bool {
	if s.cfg.AuthMode == config.AuthModePassword {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"code":  AuthModeRequiredCode,
		"error": "remote management requires password auth mode",
	})
	return false
}

// handleListRemoteNodes returns every pairing row (both directions) so
// the frontend can split into two tabs client side.
func (s *Server) handleListRemoteNodes(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	rows, err := s.store.ListRemoteNodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": rows})
}

// handleCreateInvitation generates an invitation that lets a peer
// FrpDeck dial back into ours. The full side-effect list (auto stcp
// tunnel, RemoteNode row, mgmt_token, driver push) lives in
// internal/remoteops/remoteops.go (Service.CreateInvitation); this
// handler only handles request parsing, auth, and JSON rendering.
func (s *Server) handleCreateInvitation(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	var req remoteCreateInviteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid, actor, _ := auth.Principal(c)
	res, err := s.remoteSvc.CreateInvitation(c.Request.Context(), remoteops.CreateInviteArgs{
		EndpointID: req.EndpointID,
		NodeName:   req.NodeName,
		UIScheme:   req.UIScheme,
		Actor: remoteops.Actor{
			UserID:   uid,
			Username: actor,
			IP:       s.clientIP(c),
		},
	})
	if err != nil {
		writeRemoteOpsError(c, err)
		return
	}
	resp := gin.H{
		"node":       res.Node,
		"invitation": res.Invitation,
		"expire_at":  res.ExpireAt,
		"mgmt_token": res.MgmtToken,
		"tunnel_id":  res.TunnelID,
	}
	if res.DriverWarning != "" {
		resp["driver_warning"] = res.DriverWarning
	}
	c.JSON(http.StatusOK, resp)
}

// handleRefreshInvitation regenerates the invitation for an existing
// `manages_me` RemoteNode without tearing down the underlying tunnel
// pairing. Full preconditions + side-effects in
// internal/remoteops/remoteops.go (Service.RefreshInvitation).
func (s *Server) handleRefreshInvitation(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid, actor, _ := auth.Principal(c)
	res, err := s.remoteSvc.RefreshInvitation(c.Request.Context(), remoteops.RefreshInviteArgs{
		NodeID:   id,
		UIScheme: c.Query("ui_scheme"),
		Actor: remoteops.Actor{
			UserID:   uid,
			Username: actor,
			IP:       s.clientIP(c),
		},
	})
	if err != nil {
		writeRemoteOpsError(c, err)
		return
	}
	resp := gin.H{
		"node":       res.Node,
		"invitation": res.Invitation,
		"expire_at":  res.ExpireAt,
		"mgmt_token": res.MgmtToken,
		"tunnel_id":  res.TunnelID,
	}
	if res.DriverWarning != "" {
		resp["driver_warning"] = res.DriverWarning
	}
	c.JSON(http.StatusOK, resp)
}

// handleRedeemInvitation parses an invitation pasted on B side and wires
// the local FrpDeck to the peer:
//
//   - reuses an existing Endpoint with matching addr/port if present,
//     else auto-creates one from the invitation,
//   - allocates a fresh local bind port (loopback, ephemeral range),
//   - creates a stcp visitor tunnel pointing at the peer's UI,
//   - persists a RemoteNode row in `managed_by_me` direction.
//
// Returns the new RemoteNode + a redeem URL the operator can hit
// directly (the frontend wraps this into a window.open call).
func (s *Server) handleRedeemInvitation(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	var req remoteRedeemInviteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inv, err := remotemgmt.Decode(strings.TrimSpace(req.Invitation))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if inv.Expired(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invitation has expired"})
		return
	}

	ep, err := s.findOrCreateEndpointFromInvite(inv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	bindPort, err := allocateLocalBindPort(s.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	uid, actor, _ := auth.Principal(c)
	nodeName := strings.TrimSpace(req.NodeName)
	if nodeName == "" {
		nodeName = inv.NodeName
	}
	if nodeName == "" {
		nodeName = fmt.Sprintf("remote-%s", inv.Addr)
	}

	// The visitor's ServerName must literally match the proxy name on
	// A. The invitation carries it in ServerProxyName so we don't have
	// to leak A's RemoteNode id through some other channel.
	visitor := &model.Tunnel{
		EndpointID:  ep.ID,
		Name:        fmt.Sprintf("frpdeck-mgmt-visitor-%d", time.Now().Unix()),
		Type:        "stcp",
		Role:        "visitor",
		LocalIP:     "127.0.0.1",
		LocalPort:   bindPort,
		SK:          inv.Sk,
		ServerName:  inv.ProxyName(),
		ServerUser:  inv.RemoteUser,
		Encryption:  true,
		Compression: true,
		Status:      model.StatusPending,
		Source:      model.TunnelSourceRemoteMgmt,
		Enabled:     true,
		AutoStart:   true,
		CreatedBy:   uid,
	}
	if err := s.store.CreateTunnel(visitor); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	node := &model.RemoteNode{
		Name:          nodeName,
		Direction:     model.RemoteDirectionManagedByMe,
		EndpointID:    ep.ID,
		TunnelID:      visitor.ID,
		RemoteUser:    inv.RemoteUser,
		SK:            inv.Sk,
		LocalBindPort: bindPort,
		AuthToken:     inv.MgmtToken,
		InviteExpiry:  pointerTime(inv.ExpireAt),
		Status:        model.RemoteNodeStatusActive,
		LastSeen:      &now,
	}
	if err := s.store.CreateRemoteNode(node); err != nil {
		_ = s.store.DeleteTunnel(visitor.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	driverWarning := ""
	if ep.Enabled {
		if err := s.pushTunnelToDriver(visitor); err != nil {
			driverWarning = err.Error()
		} else {
			visitor.Status = model.StatusActive
			visitor.LastStartAt = &now
			_ = s.store.UpdateTunnel(visitor)
		}
	}

	scheme := strings.ToLower(strings.TrimSpace(inv.UIScheme))
	if scheme == "" {
		scheme = "http"
	}
	redeemURL := fmt.Sprintf("%s://127.0.0.1:%d/?_redeem=%s", scheme, bindPort, inv.MgmtToken)

	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "redeem_remote_invitation", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("node=%d peer=%s:%d", node.ID, inv.Addr, inv.Port),
	})

	resp := gin.H{
		"node":       node,
		"redeem_url": redeemURL,
		"endpoint":   ep,
	}
	if driverWarning != "" {
		resp["driver_warning"] = driverWarning
	}
	c.JSON(http.StatusOK, resp)
}

// handleRevokeMgmtToken voids the currently-issued mgmt_token without
// tearing down the underlying stcp pairing. Full rationale + the
// "why we don't rotate SK" decision in
// internal/remoteops/remoteops.go (Service.RevokeMgmtToken).
func (s *Server) handleRevokeMgmtToken(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid, actor, _ := auth.Principal(c)
	node, err := s.remoteSvc.RevokeMgmtToken(c.Request.Context(), remoteops.NodeArgs{
		NodeID: id,
		Actor: remoteops.Actor{
			UserID:   uid,
			Username: actor,
			IP:       s.clientIP(c),
		},
	})
	if err != nil {
		writeRemoteOpsError(c, err)
		return
	}
	c.JSON(http.StatusOK, node)
}

// handleRevokeRemoteNode tears down a pairing. Implementation lives in
// internal/remoteops/remoteops.go (Service.RevokeRemoteNode); the two
// directions (manages_me / managed_by_me) share a single code path.
func (s *Server) handleRevokeRemoteNode(c *gin.Context) {
	if !s.remoteAuthGuard(c) {
		return
	}
	if !s.ensureAdmin(c) {
		return
	}
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid, actor, _ := auth.Principal(c)
	node, err := s.remoteSvc.RevokeRemoteNode(c.Request.Context(), remoteops.NodeArgs{
		NodeID: id,
		Actor: remoteops.Actor{
			UserID:   uid,
			Username: actor,
			IP:       s.clientIP(c),
		},
	})
	if err != nil {
		writeRemoteOpsError(c, err)
		return
	}
	c.JSON(http.StatusOK, node)
}

// writeRemoteOpsError maps a Service-side error to an HTTP status
// code. The service's well-known sentinels (validation, not-found,
// state mismatches) map to 400/404; everything else falls through
// to 500. We match on substrings rather than typed errors because
// the service's surface is small enough that error strings are
// stable + the alternative (an enumerated error type per case)
// would push concerns into the service that only matter to HTTP.
func writeRemoteOpsError(c *gin.Context, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "endpoint_id required"),
		strings.Contains(msg, "ui_scheme must be"),
		strings.Contains(msg, "only manages_me nodes"),
		strings.Contains(msg, "revoked pairing cannot be refreshed"),
		strings.Contains(msg, "pairing already revoked"),
		strings.Contains(msg, "endpoint not found"):
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
	case strings.Contains(msg, "remote node not found"):
		c.JSON(http.StatusNotFound, gin.H{"error": msg})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
	}
}

// handleRemoteRedeemToken is mounted on the public group: this path is
// the very first call B's browser issues to A through the stcp tunnel,
// so the regular auth middleware (Authorization header) cannot front it.
// The mgmt_token in the body is the credential.
//
// On success the path issues a regular session JWT keyed to the user
// the mgmt_token references; the frontend stores it like a normal
// password login result. The RemoteNode on A's side is flipped to
// `active` and last_seen is bumped.
func (s *Server) handleRemoteRedeemToken(c *gin.Context) {
	if s.cfg.AuthMode != config.AuthModePassword {
		c.JSON(http.StatusForbidden, gin.H{"code": AuthModeRequiredCode, "error": "remote management requires password auth mode"})
		return
	}
	var req remoteRedeemTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	claims := s.auth.ValidateMgmtToken(strings.TrimSpace(req.MgmtToken))
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired mgmt token"})
		return
	}
	node, err := s.store.GetRemoteNode(claims.NodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if node == nil || node.Direction != model.RemoteDirectionManagesMe {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "remote pairing not found"})
		return
	}
	if node.Status == model.RemoteNodeStatusRevoked || node.Status == model.RemoteNodeStatusExpired {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "remote pairing not active"})
		return
	}
	// Only the JTI currently on record is accepted. Empty means the
	// operator just called /revoke-token to invalidate every outstanding
	// mgmt_token without tearing down the pairing — refuse those too.
	// (handleCreateInvitation always sets a non-empty JTI when it mints
	// the row, so the only empty-JTI state on a `manages_me` row is the
	// explicit revoke-token outcome.)
	if node.MgmtTokenJTI == "" || node.MgmtTokenJTI != claims.JTI {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "mgmt token does not match this pairing"})
		return
	}
	tok, err := s.auth.IssueAccessToken(claims.Actor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	node.Status = model.RemoteNodeStatusActive
	node.LastSeen = &now
	_ = s.store.UpdateRemoteNode(node)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "remote_redeem", Actor: claims.Actor.Username, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("node=%d", node.ID),
	})
	c.JSON(http.StatusOK, gin.H{
		"token":    tok,
		"username": claims.Actor.Username,
		"role":     claims.Actor.Role,
		"node":     node,
	})
}

// allocateLocalBindPort finds an unused TCP port in the
// 9201-9499 ephemeral range we reserve for visitor bindings. Used by
// the redeem path so several remote nodes can coexist without collision.
func allocateLocalBindPort(store interface {
	ListRemoteNodes() ([]model.RemoteNode, error)
}) (int, error) {
	taken := map[int]bool{}
	if store != nil {
		nodes, err := store.ListRemoteNodes()
		if err == nil {
			for _, n := range nodes {
				if n.LocalBindPort > 0 {
					taken[n.LocalBindPort] = true
				}
			}
		}
	}
	for port := 9201; port <= 9499; port++ {
		if taken[port] {
			continue
		}
		if portFree(port) {
			return port, nil
		}
	}
	return 0, errors.New("no free port in 9201-9499 for remote bind")
}

func portFree(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func (s *Server) findOrCreateEndpointFromInvite(inv *remotemgmt.Invitation) (*model.Endpoint, error) {
	rows, err := s.store.ListEndpoints()
	if err != nil {
		return nil, err
	}
	for i := range rows {
		ep := &rows[i]
		if ep.Addr == inv.Addr && ep.Port == inv.Port && ep.User == inv.FrpsUser {
			return ep, nil
		}
	}
	ep := &model.Endpoint{
		Name:      fmt.Sprintf("remote-%s-%d", inv.Addr, inv.Port),
		Addr:      inv.Addr,
		Port:      inv.Port,
		Protocol:  inv.Protocol,
		Token:     inv.FrpsToken,
		User:      inv.FrpsUser,
		TLSEnable: inv.TLSEnable,
		Enabled:   true,
		AutoStart: true,
	}
	if err := s.store.CreateEndpoint(ep); err != nil {
		return nil, err
	}
	return ep, nil
}

func pointerTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
