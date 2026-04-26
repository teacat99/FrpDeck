// Package lifecycle owns the per-Tunnel expiry timers and the periodic
// reconciliation between the SQLite state and the live frpcd driver.
//
// Reliability strategy (cloned from PortPass; see plan.md §6):
//
//  1. Primary channel: AfterFunc fires at expire_at and stops the tunnel.
//  2. Fallback: every ReconcileInterval the manager scans DB vs. driver
//     and fixes any drift (expired-but-running tunnels, clock skew after
//     sleep, tunnels manually stopped on the frps side, etc.).
//  3. Boot: Start() reconciles once synchronously so the in-memory state
//     matches reality before the HTTP server begins accepting requests.
//  4. Shutdown: Stop() cancels timers but does NOT stop tunnels — a
//     desktop sleep / container restart should not be perceived as a
//     "stop"; the next boot reconciles and re-schedules.
//
// P0 ships the timer + reconcile skeleton without driver integration —
// the driver hooks are TODOs for P1 once `EmbeddedDriver` lands.
package lifecycle

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// Manager holds the per-tunnel timers and the reconcile ticker.
type Manager struct {
	store    *store.Store
	driver   frpcd.FrpDriver
	interval time.Duration

	mu     sync.Mutex
	timers map[uint]*time.Timer

	stopCh   chan struct{}
	stopOnce sync.Once
}

// New wires a Manager. ReconcileInterval defaults to 30s when zero.
func New(s *store.Store, d frpcd.FrpDriver, reconcileInterval time.Duration) *Manager {
	if reconcileInterval <= 0 {
		reconcileInterval = 30 * time.Second
	}
	return &Manager{
		store:    s,
		driver:   d,
		interval: reconcileInterval,
		timers:   make(map[uint]*time.Timer),
		stopCh:   make(chan struct{}),
	}
}

// Start performs initial reconciliation and launches the background ticker.
// Returns the first reconciliation error; the ticker loop swallows errors
// after logging them so a transient failure never kills the manager.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.Reconcile(); err != nil {
		return err
	}
	go m.loop(ctx)
	return nil
}

// Stop cancels every scheduled timer but leaves frp tunnels in place.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.timers {
		t.Stop()
		delete(m.timers, id)
	}
}

// Schedule registers an expiration timer for a temporary tunnel. Tunnels
// without ExpireAt are no-ops — they live forever unless stopped manually.
// Driver-side activation happens via AddTunnel / Start in P1; P0 just
// books the timer so the lifecycle plumbing is exercised.
func (m *Manager) Schedule(t *model.Tunnel) error {
	if t.ExpireAt == nil {
		return nil
	}
	m.armTimer(t.ID, time.Until(*t.ExpireAt))
	return nil
}

// Extend updates the scheduled expiration; the underlying frp proxy
// keeps running, only the timer is rewound.
func (m *Manager) Extend(t *model.Tunnel, newExpire time.Time) error {
	t.ExpireAt = &newExpire
	if err := m.store.UpdateTunnel(t); err != nil {
		return err
	}
	m.armTimer(t.ID, time.Until(newExpire))
	return nil
}

// Stop terminates a tunnel ahead of schedule (UI "立即停止"). Driver-side
// teardown is a P1 follow-up; for P0 we only update the persisted state.
func (m *Manager) StopTunnel(t *model.Tunnel) error {
	m.cancelTimer(t.ID)
	now := time.Now()
	t.Status = model.StatusStopped
	t.LastStopAt = &now
	return m.store.UpdateTunnel(t)
}

// Reconcile is exported so tests and the HTTP /api/health probe can
// trigger a forced pass. Safe to call concurrently with Schedule/Stop.
//
// P0 implementation only handles expired-but-still-active tunnels; the
// driver-side reconciliation (frpc proxy list ↔ DB) is P1.
func (m *Manager) Reconcile() error {
	active, err := m.store.ListActiveTunnels()
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range active {
		t := &active[i]
		if t.ExpireAt != nil && !t.ExpireAt.After(now) {
			m.cancelTimer(t.ID)
			t.Status = model.StatusExpired
			stoppedAt := now
			t.LastStopAt = &stoppedAt
			if err := m.store.UpdateTunnel(t); err != nil {
				log.Printf("[reconcile] mark expired tunnel %d failed: %v", t.ID, err)
			}
			continue
		}
		if t.ExpireAt != nil {
			m.armTimer(t.ID, time.Until(*t.ExpireAt))
		}
	}
	return nil
}

func (m *Manager) loop(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-t.C:
			if err := m.Reconcile(); err != nil {
				log.Printf("[reconcile] %v", err)
			}
		}
	}
}

func (m *Manager) armTimer(tunnelID uint, d time.Duration) {
	if d < 0 {
		d = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.timers[tunnelID]; ok {
		existing.Stop()
	}
	m.timers[tunnelID] = time.AfterFunc(d, func() { m.onExpire(tunnelID) })
}

func (m *Manager) cancelTimer(tunnelID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.timers[tunnelID]; ok {
		t.Stop()
		delete(m.timers, tunnelID)
	}
}

func (m *Manager) onExpire(tunnelID uint) {
	t, err := m.store.GetTunnel(tunnelID)
	if err != nil || t == nil {
		return
	}
	if t.Status != model.StatusActive && t.Status != model.StatusPending {
		return
	}
	now := time.Now()
	t.Status = model.StatusExpired
	t.LastStopAt = &now
	if err := m.store.UpdateTunnel(t); err != nil {
		log.Printf("[expire] update tunnel %d failed: %v", tunnelID, err)
	}
	m.mu.Lock()
	delete(m.timers, tunnelID)
	m.mu.Unlock()
}
