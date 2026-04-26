// Package frpcd — Embedded driver implementation.
//
// EmbeddedDriver is the in-process FrpDriver: each FrpDeck Endpoint owns a
// dedicated `*frp/client.Service` instance, and tunnels are pushed into
// the running engine through the v0.68 ConfigSourceAggregator pipeline.
//
// Threading model:
//   - All driver methods are safe to call concurrently.
//   - Each endpoint has its own `endpointRunner`. The runner serialises
//     writes to its frp Service via a per-runner mutex.
//   - The driver's EventBus is the single fan-out for status/log updates
//     consumed by the WebSocket layer (P1-C) and lifecycle reconciler.
//
// Logging:
//   - frp's package-level logger is rerouted on driver creation to a
//     fan-out writer that publishes each line as Event{Type: EventLog}
//     while still forwarding to stderr for direct visibility in dev.
package frpcd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	frplog "github.com/fatedier/frp/pkg/util/log"
	golog "github.com/fatedier/golib/log"

	"github.com/teacat99/FrpDeck/internal/model"
)

// Embedded is the in-process FrpDriver backed by github.com/fatedier/frp.
type Embedded struct {
	bus *EventBus

	mu      sync.RWMutex
	runners map[uint]*endpointRunner

	logTapOnce sync.Once
}

// NewEmbedded constructs the driver and installs the global log tap. Call
// once per process; multiple instances would fight over frp's package-level
// logger and split log delivery across buses.
func NewEmbedded() *Embedded {
	e := &Embedded{
		bus:     NewEventBus(),
		runners: make(map[uint]*endpointRunner),
	}
	e.installLogTap()
	return e
}

// Name reports the driver kind. Surfaced in /api/version and the About page.
func (e *Embedded) Name() string { return "embedded" }

// Subscribe returns a channel that receives every Event published by the
// driver until the returned cancel func is invoked.
func (e *Embedded) Subscribe() (<-chan Event, func()) { return e.bus.Subscribe() }

// PublishEvent forwards an externally-sourced event onto the driver's
// bus so subscribers (WebSocket / lifecycle / tests) see it alongside
// engine-emitted events.
func (e *Embedded) PublishEvent(ev Event) { e.bus.Publish(ev) }

// installLogTap rewires frp's logger so log lines fan out to the bus while
// still echoing to stderr. Idempotent across NewEmbedded calls.
func (e *Embedded) installLogTap() {
	e.logTapOnce.Do(func() {
		w := &logTapWriter{bus: e.bus, next: os.Stderr}
		frplog.Logger = frplog.Logger.WithOptions(golog.WithOutput(w))
	})
}

// HealthCheck performs a quick TCP probe against the configured frps
// address. The full frp negotiation (auth, TLS) is exercised by Start.
func (e *Embedded) HealthCheck(ctx context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return errors.New("nil endpoint")
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	addr := net.JoinHostPort(ep.Addr, fmt.Sprint(ep.Port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// Start brings up the frp Service for the given Endpoint. Calling Start a
// second time on an already-running endpoint is a no-op; the endpoint
// snapshot is refreshed in case the operator changed any field.
func (e *Embedded) Start(ctx context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return errors.New("nil endpoint")
	}
	e.mu.Lock()
	if existing, ok := e.runners[ep.ID]; ok {
		existing.refreshEndpoint(ep)
		e.mu.Unlock()
		return nil
	}
	r := newEndpointRunner(e, ep)
	e.runners[ep.ID] = r
	e.mu.Unlock()
	return r.start()
}

// Stop tears down the Service for an endpoint. Idempotent.
func (e *Embedded) Stop(_ context.Context, ep *model.Endpoint) error {
	if ep == nil {
		return nil
	}
	e.mu.Lock()
	r := e.runners[ep.ID]
	delete(e.runners, ep.ID)
	e.mu.Unlock()
	if r != nil {
		r.stop()
	}
	return nil
}

// AddTunnel registers (or replaces) a tunnel on the endpoint's Service. If
// the endpoint runner is not yet started, AddTunnel starts it lazily so
// callers do not need to remember the explicit Start() step.
func (e *Embedded) AddTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	r, err := e.ensureRunner(ep)
	if err != nil {
		return err
	}
	return r.addTunnel(t)
}

// RemoveTunnel unregisters a tunnel by ID. The endpoint runner stays alive
// so other tunnels under the same Endpoint keep flowing.
func (e *Embedded) RemoveTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	if ep == nil || t == nil {
		return nil
	}
	e.mu.RLock()
	r := e.runners[ep.ID]
	e.mu.RUnlock()
	if r == nil {
		return nil
	}
	return r.removeTunnel(t)
}

// UpdateTunnel re-registers a tunnel — equivalent to AddTunnel since the
// runner keys its state by tunnel ID and the underlying frp Service replaces
// proxies atomically via UpdateConfigSource.
func (e *Embedded) UpdateTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	return e.AddTunnel(ep, t)
}

