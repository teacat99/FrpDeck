// Package remoteops carries the side-effect-bearing core of the P5
// remote-management flow. It exists so two very different transports
// can drive the same business logic without duplicating it:
//
//   - The HTTP API (`internal/api/remote.go`) — gin handlers stay
//     thin: parse the request, run the auth guard, call into Service,
//     render JSON.
//   - The local control socket (`internal/control` + `cmd/cli/...`) —
//     the standalone `frpdeck` CLI calls Service through the daemon's
//     dispatch table when the operator runs `frpdeck remote invite`,
//     so headless / scripted ops do not have to mint a JWT and re-enter
//     the HTTP path just to get a one-shot pairing.
//
// What the package owns:
//   - SQLite mutations on RemoteNode + Tunnel rows
//   - mgmt_token JTI / SK / invitation generation (delegating crypto
//     to internal/auth and packaging to internal/remotemgmt)
//   - driver pushes/removes for the auto-created stcp tunnel — wrapped
//     so the caller never has to re-resolve the endpoint
//   - audit log entries with the actor that triggered the change
//
// What it explicitly does NOT own:
//   - Authentication / authorisation (callers must check permissions
//     before they hand the request to Service — for HTTP this is the
//     gin middleware + remoteAuthGuard; for the control socket it is
//     the OS-level 0600 socket permission)
//   - Output formatting (no gin.H, no json.Marshal — typed result
//     structs only, transport layer renders)
package remoteops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/remotemgmt"
	"github.com/teacat99/FrpDeck/internal/store"
)

// Service is the entry point for all P5 mutating operations. The zero
// value is unusable — always go through New() so missing dependencies
// fail fast at construction rather than mid-flow with a nil deref.
//
// Service is safe for concurrent use: every method only touches the
// underlying SQLite store + driver, both of which serialise access
// internally.
type Service struct {
	cfg    *config.Config
	store  *store.Store
	auth   *auth.Authenticator
	driver frpcd.FrpDriver
}

// New constructs a Service. cfg, st and ath are required; drv may be
// nil for tests that do not exercise the driver push path (the
// service then short-circuits the push step with a no-op).
func New(cfg *config.Config, st *store.Store, ath *auth.Authenticator, drv frpcd.FrpDriver) *Service {
	return &Service{cfg: cfg, store: st, auth: ath, driver: drv}
}

// Actor identifies the principal that triggered a mutation. UserID
// may be 0 — meaning "the caller could not name a user (typically
// CLI direct-DB)" — in which case the service falls back to the
// first active admin. Username and IP are written verbatim into the
// audit log so the trail is honest about which transport drove the
// change.
type Actor struct {
	UserID   uint
	Username string
	IP       string
}

// CreateInviteArgs is the input shape for Service.CreateInvitation.
// EndpointID is mandatory; NodeName defaults to the configured
// instance name; UIScheme defaults to http.
type CreateInviteArgs struct {
	EndpointID uint
	NodeName   string
	UIScheme   string
	Actor      Actor
}

// RefreshInviteArgs is the input shape for Service.RefreshInvitation.
// NodeID is mandatory.
type RefreshInviteArgs struct {
	NodeID   uint
	UIScheme string
	Actor    Actor
}

// NodeArgs is the input shape for the two no-payload mutating calls
// (RevokeMgmtToken / RevokeRemoteNode). Kept as its own type so the
// CLI dispatch table has a single place to look up the wire schema.
type NodeArgs struct {
	NodeID uint
	Actor  Actor
}

// InvitationResult is what callers render back to the operator after
// a successful Create or Refresh. Mirrors the JSON the HTTP API has
// always emitted so the frontend keeps working unchanged.
type InvitationResult struct {
	Node          *model.RemoteNode `json:"node"`
	Invitation    string            `json:"invitation"`
	ExpireAt      time.Time         `json:"expire_at"`
	MgmtToken     string            `json:"mgmt_token"`
	TunnelID      uint              `json:"tunnel_id"`
	DriverWarning string            `json:"driver_warning,omitempty"`
}

