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
// P0 shipped the timer + reconcile skeleton without driver integration.
// P1-A wired StopTunnel / Reconcile / onExpire to the EmbeddedDriver.
// P4 layered the expiring-soon notifier on top: each scheduled tunnel
// also arms a *secondary* timer at ExpireAt - threshold that emits a
// `tunnel_expiring` event so the UI / ntfy can warn the operator before
// traffic actually drops.
package lifecycle

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/store"
)

// expiringMinutesProvider yields the current notify-ahead threshold in
// minutes. It is a function (not a plain int) so the lifecycle picks up
// runtime-settings changes without a manager restart.
type expiringMinutesProvider func() int

// publishFn forwards lifecycle-originated events to the driver's event
// bus. It is intentionally a value (not the FrpDriver interface) so we
// stay decoupled from the driver implementation; tests inject a closure
// that captures emitted events directly.
type publishFn func(frpcd.Event)

// Manager holds the per-tunnel timers and the reconcile ticker.
type Manager struct {
	store    *store.Store
	driver   frpcd.FrpDriver
	interval time.Duration

	publish      publishFn
	expiringMins expiringMinutesProvider

	mu     sync.Mutex
	timers map[uint]*timerPair
	// notified tracks which (tunnelID, expireAt) pair has already had
	// its expiring event published, so a 30s reconcile does not spam
	// the same warning every loop tick.
	notified map[uint]time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
}

// timerPair holds the two timers we schedule per temporary tunnel: the
// expiry timer that actually stops the proxy, and the optional notifier
// timer that fires the early-warning event. expireAt is captured so the
// reconcile loop can re-enter armTimer without clobbering the dedup
// marker for an unchanged schedule.
type timerPair struct {
	expire   *time.Timer
	notify   *time.Timer
	expireAt time.Time
}

// Options bundles the optional Manager dependencies. nil-safe defaults
// keep tests and the headless boot path terse — pass an empty struct or
// `nil` directly when you only need the timer + reconcile core.
type Options struct {
	// Publish is the sink for `tunnel_expiring` and any future
	// lifecycle-originated events. nil disables event publishing.
	Publish publishFn

	// ExpiringMinutes returns the current threshold in minutes. When
	// the function is nil OR returns <= 0, expiring timers are not
	// scheduled (operator opted out of advance notifications).
	ExpiringMinutes expiringMinutesProvider
}

// ErrTunnelNoExpire is returned by Renew when the operator tries to
// renew a tunnel that never had an expiration set in the first place.
// We surface it explicitly so the API layer can return 400 "nothing to
// renew" instead of silently turning a permanent tunnel into a
// temporary one.
var ErrTunnelNoExpire = errors.New("tunnel has no expiry to renew")

// New wires a Manager. ReconcileInterval defaults to 30s when zero;
// opts may be nil to disable advance-notification features.
func New(s *store.Store, d frpcd.FrpDriver, reconcileInterval time.Duration, opts *Options) *Manager {
	if reconcileInterval <= 0 {
		reconcileInterval = 30 * time.Second
	}
	m := &Manager{
		store:    s,
		driver:   d,
		interval: reconcileInterval,
		timers:   make(map[uint]*timerPair),
		notified: make(map[uint]time.Time),
		stopCh:   make(chan struct{}),
	}
	if opts != nil {
		m.publish = opts.Publish
		m.expiringMins = opts.ExpiringMinutes
	}
	return m
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
	for id, p := range m.timers {
		stopPair(p)
		delete(m.timers, id)
	}
}

// Schedule registers an expiration timer for a temporary tunnel. When
// ExpireAt is cleared (e.g. user toggled the tunnel back to "永久") the
// helper actively cancels any pre-existing timer so a stale callback
// can never fire and silently kill a permanent tunnel.
func (m *Manager) Schedule(t *model.Tunnel) error {
	if t.ExpireAt == nil {
		m.cancelTimer(t.ID)
		return nil
	}
	m.armTimer(t.ID, *t.ExpireAt)
	return nil
}

// Extend updates the scheduled expiration; the underlying frp proxy
// keeps running, only the timer is rewound.
func (m *Manager) Extend(t *model.Tunnel, newExpire time.Time) error {
	t.ExpireAt = &newExpire
	if err := m.store.UpdateTunnel(t); err != nil {
		return err
	}
	m.armTimer(t.ID, newExpire)
	return nil
}

