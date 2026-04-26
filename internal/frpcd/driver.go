// Package frpcd defines the FrpDriver abstraction that decouples the
// rest of FrpDeck from a specific frp client implementation.
//
// Three drivers are planned:
//
//   - EmbeddedDriver  — in-process `*client.Service` (default; P1)
//   - SubprocessDriver — spawn an external user-supplied `frpc` binary (P8)
//   - MockDriver      — in-memory stub for tests / P0 scaffolding
//
// P0 ships only the interface plus MockDriver so the rest of the stack
// (lifecycle, api, store, main) compiles end-to-end.
package frpcd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
)

// EndpointStatus describes the live state of an Endpoint's connection
// to its frps server. Returned by FrpDriver.HealthCheck and exposed on
// the dashboard.
type EndpointStatus struct {
	State     string    `json:"state"` // disconnected / connecting / connected / failed
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TunnelStatus is the live status of a single Tunnel as known by the
// driver. Populated by FrpDriver.GetStatus and surfaced on the tunnels
// page next to the persisted state.
type TunnelStatus struct {
	State    string    `json:"state"` // running / stopped / failed
	LastErr  string    `json:"last_error,omitempty"`
	BytesIn  int64     `json:"bytes_in"`
	BytesOut int64     `json:"bytes_out"`
	UpdateAt time.Time `json:"updated_at"`
}

// LogEntry is a single line emitted by the underlying frpc engine.
// Drivers expose them through Logs(); the API layer streams them to
// the UI's live-log panel.
type LogEntry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

// FrpDriver is the contract every concrete frp client implementation
// must satisfy. The interface is intentionally narrow: lifecycle owns
// "when" (timers, reconcile), the driver owns "how" (open connection,
// add/remove proxy, read status). All methods must be safe to call
// from multiple goroutines concurrently.
type FrpDriver interface {
	Name() string
	HealthCheck(ctx context.Context, ep *model.Endpoint) error
	Start(ctx context.Context, ep *model.Endpoint) error
	Stop(ctx context.Context, ep *model.Endpoint) error
	AddTunnel(ep *model.Endpoint, t *model.Tunnel) error
	RemoveTunnel(ep *model.Endpoint, t *model.Tunnel) error
	UpdateTunnel(ep *model.Endpoint, t *model.Tunnel) error
	GetEndpointStatus(ep *model.Endpoint) (*EndpointStatus, error)
	GetTunnelStatus(ep *model.Endpoint, t *model.Tunnel) (*TunnelStatus, error)
	Logs(ep *model.Endpoint, n int) ([]LogEntry, error)
	// Subscribe returns a channel that fires on every async event the
	// driver wants to surface (status transitions, log lines). The cancel
	// closure unregisters the subscriber and closes the channel.
	Subscribe() (<-chan Event, func())

	// PublishEvent injects an externally-sourced event onto the same
	// bus that Subscribe() reads from. Used by the lifecycle manager to
	// emit `tunnel_expiring` warnings on the same channel the WebSocket
	// fan-out is already listening on, so the UI layer does not need a
	// second subscription path.
	PublishEvent(Event)
}

// DriverOptions carries the cross-cutting bits of context that some
// driver kinds need at construction time. Embedded ignores the options;
// SubprocessDriver requires DataDir to compute the per-endpoint runtime
// directory.
type DriverOptions struct {
	DataDir string
}

// NewDriver picks the driver implementation by name. Unknown names fall
// back to the mock so a misconfigured deployment is degraded but never
// panics on boot. Callers who don't need to pass options can use
// `DriverOptions{}` — Embedded and Mock ignore the field.
func NewDriver(name string, opts DriverOptions) (FrpDriver, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mock":
		return NewMock(), nil
	case "embedded":
		return NewEmbedded(), nil
	case "subprocess":
		return NewSubprocess(opts.DataDir), nil
	}
	return nil, fmt.Errorf("unknown frpcd driver %q", name)
}

// BundledFrpVersion is surfaced via /api/version and the About page.
// Kept in the driver package so a custom-frp build can override it via
// linker flags (`-ldflags "-X 'github.com/teacat99/FrpDeck/internal/frpcd.BundledFrpVersion=v0.65.0'"`).
var BundledFrpVersion = "v0.68.x"