// GetEndpointStatus returns the cached connection state of an endpoint.
// "disconnected" if no runner exists yet (e.g. the endpoint was created
// but never started).
func (e *Embedded) GetEndpointStatus(ep *model.Endpoint) (*EndpointStatus, error) {
	if ep == nil {
		return nil, errors.New("nil endpoint")
	}
	e.mu.RLock()
	r := e.runners[ep.ID]
	e.mu.RUnlock()
	if r == nil {
		return &EndpointStatus{State: "disconnected", UpdatedAt: time.Now()}, nil
	}
	state, lastErr := r.snapshotState()
	if state == "" {
		state = "disconnected"
	}
	return &EndpointStatus{State: state, LastError: lastErr, UpdatedAt: time.Now()}, nil
}

// GetTunnelStatus reports the live phase of a single tunnel by querying
// frp's StatusExporter and mapping the phase string into FrpDeck terms.
func (e *Embedded) GetTunnelStatus(ep *model.Endpoint, t *model.Tunnel) (*TunnelStatus, error) {
	if ep == nil || t == nil {
		return nil, errors.New("nil endpoint or tunnel")
	}
	e.mu.RLock()
	r := e.runners[ep.ID]
	e.mu.RUnlock()
	if r == nil {
		return &TunnelStatus{State: "stopped", UpdateAt: time.Now()}, nil
	}
	if ws, ok := r.proxyStatus(tunnelName(t)); ok {
		return &TunnelStatus{State: mapPhase(ws.Phase), LastErr: ws.Err, UpdateAt: time.Now()}, nil
	}
	return &TunnelStatus{State: "stopped", UpdateAt: time.Now()}, nil
}

// Logs is intentionally a no-op on the embedded driver: the WebSocket
// layer subscribes to the bus instead of polling. Returning an empty
// slice keeps the interface contract.
func (e *Embedded) Logs(_ *model.Endpoint, _ int) ([]LogEntry, error) {
	return nil, nil
}

// ensureRunner returns the runner for ep, creating one (and starting the
// frp Service) if it does not exist yet.
func (e *Embedded) ensureRunner(ep *model.Endpoint) (*endpointRunner, error) {
	e.mu.RLock()
	r, ok := e.runners[ep.ID]
	e.mu.RUnlock()
	if ok {
		return r, nil
	}
	if err := e.Start(context.Background(), ep); err != nil {
		return nil, err
	}
	e.mu.RLock()
	r = e.runners[ep.ID]
	e.mu.RUnlock()
	if r == nil {
		return nil, errors.New("runner missing after start")
	}
	return r, nil
}

// ----------------------------- runner -----------------------------------

type endpointRunner struct {
	drv *Embedded
	mu  sync.Mutex

	ep model.Endpoint

	common *v1.ClientCommonConfig
	src    *source.ConfigSource
	agg    *source.Aggregator
	svc    *client.Service

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	proxies  map[uint]v1.ProxyConfigurer
	visitors map[uint]v1.VisitorConfigurer

	state        string
	lastErr      string
	statusByName map[string]string
}

func newEndpointRunner(drv *Embedded, ep *model.Endpoint) *endpointRunner {
	return &endpointRunner{
		drv:          drv,
		ep:           *ep,
		proxies:      make(map[uint]v1.ProxyConfigurer),
		visitors:     make(map[uint]v1.VisitorConfigurer),
		statusByName: make(map[string]string),
	}
}

func (r *endpointRunner) refreshEndpoint(ep *model.Endpoint) {
	r.mu.Lock()
	r.ep = *ep
	r.mu.Unlock()
}

// start builds a fresh frp Service and runs it in a goroutine.
func (r *endpointRunner) start() error {
	r.mu.Lock()
	common := EndpointCommon(&r.ep)
	src := source.NewConfigSource()
	agg := source.NewAggregator(src)
	if err := src.ReplaceAll(nil, nil); err != nil {
		r.mu.Unlock()
		return err
	}
	svc, err := client.NewService(client.ServiceOptions{
		Common:                 common,
		ConfigSourceAggregator: agg,
	})
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.common = common
	r.src = src
	r.agg = agg
	r.svc = svc

	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	r.cancel = cancel
	r.done = make(chan struct{})
	r.mu.Unlock()

	r.setState("connecting", "")
	go func() {
		defer close(r.done)
		if runErr := svc.Run(ctx); runErr != nil {
			r.setState("failed", runErr.Error())
			return
		}
		r.setState("disconnected", "")
	}()
	go r.statusLoop()
	return nil
}

// stop closes the frp Service gracefully and waits up to 5 seconds for the
// background goroutines to wind down before returning.
func (r *endpointRunner) stop() {
	r.mu.Lock()
	svc := r.svc
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()
	if svc != nil {
		svc.GracefulClose(0)
	}
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *endpointRunner) addTunnel(t *model.Tunnel) error {
	pcfg, err := BuildProxy(t)
	if err != nil {
		return err
	}
	vcfg, err := BuildVisitor(t)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if pcfg != nil {
		r.proxies[t.ID] = pcfg
	} else {
		delete(r.proxies, t.ID)
	}
	if vcfg != nil {
		r.visitors[t.ID] = vcfg
	} else {
		delete(r.visitors, t.ID)
	}
	r.mu.Unlock()
	return r.apply()
}

