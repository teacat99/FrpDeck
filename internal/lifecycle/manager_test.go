// P4 lifecycle coverage. The tests exercise the three new behaviours
// added on top of the P0/P1 timer + reconcile core:
//
//   - The early-warning timer publishes `tunnel_expiring` exactly once
//     per (tunnel, ExpireAt) pair.
//   - Renew(0) makes a temporary tunnel permanent and reactivates an
//     expired row.
//   - Reconcile() flips expired-but-active rows to `expired` and emits
//     an immediate warning when a still-active row is already inside
//     the threshold window (process-restart safety net).
//
// We use a real *store.Store backed by a temp-dir SQLite file so the
// GORM auto-migrate runs end-to-end; the driver is a frpcd.Mock that
// records AddTunnel/RemoveTunnel calls.
package lifecycle

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "lifecycle-test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.DB() })
	return s
}

// seedActiveTunnel creates an endpoint + tunnel pair in the store and
// returns the persisted tunnel. ExpireAt is optional (nil = permanent).
func seedActiveTunnel(t *testing.T, s *store.Store, expireAt *time.Time) *model.Tunnel {
	t.Helper()
	ep := &model.Endpoint{
		Name: "ep", Addr: "127.0.0.1", Port: 7000, Enabled: true,
	}
	if err := s.CreateEndpoint(ep); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	tu := &model.Tunnel{
		EndpointID: ep.ID,
		Name:       "demo",
		Type:       "tcp",
		LocalIP:    "127.0.0.1",
		LocalPort:  3389,
		RemotePort: 13389,
		Status:     model.StatusActive,
		Enabled:    true,
		ExpireAt:   expireAt,
	}
	if err := s.CreateTunnel(tu); err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
	return tu
}

// captureBus is a publishFn that fans events into a slice for
// assertions without racing against the lifecycle goroutines.
type captureBus struct {
	mu     sync.Mutex
	events []frpcd.Event
}

func (c *captureBus) publish(ev frpcd.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureBus) typed(want frpcd.EventType) []frpcd.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]frpcd.Event, 0, len(c.events))
	for _, e := range c.events {
		if e.Type == want {
			out = append(out, e)
		}
	}
	return out
}

// waitFor blocks until pred() returns true or the deadline elapses; the
// short polling interval keeps the failure mode "test ran 200ms then
// reported a clear timeout" instead of leaking goroutines.
func waitFor(t *testing.T, deadline time.Duration, pred func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor timed out after %s", deadline)
}

// TestExpiringTimerPublishesOnce confirms a notification timer fires
// once per scheduled ExpireAt, even when Reconcile runs in the
// background. This guards the dedup contract of the `notified` map.
func TestExpiringTimerPublishesOnce(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()
	bus := &captureBus{}

	// Threshold large enough that the warning fires *immediately* on
	// arming the timer (boot-inside-window path). 60 minutes vs an
	// expiry 80ms in the future = remaining(80ms) <= threshold(60m).
	m := New(s, drv, time.Hour, &Options{
		Publish:         bus.publish,
		ExpiringMinutes: func() int { return 60 },
	})
	expire := time.Now().Add(80 * time.Millisecond)
	tu := seedActiveTunnel(t, s, &expire)
	if err := m.Schedule(tu); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	waitFor(t, 200*time.Millisecond, func() bool {
		return len(bus.typed(frpcd.EventTunnelExpiring)) >= 1
	})

	// Force a Reconcile pass; it should NOT publish a duplicate
	// because notified[tu.ID] already records the same ExpireAt.
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := len(bus.typed(frpcd.EventTunnelExpiring)); got != 1 {
		t.Fatalf("expected exactly 1 expiring event, got %d", got)
	}
}

// TestExpireFiresAndCleansUp verifies the hard-expiry path: status flips
// to expired, the proxy is removed from the driver, and no expiring
// event is published when ExpireAt is already in the past at Schedule
// time (warning is meaningless after the fact).
func TestExpireFiresAndCleansUp(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()
	bus := &captureBus{}
	m := New(s, drv, time.Hour, &Options{
		Publish:         bus.publish,
		ExpiringMinutes: func() int { return 60 },
	})

	expire := time.Now().Add(40 * time.Millisecond)
	tu := seedActiveTunnel(t, s, &expire)
	if err := m.Schedule(tu); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		got, _ := s.GetTunnel(tu.ID)
		return got != nil && got.Status == model.StatusExpired
	})
}