// Renew is the API-friendly form of Extend: the tunnel is loaded by id,
// the new expiry is computed as max(now, current_expire) + delta, and
// the row is persisted. Passing `delta = 0` is the explicit "make
// permanent" signal — ExpireAt is cleared, and the row is reactivated
// when it had been auto-expired.
//
// Returns ErrTunnelNoExpire when the operator tries to renew a row that
// was never temporary; that prevents the renewal endpoint from silently
// turning permanent tunnels into temporary ones, which the UI does not
// expose anyway (the renew button only shows on rows with ExpireAt).
//
// `expired` rows are special-cased: renewing them flips the status back
// to `active` and re-registers the proxy with the live driver, so the
// operator gets a one-click "uh, give me another hour" recovery path.
// Returns the updated tunnel for the API handler to echo back.
func (m *Manager) Renew(id uint, delta time.Duration) (*model.Tunnel, error) {
	t, err := m.store.GetTunnel(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("tunnel not found")
	}
	if t.ExpireAt == nil && delta != 0 {
		// "Add 1 hour to a permanent tunnel" is almost certainly a UI
		// bug — refuse rather than silently turning it temporary.
		return nil, ErrTunnelNoExpire
	}

	if delta == 0 {
		// "Make permanent" — clear ExpireAt and reactivate if needed.
		t.ExpireAt = nil
		m.cancelTimer(t.ID)
		if t.Status == model.StatusExpired {
			if err := m.reactivate(t); err != nil {
				return nil, err
			}
		}
		if err := m.store.UpdateTunnel(t); err != nil {
			return nil, err
		}
		return t, nil
	}

	now := time.Now()
	base := now
	if t.ExpireAt != nil && t.ExpireAt.After(now) {
		base = *t.ExpireAt
	}
	newExpire := base.Add(delta)
	t.ExpireAt = &newExpire

	if t.Status == model.StatusExpired {
		if err := m.reactivate(t); err != nil {
			return nil, err
		}
	}
	if err := m.store.UpdateTunnel(t); err != nil {
		return nil, err
	}
	m.armTimer(t.ID, newExpire)
	return t, nil
}

// reactivate is the shared helper for "an expired row is being renewed".
// It re-registers the tunnel with the live driver and flips the
// in-memory status to active so subsequent reconciles do not undo the
// reactivation. Caller is responsible for persisting the update.
func (m *Manager) reactivate(t *model.Tunnel) error {
	if !t.Enabled {
		// User intentionally disabled the row; do not silently flip
		// it back on. Renewing the expiry alone is enough.
		return nil
	}
	m.pushToDriver(t)
	now := time.Now()
	t.Status = model.StatusActive
	t.LastStartAt = &now
	t.LastError = ""
	return nil
}

// Stop terminates a tunnel ahead of schedule (UI "立即停止"). The driver
// is asked to remove the proxy first; persisted state is then flipped to
// "stopped" regardless of driver outcome — the next reconcile catches
// any drift if the driver call failed transiently.
func (m *Manager) StopTunnel(t *model.Tunnel) error {
	m.cancelTimer(t.ID)
	m.removeFromDriver(t)
	now := time.Now()
	t.Status = model.StatusStopped
	t.LastStopAt = &now
	return m.store.UpdateTunnel(t)
}

// Reconcile is exported so tests and the HTTP /api/health probe can
// trigger a forced pass. Safe to call concurrently with Schedule/Stop.
//
// Behaviour:
//
//  1. Expired-but-still-active tunnels are flipped to `expired` and have
//     their proxy removed from the driver.
//  2. Still-active tunnels with a future ExpireAt have their timer rearmed
//     (covers process restarts losing in-memory timers).
//  3. Still-active tunnels are pushed into the driver — the driver state
//     is volatile (lives in process memory) so a restart of the FrpDeck
//     process needs to re-register every running tunnel with the engine.
//  4. Tunnels that fall inside the expiring-soon window have their warning
//     event published immediately so a process restart that crossed the
//     threshold does not silently swallow the notification.
func (m *Manager) Reconcile() error {
	active, err := m.store.ListActiveTunnels()
	if err != nil {
		return err
	}
	now := time.Now()
	threshold := m.expiringThreshold()
	for i := range active {
		t := &active[i]
		if t.ExpireAt != nil && !t.ExpireAt.After(now) {
			m.cancelTimer(t.ID)
			m.removeFromDriver(t)
			t.Status = model.StatusExpired
			stoppedAt := now
			t.LastStopAt = &stoppedAt
			if err := m.store.UpdateTunnel(t); err != nil {
				log.Printf("[reconcile] mark expired tunnel %d failed: %v", t.ID, err)
			}
			continue
		}
		m.pushToDriver(t)
		if t.ExpireAt != nil {
			m.armTimer(t.ID, *t.ExpireAt)
			// Process-restart safety net: if we crossed the
			// expiring threshold while the manager was down, the
			// armTimer call above already published the warning,
			// but the per-tunnel timer also fires the
			// notification, so we deduplicate via the notified
			// map (keyed by ExpireAt instant).
			if threshold > 0 && t.ExpireAt.Sub(now) <= threshold {
				m.publishExpiring(t, t.ExpireAt.Sub(now))
			}
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
			if err := m.ReconcileRemoteNodes(); err != nil {
				log.Printf("[reconcile-remote] %v", err)
			}
		}
	}
}

