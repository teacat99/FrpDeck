package remoteops_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/remoteops"
	"github.com/teacat99/FrpDeck/internal/runtime"
	"github.com/teacat99/FrpDeck/internal/store"
)

// newTestService spins up a fresh sqlite-backed Service inside a
// temp dir, seeds the admin row, and returns the service handle
// + a freshly-created endpoint id the caller can use as the
// stcp transit. Failures are fatal: a half-built service has
// no useful surface for the test to assert against.
func newTestService(t *testing.T) (*remoteops.Service, *store.Store, uint, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := st.SeedAdminIfEmpty("admin", "passwd"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := &config.Config{
		Listen:       ":18080",
		AuthMode:     config.AuthModePassword,
		JWTSecret:    "test-secret-key-not-used-in-prod-32b",
		InstanceName: "test-node",
		DataDir:      dir,
	}
	rt := runtime.New(cfg)
	athn := auth.New(cfg, rt, st)
	drv := frpcd.NewMock()
	svc := remoteops.New(cfg, st, athn, drv)

	ep := &model.Endpoint{
		Name:    "transit",
		Addr:    "frps.example.com",
		Port:    7000,
		User:    "alice",
		Token:   "sekret",
		Enabled: true,
	}
	if err := st.CreateEndpoint(ep); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	return svc, st, ep.ID, cfg
}

func TestCreateInvitationHappyPath(t *testing.T) {
	svc, st, epID, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := svc.CreateInvitation(ctx, remoteops.CreateInviteArgs{
		EndpointID: epID,
		NodeName:   "laptop",
		UIScheme:   "http",
		Actor:      remoteops.Actor{Username: "test", IP: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Node == nil || res.Node.ID == 0 {
		t.Fatalf("expected non-zero node id, got %+v", res.Node)
	}
	if res.Invitation == "" {
		t.Fatalf("expected encoded invitation")
	}
	if res.MgmtToken == "" {
		t.Fatalf("expected mgmt_token in result")
	}
	if res.TunnelID == 0 {
		t.Fatalf("expected backing tunnel id")
	}
	// Persisted row should match the returned shape and carry the
	// pending status (B has not redeemed yet).
	persisted, err := st.GetRemoteNode(res.Node.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read node: %v / %v", err, persisted)
	}
	if persisted.Status != model.RemoteNodeStatusPending {
		t.Fatalf("status = %s, want pending", persisted.Status)
	}
	if persisted.TunnelID != res.TunnelID {
		t.Fatalf("node.TunnelID = %d, want %d", persisted.TunnelID, res.TunnelID)
	}
	if persisted.Direction != model.RemoteDirectionManagesMe {
		t.Fatalf("direction = %s, want manages_me", persisted.Direction)
	}
	tunnel, err := st.GetTunnel(res.TunnelID)
	if err != nil || tunnel == nil {
		t.Fatalf("re-read tunnel: %v / %v", err, tunnel)
	}
	if tunnel.Source != model.TunnelSourceRemoteMgmt {
		t.Fatalf("tunnel source = %s, want %s", tunnel.Source, model.TunnelSourceRemoteMgmt)
	}
	if tunnel.Type != "stcp" || tunnel.Role != "server" {
		t.Fatalf("tunnel shape unexpected: type=%s role=%s", tunnel.Type, tunnel.Role)
	}
}

func TestCreateInvitationRequiresEndpoint(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	_, err := svc.CreateInvitation(context.Background(), remoteops.CreateInviteArgs{})
	if err == nil || !strings.Contains(err.Error(), "endpoint_id") {
		t.Fatalf("expected endpoint_id required, got %v", err)
	}
}

func TestCreateInvitationRejectsBadScheme(t *testing.T) {
	svc, _, epID, _ := newTestService(t)
	_, err := svc.CreateInvitation(context.Background(), remoteops.CreateInviteArgs{
		EndpointID: epID,
		UIScheme:   "ftp",
	})
	if err == nil || !strings.Contains(err.Error(), "ui_scheme") {
		t.Fatalf("expected ui_scheme rejection, got %v", err)
	}
}

func TestRefreshInvitationRotatesTokenAndKeepsTunnel(t *testing.T) {
	svc, st, epID, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := svc.CreateInvitation(ctx, remoteops.CreateInviteArgs{EndpointID: epID, NodeName: "laptop"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	originalSK := first.Node.SK
	originalJTI := first.Node.MgmtTokenJTI
	originalTunnelID := first.TunnelID

	second, err := svc.RefreshInvitation(ctx, remoteops.RefreshInviteArgs{NodeID: first.Node.ID})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.Node.ID != first.Node.ID {
		t.Fatalf("refresh changed node id: got %d, want %d", second.Node.ID, first.Node.ID)
	}
	if second.TunnelID != originalTunnelID {
		t.Fatalf("refresh changed tunnel id: got %d, want %d", second.TunnelID, originalTunnelID)
	}
	if second.Node.SK == originalSK {
		t.Fatalf("expected SK rotation; both halves had %q", originalSK)
	}
	if second.Node.MgmtTokenJTI == originalJTI {
		t.Fatalf("expected JTI rotation")
	}
	tunnel, err := st.GetTunnel(originalTunnelID)
	if err != nil || tunnel == nil {
		t.Fatalf("tunnel re-read: %v / %v", err, tunnel)
	}
	if tunnel.SK != second.Node.SK {
		t.Fatalf("tunnel SK %q != node SK %q", tunnel.SK, second.Node.SK)
	}
}

func TestRefreshRejectsRevoked(t *testing.T) {
	svc, _, epID, _ := newTestService(t)
	ctx := context.Background()
	first, err := svc.CreateInvitation(ctx, remoteops.CreateInviteArgs{EndpointID: epID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.RevokeRemoteNode(ctx, remoteops.NodeArgs{NodeID: first.Node.ID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.RefreshInvitation(ctx, remoteops.RefreshInviteArgs{NodeID: first.Node.ID}); err == nil {
		t.Fatalf("expected refresh to refuse revoked pairing")
	}
}

func TestRevokeMgmtTokenClearsJTI(t *testing.T) {
	svc, st, epID, _ := newTestService(t)
	ctx := context.Background()
	first, err := svc.CreateInvitation(ctx, remoteops.CreateInviteArgs{EndpointID: epID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.Node.MgmtTokenJTI == "" {
		t.Fatalf("expected non-empty initial JTI")
	}
	updated, err := svc.RevokeMgmtToken(ctx, remoteops.NodeArgs{NodeID: first.Node.ID})
	if err != nil {
		t.Fatalf("revoke-token: %v", err)
	}
	if updated.MgmtTokenJTI != "" {
		t.Fatalf("expected JTI cleared, got %q", updated.MgmtTokenJTI)
	}
	if updated.AuthToken != "" {
		t.Fatalf("expected mgmt token cleared, got %q", updated.AuthToken)
	}
	// Idempotent second call returns the row unchanged.
	again, err := svc.RevokeMgmtToken(ctx, remoteops.NodeArgs{NodeID: first.Node.ID})
	if err != nil {
		t.Fatalf("revoke-token (idempotent): %v", err)
	}
	if again.ID != updated.ID {
		t.Fatalf("idempotent call returned different row")
	}
	// Persisted row should match.
	read, err := st.GetRemoteNode(first.Node.ID)
	if err != nil || read == nil {
		t.Fatalf("re-read: %v / %v", err, read)
	}
	if read.MgmtTokenJTI != "" {
		t.Fatalf("persisted JTI = %q, want empty", read.MgmtTokenJTI)
	}
}

func TestRevokeRemoteNodeDeletesTunnel(t *testing.T) {
	svc, st, epID, _ := newTestService(t)
	ctx := context.Background()
	first, err := svc.CreateInvitation(ctx, remoteops.CreateInviteArgs{EndpointID: epID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.RevokeRemoteNode(ctx, remoteops.NodeArgs{NodeID: first.Node.ID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	read, err := st.GetRemoteNode(first.Node.ID)
	if err != nil || read == nil {
		t.Fatalf("node disappeared: %v / %v", err, read)
	}
	if read.Status != model.RemoteNodeStatusRevoked {
		t.Fatalf("status = %s, want revoked", read.Status)
	}
	tunnel, err := st.GetTunnel(first.TunnelID)
	if err != nil {
		t.Fatalf("tunnel lookup: %v", err)
	}
	if tunnel != nil {
		t.Fatalf("expected backing tunnel deleted, got %+v", tunnel)
	}
}