// CreateInvitation generates a fresh invitation that lets a peer
// FrpDeck dial back to ours. See the doc comment on
// `internal/api/remote.go` handleCreateInvitation for the side-effect
// list — this method now owns that logic verbatim.
func (s *Service) CreateInvitation(ctx context.Context, args CreateInviteArgs) (*InvitationResult, error) {
	if args.EndpointID == 0 {
		return nil, errors.New("endpoint_id required")
	}
	ep, err := s.store.GetEndpoint(args.EndpointID)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, errors.New("endpoint not found")
	}
	uiPort, err := LocalUIPort(s.cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("cannot determine local UI port: %w", err)
	}
	uiScheme, err := normalizeUIScheme(args.UIScheme)
	if err != nil {
		return nil, err
	}
	user, err := s.resolveActor(args.Actor)
	if err != nil {
		return nil, err
	}

	sk, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	jti, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	nodeName := strings.TrimSpace(args.NodeName)
	if nodeName == "" {
		nodeName = FallbackNodeName(s.cfg)
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
		return nil, err
	}

	mgmtToken, err := s.auth.IssueMgmtToken(user, node.ID, remotemgmt.MgmtTokenTTL, jti)
	if err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		return nil, err
	}
	node.AuthToken = mgmtToken
	if err := s.store.UpdateRemoteNode(node); err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		return nil, err
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
		CreatedBy:   user.ID,
	}
	if err := s.store.CreateTunnel(tunnel); err != nil {
		_ = s.store.DeleteRemoteNode(node.ID)
		return nil, fmt.Errorf("create tunnel: %w", err)
	}
	node.TunnelID = tunnel.ID
	if err := s.store.UpdateRemoteNode(node); err != nil {
		_ = s.store.DeleteTunnel(tunnel.ID)
		_ = s.store.DeleteRemoteNode(node.ID)
		return nil, err
	}

	driverWarning := ""
	if ep.Enabled {
		if err := s.pushTunnelToDriver(ep, tunnel); err != nil {
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
		return nil, err
	}

	s.writeAudit(args.Actor, "create_remote_invitation", fmt.Sprintf("node=%d name=%s", node.ID, nodeName))

	_ = ctx
	return &InvitationResult{
		Node:          node,
		Invitation:    encoded,
		ExpireAt:      expireAt,
		MgmtToken:     mgmtToken,
		TunnelID:      tunnel.ID,
		DriverWarning: driverWarning,
	}, nil
}

// RefreshInvitation regenerates the invitation for an existing
// `manages_me` RemoteNode without tearing down the underlying tunnel
// pairing. See the original handleRefreshInvitation doc comment for
// the precondition matrix.
func (s *Service) RefreshInvitation(ctx context.Context, args RefreshInviteArgs) (*InvitationResult, error) {
	node, err := s.store.GetRemoteNode(args.NodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("remote node not found")
	}
	if node.Direction != model.RemoteDirectionManagesMe {
		return nil, errors.New("only manages_me nodes can refresh invitations")
	}
	if node.Status == model.RemoteNodeStatusRevoked {
		return nil, errors.New("revoked pairing cannot be refreshed; create a new invitation")
	}
	if node.TunnelID == 0 {
		return nil, errors.New("remote node has no backing tunnel")
	}
	tunnel, err := s.store.GetTunnel(node.TunnelID)
	if err != nil {
		return nil, err
	}
	if tunnel == nil {
		return nil, errors.New("backing tunnel disappeared")
	}
	ep, err := s.store.GetEndpoint(node.EndpointID)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, errors.New("endpoint disappeared")
	}
	user, err := s.resolveActor(args.Actor)
	if err != nil {
		return nil, err
	}
	uiPort, err := LocalUIPort(s.cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("cannot determine local UI port: %w", err)
	}
	uiScheme, err := normalizeUIScheme(args.UIScheme)
	if err != nil {
		return nil, err
	}

	sk, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	jti, err := randomHex(16)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expireAt := now.Add(remotemgmt.InvitationTTL)

	mgmtToken, err := s.auth.IssueMgmtToken(user, node.ID, remotemgmt.MgmtTokenTTL, jti)
	if err != nil {
		return nil, err
	}

	tunnel.SK = sk
	tunnel.LocalPort = uiPort
	if err := s.store.UpdateTunnel(tunnel); err != nil {
		return nil, err
	}

	driverWarning := ""
	if ep.Enabled {
		_ = s.removeTunnelFromDriver(ep, tunnel)
		if err := s.pushTunnelToDriver(ep, tunnel); err != nil {
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
		return nil, err
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
		return nil, err
	}

	s.writeAudit(args.Actor, "refresh_remote_invitation", fmt.Sprintf("node=%d name=%s", node.ID, node.Name))

	_ = ctx
	return &InvitationResult{
		Node:          node,
		Invitation:    encoded,
		ExpireAt:      expireAt,
		MgmtToken:     mgmtToken,
		TunnelID:      tunnel.ID,
		DriverWarning: driverWarning,
	}, nil
}

// RevokeMgmtToken voids the currently-issued mgmt_token without
// tearing down the underlying stcp pairing. Idempotent: revoking a
// row that already has an empty JTI returns the row unchanged.
func (s *Service) RevokeMgmtToken(ctx context.Context, args NodeArgs) (*model.RemoteNode, error) {
	node, err := s.store.GetRemoteNode(args.NodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("remote node not found")
	}
	if node.Direction != model.RemoteDirectionManagesMe {
		return nil, errors.New("only manages_me nodes have a mgmt_token to revoke")
	}
	if node.Status == model.RemoteNodeStatusRevoked {
		return nil, errors.New("pairing already revoked")
	}
	if node.MgmtTokenJTI == "" {
		// Idempotent: nothing to revoke.
		return node, nil
	}

	previousJTI := node.MgmtTokenJTI
	node.MgmtTokenJTI = ""
	node.AuthToken = ""
	node.InviteExpiry = nil
	if err := s.store.UpdateRemoteNode(node); err != nil {
		return nil, err
	}

	s.writeAudit(args.Actor, "revoke_mgmt_token", fmt.Sprintf("node=%d direction=%s prev_jti=%s", node.ID, node.Direction, previousJTI))

	_ = ctx
	return node, nil
}

