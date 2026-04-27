package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/remotemgmt"
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
// FrpDeck dial back into ours. Side-effects:
//
//   - creates a fresh stcp server-role tunnel pointed at our own
//     web UI port (parsed from cfg.Listen),
//   - persists a RemoteNode row in `manages_me` direction with status
//     `pending` so the redemption path can find and validate it,
//   - pushes the new tunnel to the driver if its endpoint is enabled
//     (best-effort, falls back to a driver_warning so the operator can
//     still copy the invitation and start the endpoint manually).
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
	if req.EndpointID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint_id required"})
		return
	}
	ep, err := s.store.GetEndpoint(req.EndpointID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ep == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint not found"})
		return
	}
	uiPort, err := localUIPort(s.cfg.Listen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("cannot determine local UI port: %v", err)})
		return
	}
	uiScheme := strings.ToLower(strings.TrimSpace(req.UIScheme))
	if uiScheme == "" {
		uiScheme = "http"
	}
	if uiScheme != "http" && uiScheme != "https" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ui_scheme must be http or https"})
		return
	}
	uid, actor, _ := auth.Principal(c)
	user, err := s.store.GetUserByID(uid)
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "actor not found"})
		return
	}

	sk, err := randomHex(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	jti, err := randomHex(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	nodeName := strings.TrimSpace(req.NodeName)
	if nodeName == "" {
		nodeName = fallbackNodeName(s.cfg)
	}

	now := time.Now()
	node := &model.RemoteNode{
		Name:         nodeName,
		Direction:    model.RemoteDirectionManagesMe,
		EndpointID:   ep.ID,
		RemoteUser:   ep.User,
		SK:           sk,
		MgmtTokenJTI: jti,
		Status:       model.RemoteNodeStatusPending,
	}
	expireAt := now.Add(remotemgmt.InvitationTTL)
	node.InviteExpiry = &expireAt
	if err := s.store.CreateRemoteNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	mgmtToken, err := s.auth.IssueMgmtToken(user, node.ID, remotemgmt.MgmtTokenTTL, jti)
	if err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	node.AuthToken = mgmtToken
	if err := s.store.UpdateRemoteNode(node); err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	proxyName := fmt.Sprintf("frpdeck-mgmt-%d", node.ID)
	tunnel := &model.Tunnel{
		EndpointID:  ep.ID,
		Name:        proxyName,
		Type:        "stcp",
		Role:        "server",
		LocalIP:     "127.0.0.1",
		LocalPort:   uiPort,
		SK:          sk,
		AllowUsers:  "*",
		Encryption:  true,
		Compression: true,
		Status:      model.StatusPending,
		Source:      model.TunnelSourceRemoteMgmt,
		Enabled:     true,
		AutoStart:   true,
		CreatedBy:   uid,
	}
	if err := s.store.CreateTunnel(tunnel); err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create tunnel: %v", err)})
		return
	}
	node.TunnelID = tunnel.ID
	if err := s.store.UpdateRemoteNode(node); err != nil {
		_ = s.store.DeleteTunnel(tunnel.ID)
		_ = s.store.DeleteRemoteNode(node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	driverWarning := ""
	if ep.Enabled {
		if err := s.pushTunnelToDriver(tunnel); err != nil {
			driverWarning = err.Error()
		} else {
			tunnel.Status = model.StatusActive
			startAt := time.Now()
			tunnel.LastStartAt = &startAt
			_ = s.store.UpdateTunnel(tunnel)
		}
	}

	inv := &remotemgmt.Invitation{
		V:               remotemgmt.InvitationVersion,
		NodeName:        nodeName,
		Addr:            ep.Addr,
		Port:            ep.Port,
		Protocol:        ep.Protocol,
		TLSEnable:       ep.TLSEnable,
		FrpsUser:        ep.User,
		FrpsToken:       ep.Token,
		RemoteUser:      ep.User,
		Sk:              sk,
		UIScheme:        uiScheme,
		ServerProxyName: proxyName,
		MgmtToken:       mgmtToken,
		IssuedAt:        now,
		ExpireAt:        expireAt,
	}
	encoded, err := remotemgmt.Encode(inv)
	if err != nil {
		_ = s.store.DeleteTunnel(tunnel.ID)
		_ = s.store.DeleteRemoteNode(node.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "create_remote_invitation", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("node=%d name=%s", node.ID, nodeName),
	})

	resp := gin.H{
		"node":          node,
		"invitation":    encoded,
		"expire_at":     expireAt,
		"mgmt_token":    mgmtToken, // returned ONCE so the operator can debug; not stored client-side
		"tunnel_id":     tunnel.ID,
	}
	if driverWarning != "" {
		resp["driver_warning"] = driverWarning
	}
	c.JSON(http.StatusOK, resp)
}

// handleRefreshInvitation regenerates the invitation for an existing
// `manages_me` RemoteNode without tearing down the underlying tunnel
// pairing. Used when the original invitation expired before the peer
// redeemed it, or when the operator wants to share a fresh QR after
// rotating the SK / mgmt_token. Behaviour:
//
//   - rotates SK + mgmt_token JTI on A's row,
//   - reissues the JWT (5 min default TTL),
//   - updates the auto-created stcp tunnel's SK in DB and replays it
//     into the driver so the visitor only needs the new invitation,
//   - flips status back to `pending` so the reaper does not race the
//     redeem call,
//   - keeps node id stable so the UI list does not reorder.
//
// Refusing on `revoked` rows: a revoked pairing has its tunnel deleted,
// reviving it would require a full create — point operators at the
// "generate invitation" form instead.
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
	node, err := s.store.GetRemoteNode(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "remote node not found"})
		return
	}
	if node.Direction != model.RemoteDirectionManagesMe {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only manages_me nodes can refresh invitations"})
		return
	}
	if node.Status == model.RemoteNodeStatusRevoked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "revoked pairing cannot be refreshed; create a new invitation"})
		return
	}
	if node.TunnelID == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "remote node has no backing tunnel"})
		return
	}
	tunnel, err := s.store.GetTunnel(node.TunnelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tunnel == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backing tunnel disappeared"})
		return
	}
	ep, err := s.store.GetEndpoint(node.EndpointID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ep == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "endpoint disappeared"})
		return
	}
	uid, actor, _ := auth.Principal(c)
	user, err := s.store.GetUserByID(uid)
	if err != nil || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "actor not found"})
		return
	}
	uiPort, err := localUIPort(s.cfg.Listen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("cannot determine local UI port: %v", err)})
		return
	}
	uiScheme := strings.ToLower(strings.TrimSpace(c.Query("ui_scheme")))
	if uiScheme == "" {
		uiScheme = "http"
	}
	if uiScheme != "http" && uiScheme != "https" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ui_scheme must be http or https"})
		return
	}

	sk, err := randomHex(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	jti, err := randomHex(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	expireAt := now.Add(remotemgmt.InvitationTTL)

	mgmtToken, err := s.auth.IssueMgmtToken(user, node.ID, remotemgmt.MgmtTokenTTL, jti)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tunnel.SK = sk
	tunnel.LocalPort = uiPort
	if err := s.store.UpdateTunnel(tunnel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	driverWarning := ""
	if ep.Enabled {
		_ = s.removeTunnelFromDriver(tunnel)
		if err := s.pushTunnelToDriver(tunnel); err != nil {
			driverWarning = err.Error()
			tunnel.Status = model.StatusFailed
		} else {
			tunnel.Status = model.StatusActive
			tunnel.LastStartAt = &now
		}
		_ = s.store.UpdateTunnel(tunnel)
	}

	node.SK = sk
	node.MgmtTokenJTI = jti
	node.AuthToken = mgmtToken
	node.InviteExpiry = &expireAt
	node.Status = model.RemoteNodeStatusPending
	node.LastSeen = nil
	if err := s.store.UpdateRemoteNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	inv := &remotemgmt.Invitation{
		V:               remotemgmt.InvitationVersion,
		NodeName:        node.Name,
		Addr:            ep.Addr,
		Port:            ep.Port,
		Protocol:        ep.Protocol,
		TLSEnable:       ep.TLSEnable,
		FrpsUser:        ep.User,
		FrpsToken:       ep.Token,
		RemoteUser:      ep.User,
		Sk:              sk,
		UIScheme:        uiScheme,
		ServerProxyName: tunnel.Name,
		MgmtToken:       mgmtToken,
		IssuedAt:        now,
		ExpireAt:        expireAt,
	}
	encoded, err := remotemgmt.Encode(inv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "refresh_remote_invitation", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("node=%d name=%s", node.ID, node.Name),
	})
	resp := gin.H{
		"node":       node,
		"invitation": encoded,
		"expire_at":  expireAt,
		"mgmt_token": mgmtToken,
		"tunnel_id":  tunnel.ID,
	}
	if driverWarning != "" {
		resp["driver_warning"] = driverWarning
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
// tearing down the underlying stcp pairing. The use case is "I shared
// the QR with the wrong contact and want them locked out before the
// 24h TTL expires": we clear the on-record JTI so the next
// /api/auth/remote-redeem call from anyone holding the stale token is
// rejected by the JTI mismatch check (see handleRemoteRedeemToken
// L661–664).
//
// The pairing remains usable — generating a new invitation via
// `POST /remote/nodes/:id/refresh` will mint a fresh JTI + mgmt_token
// without touching the existing tunnel SK; the visitor on B side keeps
// working without redeem.
//
// Why we don't also rotate the SK: B side has no mgmt_token to redeem
// once we clear the JTI, so the leak path (someone holding the old
// mgmt_token) is closed regardless of SK rotation. Rotating SK would
// require B to repair the visitor end, which is a far heavier
// operation than what "revoke this leaked token" should cost.
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
	node, err := s.store.GetRemoteNode(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "remote node not found"})
		return
	}
	if node.Direction != model.RemoteDirectionManagesMe {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only manages_me nodes have a mgmt_token to revoke"})
		return
	}
	if node.Status == model.RemoteNodeStatusRevoked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pairing already revoked"})
		return
	}
	if node.MgmtTokenJTI == "" {
		// Already in "no mgmt_token outstanding" state; treat as
		// idempotent success so a UI double-click is harmless.
		c.JSON(http.StatusOK, node)
		return
	}

	previousJTI := node.MgmtTokenJTI
	node.MgmtTokenJTI = ""
	node.AuthToken = ""
	// Drop expiry so the reaper does not flip this row to expired
	// before the operator generates a fresh invitation. The next
	// refresh call resets InviteExpiry anyway.
	node.InviteExpiry = nil
	if err := s.store.UpdateRemoteNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action:  "revoke_mgmt_token",
		Actor:   actor,
		ActorIP: s.clientIP(c),
		Detail:  fmt.Sprintf("node=%d direction=%s prev_jti=%s", node.ID, node.Direction, previousJTI),
	})
	c.JSON(http.StatusOK, node)
}

