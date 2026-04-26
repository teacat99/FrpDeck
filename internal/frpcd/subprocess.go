// Package frpcd — Subprocess driver implementation (P8-A).
//
// The subprocess driver spawns the user-supplied (or FrpDeck-downloaded)
// frpc binary and treats it as an opaque worker. Each FrpDeck Endpoint
// owns one frpc process plus its own runtime directory under
// `<data_dir>/run/subprocess/ep-<id>/`:
//
//	frpc.toml       — full configuration rendered by BuildSubprocessTOML
//	frpc.log        — captured stdout/stderr for forensics
//
// Communication with the spawned frpc is via its built-in admin REST API
// (`webServer.addr/port/user/password`):
//
//	GET  /api/status            — proxy phases, drives EventTunnelState
//	POST /api/reload            — reload config from disk after we rewrite frpc.toml
//	POST /api/stop              — graceful shutdown on Stop()
//
// Threading model mirrors EmbeddedDriver: the driver holds a runner per
// endpoint, every method is safe for concurrent callers, and a single
// fan-out EventBus surfaces status / log lines to the WebSocket layer.

package frpcd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
)

// Subprocess is the FrpDriver implementation that proxies all real work
// to a spawned frpc binary. dataDir is the host-side root under which
// per-endpoint runtime directories are created.
type Subprocess struct {
	bus     *EventBus
	dataDir string

	mu      sync.RWMutex
	runners map[uint]*subprocessRunner
}

// NewSubprocess constructs a SubprocessDriver rooted at dataDir. The
// runtime directory `<dataDir>/run/subprocess` is created lazily on the
// first Start() so a misconfigured deployment fails late rather than at
// boot.
func NewSubprocess(dataDir string) *Subprocess {
	return &Subprocess{
		bus:     NewEventBus(),
		dataDir: dataDir,
		runners: make(map[uint]*subprocessRunner),
	}
}

// Name surfaces the driver kind on /api/version + the About page.
func (s *Subprocess) Name() string { return "subprocess" }

// Subscribe registers a fan-out subscriber on the driver's EventBus.
func (s *Subprocess) Subscribe() (<-chan Event, func()) { return s.bus.Subscribe() }

// PublishEvent forwards an externally-produced event onto the bus so the
// WebSocket layer / lifecycle reconciler see it alongside engine events.
func (s *Subprocess) PublishEvent(ev Event) { s.bus.Publish(ev) }

// HealthCheck is a TCP probe — the actual frp negotiation (auth/TLS)
// happens once the runner spawns frpc.
func (s *Subprocess) HealthCheck(ctx context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return errors.New("nil endpoint")
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ep.Addr, fmt.Sprint(ep.Port)))
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// Start brings up the frpc subprocess for ep. Idempotent: if the runner
// already exists it just refreshes the cached endpoint snapshot.
func (s *Subprocess) Start(ctx context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return errors.New("nil endpoint")
	}
	s.mu.Lock()
	if r, ok := s.runners[ep.ID]; ok {
		r.refreshEndpoint(ep)
		s.mu.Unlock()
		return nil
	}
	r, err := newSubprocessRunner(s, ep)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.runners[ep.ID] = r
	s.mu.Unlock()
	return r.start(ctx)
}

// Stop tears down the runner. Idempotent — a missing runner is a no-op.
func (s *Subprocess) Stop(_ context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return nil
	}
	s.mu.Lock()
	r := s.runners[ep.ID]
	delete(s.runners, ep.ID)
	s.mu.Unlock()
	if r != nil {
		r.stop()
	}
	return nil
}

// AddTunnel registers (or replaces) a tunnel. Lazily starts the runner so
// callers do not need a prior Start().
func (s *Subprocess) AddTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	r, err := s.ensureRunner(ep)
	if err != nil {
		return err
	}
	return r.addTunnel(t)
}

// RemoveTunnel drops a tunnel from the spawned config. The runner stays
// up so other tunnels under the same Endpoint keep flowing.
func (s *Subprocess) RemoveTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	if ep == nil || t == nil {
		return nil
	}
	s.mu.RLock()
	r := s.runners[ep.ID]
	s.mu.RUnlock()
	if r == nil {
		return nil
	}
	return r.removeTunnel(t)
}