// RevokeRemoteNode tears down a pairing entirely. Behaves the same on
// both directions: stop + delete the auto-created tunnel, mark the
// RemoteNode as revoked, audit-log the action.
func (s *Service) RevokeRemoteNode(ctx context.Context, args NodeArgs) (*model.RemoteNode, error) {
	node, err := s.store.GetRemoteNode(args.NodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("remote node not found")
	}
	if node.TunnelID > 0 {
		if t, err := s.store.GetTunnel(node.TunnelID); err == nil && t != nil {
			if ep, _ := s.store.GetEndpoint(t.EndpointID); ep != nil {
				_ = s.removeTunnelFromDriver(ep, t)
			}
			_ = s.store.DeleteTunnel(t.ID)
		}
	}
	node.Status = model.RemoteNodeStatusRevoked
	if err := s.store.UpdateRemoteNode(node); err != nil {
		return nil, err
	}

	s.writeAudit(args.Actor, "revoke_remote_node", fmt.Sprintf("node=%d direction=%s", node.ID, node.Direction))

	_ = ctx
	return node, nil
}

// resolveActor looks up the user that should sign the mgmt_token. If
// the caller already names a user (HTTP path: extracted from the JWT
// in the request) we trust that. Otherwise (CLI direct-DB path) we
// fall back to the first active admin so unattended scripts still
// work — at the cost of attributing the audit row to whoever happens
// to be admin #1, which is exactly what we want for a CLI-driven op.
func (s *Service) resolveActor(a Actor) (*model.User, error) {
	if a.UserID != 0 {
		u, err := s.store.GetUserByID(a.UserID)
		if err != nil {
			return nil, fmt.Errorf("actor lookup: %w", err)
		}
		if u == nil {
			return nil, errors.New("actor not found")
		}
		return u, nil
	}
	if a.Username != "" {
		u, err := s.store.GetUserByUsername(a.Username)
		if err != nil {
			return nil, fmt.Errorf("actor lookup: %w", err)
		}
		if u != nil {
			return u, nil
		}
	}
	users, err := s.store.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	for i := range users {
		u := &users[i]
		if u.Role == model.RoleAdmin && !u.Disabled {
			return u, nil
		}
	}
	return nil, errors.New("no active admin user available to sign mgmt token")
}

func (s *Service) writeAudit(a Actor, action, detail string) {
	actorName := a.Username
	if actorName == "" {
		actorName = "cli"
	}
	_ = s.store.WriteAudit(&model.AuditLog{
		Action:  action,
		Actor:   actorName,
		ActorIP: a.IP,
		Detail:  detail,
	})
}

func (s *Service) pushTunnelToDriver(ep *model.Endpoint, t *model.Tunnel) error {
	if s.driver == nil {
		return errors.New("driver unavailable")
	}
	if !ep.Enabled {
		return errors.New("endpoint disabled")
	}
	return s.driver.AddTunnel(ep, t)
}

func (s *Service) removeTunnelFromDriver(ep *model.Endpoint, t *model.Tunnel) error {
	if s.driver == nil {
		return nil
	}
	return s.driver.RemoveTunnel(ep, t)
}

// LocalUIPort extracts the TCP port FrpDeck listens on from a
// gin-style host[:port] string. Exported so the HTTP layer can
// reuse the same parser in its own legacy helper without forking.
func LocalUIPort(listen string) (int, error) {
	if listen == "" {
		return 0, errors.New("listen address empty")
	}
	host, portStr, err := net.SplitHostPort(listen)
	_ = host
	if err != nil {
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

// FallbackNodeName picks a sensible default invitation node name when
// the operator did not provide one. Uses cfg.InstanceName when set,
// else "frpdeck-<UI-port>".
func FallbackNodeName(cfg *config.Config) string {
	if cfg == nil {
		return "frpdeck-node"
	}
	if name := strings.TrimSpace(cfg.InstanceName); name != "" {
		return name
	}
	if port, err := LocalUIPort(cfg.Listen); err == nil {
		return fmt.Sprintf("frpdeck-%d", port)
	}
	return "frpdeck-node"
}

func normalizeUIScheme(raw string) (string, error) {
	scheme := strings.ToLower(strings.TrimSpace(raw))
	if scheme == "" {
		return "http", nil
	}
	if scheme != "http" && scheme != "https" {
		return "", errors.New("ui_scheme must be http or https")
	}
	return scheme, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
