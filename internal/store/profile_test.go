// Profile + ProfileBinding store coverage (P8-C). The fixtures are
// minimal — what we care about is that ActivateProfile flips the
// Endpoint.Enabled / Tunnel.Enabled toggles correctly and the
// single-active invariant holds across overlapping create/activate
// calls.

package store

import (
	"path/filepath"
	"testing"

	"github.com/teacat99/FrpDeck/internal/model"
)

func newProfileTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "profile-test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func seedEndpointTunnels(t *testing.T, s *Store) (uint, uint, uint, uint) {
	t.Helper()
	ep1 := &model.Endpoint{Name: "ep1", Addr: "127.0.0.1", Port: 7000, Enabled: true}
	ep2 := &model.Endpoint{Name: "ep2", Addr: "127.0.0.1", Port: 7001, Enabled: true}
	if err := s.CreateEndpoint(ep1); err != nil {
		t.Fatalf("CreateEndpoint ep1: %v", err)
	}
	if err := s.CreateEndpoint(ep2); err != nil {
		t.Fatalf("CreateEndpoint ep2: %v", err)
	}
	t1 := &model.Tunnel{EndpointID: ep1.ID, Name: "t1", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 80, Enabled: true, Status: model.StatusPending}
	t2 := &model.Tunnel{EndpointID: ep2.ID, Name: "t2", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 81, Enabled: true, Status: model.StatusPending}
	if err := s.CreateTunnel(t1); err != nil {
		t.Fatalf("CreateTunnel t1: %v", err)
	}
	if err := s.CreateTunnel(t2); err != nil {
		t.Fatalf("CreateTunnel t2: %v", err)
	}
	return ep1.ID, ep2.ID, t1.ID, t2.ID
}

func TestProfile_CreateGetDelete(t *testing.T) {
	s := newProfileTestStore(t)
	ep1, _, t1, _ := seedEndpointTunnels(t, s)

	p := &model.Profile{Name: "home"}
	bindings := []model.ProfileBinding{{EndpointID: ep1, TunnelID: t1}}
	if err := s.CreateProfile(p, bindings); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if p.ID == 0 {
		t.Fatalf("profile id not assigned")
	}
	got, err := s.GetProfile(p.ID)
	if err != nil || got == nil {
		t.Fatalf("GetProfile err=%v got=%v", err, got)
	}
	if got.Name != "home" {
		t.Errorf("name=%q want home", got.Name)
	}
	rows, err := s.ListProfileBindings(p.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListProfileBindings rows=%d err=%v", len(rows), err)
	}
	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	rows, _ = s.ListProfileBindings(p.ID)
	if len(rows) != 0 {
		t.Errorf("bindings should be wiped after delete, got %d", len(rows))
	}
}

func TestProfile_ActivateAppliesEnabled(t *testing.T) {
	s := newProfileTestStore(t)
	ep1, ep2, t1, t2 := seedEndpointTunnels(t, s)

	// Profile A: only ep1 + t1 (so t2 must be disabled, ep2 disabled).
	pa := &model.Profile{Name: "A"}
	if err := s.CreateProfile(pa, []model.ProfileBinding{{EndpointID: ep1, TunnelID: t1}}); err != nil {
		t.Fatalf("CreateProfile A: %v", err)
	}
	if _, err := s.ActivateProfile(pa.ID); err != nil {
		t.Fatalf("ActivateProfile A: %v", err)
	}

	gotEp1, _ := s.GetEndpoint(ep1)
	gotEp2, _ := s.GetEndpoint(ep2)
	gotT1, _ := s.GetTunnel(t1)
	gotT2, _ := s.GetTunnel(t2)
	if !gotEp1.Enabled {
		t.Errorf("ep1 should be enabled")
	}
	if gotEp2.Enabled {
		t.Errorf("ep2 should be disabled")
	}
	if !gotT1.Enabled {
		t.Errorf("t1 should be enabled")
	}
	if gotT2.Enabled {
		t.Errorf("t2 should be disabled")
	}

	// Profile B: ep2 wildcarded → t2 enabled even though it isn't
	// listed explicitly.
	pb := &model.Profile{Name: "B"}
	if err := s.CreateProfile(pb, []model.ProfileBinding{{EndpointID: ep2}}); err != nil {
		t.Fatalf("CreateProfile B: %v", err)
	}
	if _, err := s.ActivateProfile(pb.ID); err != nil {
		t.Fatalf("ActivateProfile B: %v", err)
	}
	gotEp1, _ = s.GetEndpoint(ep1)
	gotEp2, _ = s.GetEndpoint(ep2)
	gotT1, _ = s.GetTunnel(t1)
	gotT2, _ = s.GetTunnel(t2)
	if gotEp1.Enabled {
		t.Errorf("ep1 should be disabled under profile B")
	}
	if !gotEp2.Enabled {
		t.Errorf("ep2 should be enabled under profile B")
	}
	if gotT1.Enabled {
		t.Errorf("t1 should be disabled under profile B")
	}
	if !gotT2.Enabled {
		t.Errorf("t2 should be enabled under profile B (wildcard)")
	}

	// Single-active invariant: A must have flipped to inactive.
	gotA, _ := s.GetProfile(pa.ID)
	gotB, _ := s.GetProfile(pb.ID)
	if gotA.Active {
		t.Errorf("profile A should be inactive after B activate")
	}
	if !gotB.Active {
		t.Errorf("profile B should be active")
	}

	active, err := s.GetActiveProfile()
	if err != nil {
		t.Fatalf("GetActiveProfile: %v", err)
	}
	if active == nil || active.ID != pb.ID {
		t.Errorf("GetActiveProfile=%v want id=%d", active, pb.ID)
	}
}

func TestProfile_DeleteActiveRefused(t *testing.T) {
	s := newProfileTestStore(t)
	ep1, _, _, _ := seedEndpointTunnels(t, s)
	p := &model.Profile{Name: "p"}
	if err := s.CreateProfile(p, []model.ProfileBinding{{EndpointID: ep1}}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if _, err := s.ActivateProfile(p.ID); err != nil {
		t.Fatalf("ActivateProfile: %v", err)
	}
	if err := s.DeleteProfile(p.ID); err == nil {
		t.Errorf("expected error deleting active profile")
	}
}