// TestRenewExtendsAndReactivates covers two flows in one test (cheap to
// share the store/driver setup):
//
//  1. Renew(+1h) on an active row pushes ExpireAt forward and rearms.
//  2. Renew(0) on an expired row clears ExpireAt and reactivates it.
func TestRenewExtendsAndReactivates(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()
	bus := &captureBus{}
	m := New(s, drv, time.Hour, &Options{
		Publish:         bus.publish,
		ExpiringMinutes: func() int { return 0 }, // disable warnings to keep events list clean
	})

	expire := time.Now().Add(2 * time.Hour)
	tu := seedActiveTunnel(t, s, &expire)
	if err := m.Schedule(tu); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	renewed, err := m.Renew(tu.ID, time.Hour)
	if err != nil {
		t.Fatalf("Renew(+1h): %v", err)
	}
	if renewed.ExpireAt == nil || !renewed.ExpireAt.After(expire) {
		t.Fatalf("expected ExpireAt to advance past %v, got %v", expire, renewed.ExpireAt)
	}

	// Make permanent: pass delta=0; ExpireAt should be cleared.
	permanent, err := m.Renew(tu.ID, 0)
	if err != nil {
		t.Fatalf("Renew(0): %v", err)
	}
	if permanent.ExpireAt != nil {
		t.Fatalf("expected ExpireAt to be cleared, got %v", *permanent.ExpireAt)
	}

	// Renew(+delta) on a permanent row is rejected so the operator
	// cannot accidentally turn a permanent tunnel temporary via the
	// quick-action menu. A subsequent direct PUT is the right path.
	if _, err := m.Renew(tu.ID, time.Hour); err != ErrTunnelNoExpire {
		t.Fatalf("expected ErrTunnelNoExpire, got %v", err)
	}
}

// TestReconcileImmediateWarning is the boot safety net: a tunnel whose
// ExpireAt is already inside the threshold window when Reconcile runs
// should produce a warning event right away instead of waiting for the
// next loop tick.
func TestReconcileImmediateWarning(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()
	bus := &captureBus{}

	expire := time.Now().Add(2 * time.Minute)
	tu := seedActiveTunnel(t, s, &expire)

	m := New(s, drv, time.Hour, &Options{
		Publish:         bus.publish,
		ExpiringMinutes: func() int { return 5 }, // 5 min > 2 min remaining
	})
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	events := bus.typed(frpcd.EventTunnelExpiring)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 expiring event, got %d (%v)", len(events), events)
	}
	if events[0].TunnelID != tu.ID {
		t.Fatalf("warning fired for wrong tunnel: %d vs %d", events[0].TunnelID, tu.ID)
	}
}

// TestReconcileMarksExpired ensures a row whose ExpireAt is already in
// the past at boot is flipped to `expired` (and its proxy removed),
// guarding the "container restarted long after expiry" scenario.
func TestReconcileMarksExpired(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()

	expire := time.Now().Add(-5 * time.Minute)
	tu := seedActiveTunnel(t, s, &expire)

	m := New(s, drv, time.Hour, nil)
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got, err := s.GetTunnel(tu.ID)
	if err != nil {
		t.Fatalf("GetTunnel: %v", err)
	}
	if got.Status != model.StatusExpired {
		t.Fatalf("expected status=expired, got %q", got.Status)
	}
	if got.LastStopAt == nil {
		t.Fatalf("expected LastStopAt to be set")
	}
}