func (r *endpointRunner) removeTunnel(t *model.Tunnel) error {
	r.mu.Lock()
	delete(r.proxies, t.ID)
	delete(r.visitors, t.ID)
	delete(r.statusByName, tunnelName(t))
	r.mu.Unlock()
	return r.apply()
}

// apply pushes the current proxy/visitor map to the running Service via
// UpdateConfigSource. Safe to call before login completes — the inner
// reload no-ops when the control connection is not yet up.
func (r *endpointRunner) apply() error {
	r.mu.Lock()
	proxies := make([]v1.ProxyConfigurer, 0, len(r.proxies))
	for _, p := range r.proxies {
		proxies = append(proxies, p)
	}
	visitors := make([]v1.VisitorConfigurer, 0, len(r.visitors))
	for _, v := range r.visitors {
		visitors = append(visitors, v)
	}
	common := r.common
	svc := r.svc
	r.mu.Unlock()
	if svc == nil {
		return errors.New("endpoint not started")
	}
	return svc.UpdateConfigSource(common, proxies, visitors)
}

// statusLoop polls each registered proxy's WorkingStatus every 3s and
// publishes EventTunnelState whenever the phase changes.
func (r *endpointRunner) statusLoop() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			r.pollOnce()
		}
	}
}

func (r *endpointRunner) pollOnce() {
	r.mu.Lock()
	svc := r.svc
	r.mu.Unlock()
	if svc == nil {
		return
	}
	exp := svc.StatusExporter()

	r.mu.Lock()
	type pair struct {
		tunnelID uint
		name     string
	}
	pairs := make([]pair, 0, len(r.proxies))
	for tid, pc := range r.proxies {
		pairs = append(pairs, pair{tid, pc.GetBaseConfig().Name})
	}
	prev := r.statusByName
	r.mu.Unlock()

	anyConnected := false
	for _, p := range pairs {
		ws, ok := exp.GetProxyStatus(p.name)
		if !ok {
			continue
		}
		if ws.Phase != "" && ws.Phase != "new" {
			anyConnected = true
		}
		r.mu.Lock()
		previous := prev[p.name]
		r.statusByName[p.name] = ws.Phase
		r.mu.Unlock()
		if previous == ws.Phase {
			continue
		}
		r.drv.bus.Publish(Event{
			Type:       EventTunnelState,
			EndpointID: r.ep.ID,
			TunnelID:   p.tunnelID,
			State:      mapPhase(ws.Phase),
			Err:        ws.Err,
		})
	}

	if anyConnected {
		state, _ := r.snapshotState()
		if state != "connected" {
			r.setState("connected", "")
		}
	}
}

func (r *endpointRunner) proxyStatus(name string) (workingStatusView, bool) {
	r.mu.Lock()
	svc := r.svc
	r.mu.Unlock()
	if svc == nil {
		return workingStatusView{}, false
	}
	ws, ok := svc.StatusExporter().GetProxyStatus(name)
	if !ok || ws == nil {
		return workingStatusView{}, false
	}
	return workingStatusView{Phase: ws.Phase, Err: ws.Err}, true
}

type workingStatusView struct {
	Phase string
	Err   string
}

func (r *endpointRunner) snapshotState() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.lastErr
}

func (r *endpointRunner) setState(state, err string) {
	r.mu.Lock()
	if r.state == state && r.lastErr == err {
		r.mu.Unlock()
		return
	}
	r.state = state
	r.lastErr = err
	r.mu.Unlock()
	r.drv.bus.Publish(Event{
		Type:       EventEndpointState,
		EndpointID: r.ep.ID,
		State:      state,
		Err:        err,
	})
}

// mapPhase normalises frp's internal phase ("running", "wait start", ...)
// into the shorter set the FrpDeck UI expects ("starting", "running", ...).
func mapPhase(p string) string {
	switch p {
	case "new", "wait start":
		return "starting"
	case "running":
		return "running"
	case "start error", "check failed":
		return "failed"
	case "closed":
		return "stopped"
	}
	return p
}

// ---------------------------- log tap -----------------------------------

type logTapWriter struct {
	bus  *EventBus
	next io.Writer
}

// WriteLog implements golib/log.Writer so we receive level + timestamp
// alongside the formatted line. We strip the trailing newline before
// publishing so subscribers do not have to.
func (w *logTapWriter) WriteLog(p []byte, level golog.Level, when time.Time) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	w.bus.Publish(Event{
		Type:  EventLog,
		Level: level.String(),
		Msg:   line,
		At:    when,
	})
	if w.next != nil {
		return w.next.Write(p)
	}
	return len(p), nil
}

// Write satisfies io.Writer. Used as a fallback path when callers go
// through the plain stdlib logger surface.
func (w *logTapWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	w.bus.Publish(Event{Type: EventLog, Msg: line, At: time.Now()})
	if w.next != nil {
		return w.next.Write(p)
	}
	return len(p), nil
}