// UpdateTunnel re-registers a tunnel — the runner re-renders frpc.toml
// and POSTs /api/reload, which is atomic from frpc's perspective.
func (s *Subprocess) UpdateTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	return s.AddTunnel(ep, t)
}

// GetEndpointStatus reports the runner's cached state. "disconnected" if
// no runner has been spawned yet.
func (s *Subprocess) GetEndpointStatus(ep *model.Endpoint) (*EndpointStatus, error) {
	if ep == nil {
		return nil, errors.New("nil endpoint")
	}
	s.mu.RLock()
	r := s.runners[ep.ID]
	s.mu.RUnlock()
	if r == nil {
		return &EndpointStatus{State: "disconnected", UpdatedAt: time.Now()}, nil
	}
	state, lastErr := r.snapshotState()
	if state == "" {
		state = "disconnected"
	}
	return &EndpointStatus{State: state, LastError: lastErr, UpdatedAt: time.Now()}, nil
}

// GetTunnelStatus polls the cached per-tunnel phase. Driver populates
// the cache on each /api/status poll inside the runner.
func (s *Subprocess) GetTunnelStatus(ep *model.Endpoint, t *model.Tunnel) (*TunnelStatus, error) {
	if ep == nil || t == nil {
		return nil, errors.New("nil endpoint or tunnel")
	}
	s.mu.RLock()
	r := s.runners[ep.ID]
	s.mu.RUnlock()
	if r == nil {
		return &TunnelStatus{State: "stopped", UpdateAt: time.Now()}, nil
	}
	phase, errMsg, ok := r.tunnelPhase(tunnelName(t))
	if !ok {
		return &TunnelStatus{State: "stopped", UpdateAt: time.Now()}, nil
	}
	return &TunnelStatus{State: mapPhase(phase), LastErr: errMsg, UpdateAt: time.Now()}, nil
}

// Logs is a no-op: stdout/stderr are streamed to the EventBus already so
// the WebSocket layer subscribes there directly.
func (s *Subprocess) Logs(_ *model.Endpoint, _ int) ([]LogEntry, error) {
	return nil, nil
}

func (s *Subprocess) ensureRunner(ep *model.Endpoint) (*subprocessRunner, error) {
	s.mu.RLock()
	r, ok := s.runners[ep.ID]
	s.mu.RUnlock()
	if ok {
		return r, nil
	}
	if err := s.Start(context.Background(), ep); err != nil {
		return nil, err
	}
	s.mu.RLock()
	r = s.runners[ep.ID]
	s.mu.RUnlock()
	if r == nil {
		return nil, errors.New("runner missing after start")
	}
	return r, nil
}

// resolveBinary locates the frpc executable. Search order:
//  1. ep.SubprocessPath (operator override)
//  2. <dataDir>/bin/frpc-<BundledFrpVersion>[.exe] (FrpDeck-managed download)
//  3. PATH lookup via os.LookPath("frpc")
func (s *Subprocess) resolveBinary(ep *model.Endpoint) (string, error) {
	if p := strings.TrimSpace(ep.SubprocessPath); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else {
			return "", fmt.Errorf("frpc binary not found at %q: %w", p, err)
		}
	}
	candidate := filepath.Join(s.dataDir, "bin", "frpc-"+BundledFrpVersion)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	if _, err := os.Stat(candidate + ".exe"); err == nil {
		return candidate + ".exe", nil
	}
	return "", fmt.Errorf("frpc binary not configured; set Endpoint.SubprocessPath or use the bundled-frpc downloader")
}

// allocateAdminPort grabs an ephemeral 127.0.0.1 port. There is a small
// race between Listener close and frpc bind, but the loopback-only scope
// + the fact that we mint random admin credentials makes the leftover
// risk small enough for the convenience.
func allocateAdminPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// generateSecret returns a hex-encoded random string of `n` bytes — used
// for the admin user/password which are in-memory only.
func generateSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// runDir returns the per-endpoint runtime directory. Created with 0o700
// because frpc.toml may contain auth tokens.
func (s *Subprocess) runDir(epID uint) string {
	return filepath.Join(s.dataDir, "run", "subprocess", fmt.Sprintf("ep-%d", epID))
}