// ReconcileRemoteNodes walks every RemoteNode row and brings its status
// in line with reality:
//
//  1. `pending` rows whose InviteExpiry has passed without redemption
//     are flipped to `expired`. The operator must regenerate.
//  2. `active` rows whose backing tunnel was deleted out of band (or
//     never existed) are flipped to `offline`; revoke() already deletes
//     the tunnel + flips status itself, but a manual delete on the
//     Tunnels page leaves the RemoteNode behind so we adopt it here.
//  3. `active` rows whose backing tunnel sits in `failed` / `expired`
//     status are flipped to `offline` so the operator can spot dead
//     pairings without staring at the Tunnels list.
//
// The function is idempotent: nothing happens when every row already
// matches reality, so the 30s loop is cheap.
func (m *Manager) ReconcileRemoteNodes() error {
	if m.store == nil {
		return nil
	}
	nodes, err := m.store.ListRemoteNodes()
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range nodes {
		n := &nodes[i]
		nextStatus := n.Status
		switch n.Status {
		case model.RemoteNodeStatusPending:
			if n.InviteExpiry != nil && now.After(*n.InviteExpiry) {
				nextStatus = model.RemoteNodeStatusExpired
			}
		case model.RemoteNodeStatusActive:
			if n.TunnelID == 0 {
				nextStatus = model.RemoteNodeStatusOffline
				break
			}
			t, err := m.store.GetTunnel(n.TunnelID)
			if err != nil {
				log.Printf("[reconcile-remote] node %d tunnel lookup: %v", n.ID, err)
				continue
			}
			if t == nil {
				nextStatus = model.RemoteNodeStatusOffline
				break
			}
			if t.Status == model.StatusFailed || t.Status == model.StatusExpired || t.Status == model.StatusStopped {
				nextStatus = model.RemoteNodeStatusOffline
			}
		}
		if nextStatus != n.Status {
			n.Status = nextStatus
			if err := m.store.UpdateRemoteNode(n); err != nil {
				log.Printf("[reconcile-remote] update node %d: %v", n.ID, err)
			}
		}
	}
	return nil
}

// armTimer schedules both the hard expiry timer and (when a threshold
// is configured) the early-warning timer. Idempotent: any prior timer
// for the same tunnel ID is cancelled first so renewals never leak.
func (m *Manager) armTimer(tunnelID uint, expireAt time.Time) {
	d := time.Until(expireAt)
	if d < 0 {
		d = 0
	}
	threshold := m.expiringThreshold()

	m.mu.Lock()
	// Only clear the dedup marker when the schedule actually changes
	// (Renew / Extend); a Reconcile pass that re-arms an unchanged
	// ExpireAt must NOT trigger a second warning event.
	if existing, ok := m.timers[tunnelID]; ok {
		stopPair(existing)
		if !existing.expireAt.Equal(expireAt) {
			delete(m.notified, tunnelID)
		}
	}
	pair := &timerPair{
		expire:   time.AfterFunc(d, func() { m.onExpire(tunnelID) }),
		expireAt: expireAt,
	}
	if threshold > 0 {
		warnDur := d - threshold
		if warnDur > 0 {
			pair.notify = time.AfterFunc(warnDur, func() { m.onExpiring(tunnelID) })
		}
	}
	m.timers[tunnelID] = pair
	m.mu.Unlock()

	// Reconcile path: if we are *already* inside the warning window
	// when armTimer is called (e.g. boot picked up a tunnel about to
	// expire in 30s and the threshold is 5min), publish immediately so
	// the operator sees the alert without waiting for the next loop.
	if threshold > 0 && d > 0 && d <= threshold {
		t, err := m.store.GetTunnel(tunnelID)
		if err == nil && t != nil {
			m.publishExpiring(t, d)
		}
	}
}