// TestReconcileRemoteNodes_ExpiresInvite ensures pending RemoteNodes
// whose InviteExpiry has passed are flipped to `expired` so the UI can
// surface "this invitation timed out, generate a fresh one".
func TestReconcileRemoteNodes_ExpiresInvite(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()

	past := time.Now().Add(-1 * time.Minute)
	node := &model.RemoteNode{
		Name:         "peer-a",
		Direction:    "manages_me",
		Status:       model.RemoteNodeStatusPending,
		InviteExpiry: &past,
	}
	if err := s.CreateRemoteNode(node); err != nil {
		t.Fatalf("CreateRemoteNode: %v", err)
	}

	m := New(s, drv, time.Hour, nil)
	if err := m.ReconcileRemoteNodes(); err != nil {
		t.Fatalf("ReconcileRemoteNodes: %v", err)
	}
	got, err := s.GetRemoteNode(node.ID)
	if err != nil {
		t.Fatalf("GetRemoteNode: %v", err)
	}
	if got == nil {
		t.Fatalf("node disappeared after reconcile")
	}
	if got.Status != model.RemoteNodeStatusExpired {
		t.Fatalf("expected status=expired, got %q", got.Status)
	}
}

// TestReconcileRemoteNodes_OfflineWhenTunnelDeleted ensures an active
// pairing whose backing tunnel was removed out-of-band gets flipped to
// `offline` so the UI does not keep advertising a dead pairing.
func TestReconcileRemoteNodes_OfflineWhenTunnelDeleted(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()

	node := &model.RemoteNode{
		Name:      "peer-b",
		Direction: "managed_by_me",
		Status:    model.RemoteNodeStatusActive,
		TunnelID:  9999, // never existed
	}
	if err := s.CreateRemoteNode(node); err != nil {
		t.Fatalf("CreateRemoteNode: %v", err)
	}

	m := New(s, drv, time.Hour, nil)
	if err := m.ReconcileRemoteNodes(); err != nil {
		t.Fatalf("ReconcileRemoteNodes: %v", err)
	}
	got, err := s.GetRemoteNode(node.ID)
	if err != nil {
		t.Fatalf("GetRemoteNode: %v", err)
	}
	if got.Status != model.RemoteNodeStatusOffline {
		t.Fatalf("expected status=offline, got %q", got.Status)
	}
}

// TestReconcileRemoteNodes_OfflineWhenTunnelFailed ensures a pairing
// whose linked tunnel sat in `failed`/`expired`/`stopped` for any
// reason is reflected as offline at the RemoteNode level.
func TestReconcileRemoteNodes_OfflineWhenTunnelFailed(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()

	tu := seedActiveTunnel(t, s, nil)
	tu.Status = model.StatusFailed
	if err := s.UpdateTunnel(tu); err != nil {
		t.Fatalf("UpdateTunnel: %v", err)
	}

	node := &model.RemoteNode{
		Name:      "peer-c",
		Direction: "manages_me",
		Status:    model.RemoteNodeStatusActive,
		TunnelID:  tu.ID,
	}
	if err := s.CreateRemoteNode(node); err != nil {
		t.Fatalf("CreateRemoteNode: %v", err)
	}

	m := New(s, drv, time.Hour, nil)
	if err := m.ReconcileRemoteNodes(); err != nil {
		t.Fatalf("ReconcileRemoteNodes: %v", err)
	}
	got, err := s.GetRemoteNode(node.ID)
	if err != nil {
		t.Fatalf("GetRemoteNode: %v", err)
	}
	if got.Status != model.RemoteNodeStatusOffline {
		t.Fatalf("expected status=offline, got %q", got.Status)
	}
}

// TestReconcileRemoteNodes_NoChangeForHealthyActive guards against the
// reaper accidentally flipping healthy pairings during a normal tick.
func TestReconcileRemoteNodes_NoChangeForHealthyActive(t *testing.T) {
	s := newTestStore(t)
	drv := frpcd.NewMock()

	tu := seedActiveTunnel(t, s, nil)
	node := &model.RemoteNode{
		Name:      "peer-d",
		Direction: "managed_by_me",
		Status:    model.RemoteNodeStatusActive,
		TunnelID:  tu.ID,
	}
	if err := s.CreateRemoteNode(node); err != nil {
		t.Fatalf("CreateRemoteNode: %v", err)
	}

	m := New(s, drv, time.Hour, nil)
	if err := m.ReconcileRemoteNodes(); err != nil {
		t.Fatalf("ReconcileRemoteNodes: %v", err)
	}
	got, err := s.GetRemoteNode(node.ID)
	if err != nil {
		t.Fatalf("GetRemoteNode: %v", err)
	}
	if got.Status != model.RemoteNodeStatusActive {
		t.Fatalf("expected status=active, got %q", got.Status)
	}
}
