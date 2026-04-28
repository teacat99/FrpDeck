package cmds

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// newSeededStore creates a fresh SQLite store under t.TempDir() and
// seeds two endpoints + three tunnels covering the disambiguation
// edge cases the resolvers care about.
func newSeededStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "frpdeck.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	now := time.Now()
	mustCreateEndpoint(t, st, &model.Endpoint{Name: "nas", Addr: "nas.example.com", Port: 7000, DriverMode: model.DriverModeEmbedded, Enabled: true, AutoStart: true, CreatedAt: now, UpdatedAt: now})
	mustCreateEndpoint(t, st, &model.Endpoint{Name: "office", Addr: "office.example.com", Port: 7000, DriverMode: model.DriverModeEmbedded, Enabled: true, AutoStart: true, CreatedAt: now, UpdatedAt: now})
	mustCreateTunnel(t, st, &model.Tunnel{EndpointID: 1, Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 22022, Status: model.StatusPending, Enabled: true, CreatedAt: now, UpdatedAt: now})
	mustCreateTunnel(t, st, &model.Tunnel{EndpointID: 2, Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 22023, Status: model.StatusPending, Enabled: true, CreatedAt: now, UpdatedAt: now})
	mustCreateTunnel(t, st, &model.Tunnel{EndpointID: 1, Name: "rdp", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 3389, RemotePort: 33389, Status: model.StatusPending, Enabled: true, CreatedAt: now, UpdatedAt: now})
	return st
}

func mustCreateEndpoint(t *testing.T, st *store.Store, e *model.Endpoint) {
	t.Helper()
	if err := st.CreateEndpoint(e); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
}

func mustCreateTunnel(t *testing.T, st *store.Store, tu *model.Tunnel) {
	t.Helper()
	if err := st.CreateTunnel(tu); err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
}

func TestResolveEndpoint_ByID(t *testing.T) {
	st := newSeededStore(t)
	ep, err := resolveEndpoint(st, "1")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if ep.Name != "nas" {
		t.Errorf("got %q, want nas", ep.Name)
	}
}

func TestResolveEndpoint_ByName(t *testing.T) {
	st := newSeededStore(t)
	ep, err := resolveEndpoint(st, "Office")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if ep.ID != 2 {
		t.Errorf("got id=%d, want 2", ep.ID)
	}
}

func TestResolveEndpoint_NotFound(t *testing.T) {
	st := newSeededStore(t)
	if _, err := resolveEndpoint(st, "ghost"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, err := resolveEndpoint(st, "999"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveTunnel_ByID(t *testing.T) {
	st := newSeededStore(t)
	tn, err := resolveTunnel(st, "3")
	if err != nil {
		t.Fatalf("resolveTunnel: %v", err)
	}
	if tn.Name != "rdp" {
		t.Errorf("got %q, want rdp", tn.Name)
	}
}

func TestResolveTunnel_AmbiguousNameRejected(t *testing.T) {
	st := newSeededStore(t)
	if _, err := resolveTunnel(st, "ssh"); err == nil {
		t.Fatal("expected ambiguity error, got nil")
	}
}

func TestResolveTunnel_DisambiguatedByEndpoint(t *testing.T) {
	st := newSeededStore(t)
	tn, err := resolveTunnel(st, "office/ssh")
	if err != nil {
		t.Fatalf("resolveTunnel: %v", err)
	}
	if tn.EndpointID != 2 {
		t.Errorf("got endpoint=%d, want 2", tn.EndpointID)
	}
}

func TestResolveTunnel_DisambiguatedByEndpointID(t *testing.T) {
	st := newSeededStore(t)
	tn, err := resolveTunnel(st, "1/ssh")
	if err != nil {
		t.Fatalf("resolveTunnel: %v", err)
	}
	if tn.EndpointID != 1 || tn.Name != "ssh" {
		t.Errorf("got endpoint=%d name=%s, want endpoint=1 name=ssh", tn.EndpointID, tn.Name)
	}
}

func TestResolveProfile_NotFound(t *testing.T) {
	st := newSeededStore(t)
	if _, err := resolveProfile(st, "ghost"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveProfile_ByName(t *testing.T) {
	st := newSeededStore(t)
	now := time.Now()
	if err := st.CreateProfile(&model.Profile{Name: "home", CreatedAt: now, UpdatedAt: now}, nil); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	p, err := resolveProfile(st, "home")
	if err != nil {
		t.Fatalf("resolveProfile: %v", err)
	}
	if p.ID != 1 {
		t.Errorf("got id=%d, want 1", p.ID)
	}
}