func (m *Manager) cancelTimer(tunnelID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pair, ok := m.timers[tunnelID]; ok {
		stopPair(pair)
		delete(m.timers, tunnelID)
	}
	delete(m.notified, tunnelID)
}

// stopPair stops both timers in a pair, tolerating a nil notify timer.
func stopPair(p *timerPair) {
	if p == nil {
		return
	}
	if p.expire != nil {
		p.expire.Stop()
	}
	if p.notify != nil {
		p.notify.Stop()
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
	m.removeFromDriver(t)
	now := time.Now()
	t.Status = model.StatusExpired
	t.LastStopAt = &now
	if err := m.store.UpdateTunnel(t); err != nil {
		log.Printf("[expire] update tunnel %d failed: %v", tunnelID, err)
	}
	m.mu.Lock()
	delete(m.timers, tunnelID)
	delete(m.notified, tunnelID)
	m.mu.Unlock()
}

// onExpiring fires when the warning timer elapses — the tunnel still
// has `threshold` time left before it is auto-stopped. Publishes a
// `tunnel_expiring` event so the WebSocket layer / ntfy can surface
// the warning. Idempotent against multiple Reconcile passes via
// `notified`.
func (m *Manager) onExpiring(tunnelID uint) {
	t, err := m.store.GetTunnel(tunnelID)
	if err != nil || t == nil || t.ExpireAt == nil {
		return
	}
	// Skip if the row is no longer active (e.g. user already stopped
	// or renewed it past the threshold in the interim — the renewal
	// path resets `notified`, so a fresh warning will fire).
	if t.Status != model.StatusActive && t.Status != model.StatusPending {
		return
	}
	remaining := time.Until(*t.ExpireAt)
	if remaining <= 0 {
		return
	}
	m.publishExpiring(t, remaining)
}

// publishExpiring is the dedup'd publish path used by both the warning
// timer and the reconcile loop. Skips if we already fired for this
// (tunnelID, ExpireAt) pair to keep noise off the wire.
func (m *Manager) publishExpiring(t *model.Tunnel, remaining time.Duration) {
	if m.publish == nil || t.ExpireAt == nil {
		return
	}
	m.mu.Lock()
	last, seen := m.notified[t.ID]
	if seen && last.Equal(*t.ExpireAt) {
		m.mu.Unlock()
		return
	}
	m.notified[t.ID] = *t.ExpireAt
	m.mu.Unlock()
	m.publish(frpcd.Event{
		Type:       frpcd.EventTunnelExpiring,
		TunnelID:   t.ID,
		EndpointID: t.EndpointID,
		State:      strconv.FormatInt(int64(remaining.Seconds()), 10),
		Msg:        t.Name,
		At:         time.Now(),
	})
}

// expiringThreshold returns the current notify-ahead window. Capped at
// the lifecycle interval upper bound (24h) defensively so a misconfigured
// runtime setting cannot schedule a timer years into the future.
func (m *Manager) expiringThreshold() time.Duration {
	if m.expiringMins == nil {
		return 0
	}
	mins := m.expiringMins()
	if mins <= 0 {
		return 0
	}
	if mins > 24*60 {
		mins = 24 * 60
	}
	return time.Duration(mins) * time.Minute
}

// pushToDriver looks up the tunnel's endpoint and re-registers the proxy
// with the live engine. Failures are logged and swallowed; the periodic
// reconcile retries on its own cadence.
func (m *Manager) pushToDriver(t *model.Tunnel) {
	if m.driver == nil {
		return
	}
	ep, err := m.store.GetEndpoint(t.EndpointID)
	if err != nil || ep == nil || !ep.Enabled {
		return
	}
	if err := m.driver.AddTunnel(ep, t); err != nil {
		log.Printf("[reconcile] driver add tunnel %d: %v", t.ID, err)
	}
}

// removeFromDriver is the symmetric helper for proxy teardown. Same
// best-effort policy as pushToDriver.
func (m *Manager) removeFromDriver(t *model.Tunnel) {
	if m.driver == nil {
		return
	}
	ep, err := m.store.GetEndpoint(t.EndpointID)
	if err != nil || ep == nil {
		return
	}
	if err := m.driver.RemoveTunnel(ep, t); err != nil {
		log.Printf("[lifecycle] driver remove tunnel %d: %v", t.ID, err)
	}
}