// handleRevokeRemoteNode tears down a pairing. Both sides perform the
// same operation: stop + delete the auto-created tunnel, mark the
// RemoteNode as revoked (kept for audit), and audit-log the action.
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
	node, err := s.store.GetRemoteNode(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if node == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "remote node not found"})
		return
	}
	if node.TunnelID > 0 {
		if t, err := s.store.GetTunnel(node.TunnelID); err == nil && t != nil {
			_ = s.removeTunnelFromDriver(t)
			_ = s.store.DeleteTunnel(t.ID)
		}
	}
	node.Status = model.RemoteNodeStatusRevoked
	if err := s.store.UpdateRemoteNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, actor, _ := auth.Principal(c)
	_ = s.store.WriteAudit(&model.AuditLog{
		Action: "revoke_remote_node", Actor: actor, ActorIP: s.clientIP(c),
		Detail: fmt.Sprintf("node=%d direction=%s", node.ID, node.Direction),
	})
	c.JSON(http.StatusOK, node)
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

// localUIPort extracts the TCP port FrpDeck listens on from a gin-style
// host[:port] string. Used in invitation generation so the auto-created
// stcp tunnel forwards to the right local port.
func localUIPort(listen string) (int, error) {
	if listen == "" {
		return 0, errors.New("listen address empty")
	}
	host, portStr, err := net.SplitHostPort(listen)
	_ = host
	if err != nil {
		// Fallback: leading colon shorthand like ":8080"
		if strings.HasPrefix(listen, ":") {
			portStr = strings.TrimPrefix(listen, ":")
		} else {
			return 0, fmt.Errorf("listen address %q: %w", listen, err)
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port in listen address %q", listen)
	}
	return port, nil
}

// fallbackNodeName picks a sensible default invitation node name when
// the operator did not provide one. Uses FRPDECK_INSTANCE_NAME when set
// (the env var declared in plan.md §9), else "frpdeck-<host port>".
func fallbackNodeName(cfg *config.Config) string {
	if cfg == nil {
		return "frpdeck-node"
	}
	if name := strings.TrimSpace(cfg.InstanceName); name != "" {
		return name
	}
	if port, err := localUIPort(cfg.Listen); err == nil {
		return fmt.Sprintf("frpdeck-%d", port)
	}
	return "frpdeck-node"
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

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func pointerTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
