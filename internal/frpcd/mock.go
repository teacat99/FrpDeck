package frpcd

import (
	"context"
	"sync"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
)

// Mock is an in-memory FrpDriver used by tests and by P0 development
// before a real driver lands. It records every Start/Stop/Add/Remove
// so test assertions can verify call sequences, and it never dials
// the network — perfect for CI and unit tests.
type Mock struct {
	mu        sync.Mutex
	started   map[uint]bool                   // endpointID -> running?
	tunnels   map[uint]map[uint]*model.Tunnel // endpointID -> tunnelID -> tunnel
	tunStatus map[uint]map[uint]*TunnelStatus // endpointID -> tunnelID -> status
	bus       *EventBus
}

// NewMock returns a fresh in-memory driver instance. Each Endpoint
// gets its own logical state so concurrent Endpoints don't interfere.
func NewMock() *Mock {
	return &Mock{
		started:   make(map[uint]bool),
		tunnels:   make(map[uint]map[uint]*model.Tunnel),
		tunStatus: make(map[uint]map[uint]*TunnelStatus),
		bus:       NewEventBus(),
	}
}

// Subscribe returns the no-op event bus tied to this mock. The mock never
// publishes events on its own, but tests can inject events through it.
func (m *Mock) Subscribe() (<-chan Event, func()) { return m.bus.Subscribe() }

// Bus exposes the underlying EventBus so tests can publish synthetic
// events to drive the WebSocket / lifecycle reconciler under test.
func (m *Mock) Bus() *EventBus { return m.bus }

// Name reports the driver kind. Used in logs and the /api/version
// endpoint so operators can see which driver is live.
func (m *Mock) Name() string { return "mock" }

func (m *Mock) HealthCheck(_ context.Context, ep *model.Endpoint) error { return nil }

func (m *Mock) Start(_ context.Context, ep *model.Endpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started[ep.ID] = true
	if _, ok := m.tunnels[ep.ID]; !ok {
		m.tunnels[ep.ID] = make(map[uint]*model.Tunnel)
		m.tunStatus[ep.ID] = make(map[uint]*TunnelStatus)
	}
	return nil
}

func (m *Mock) Stop(_ context.Context, ep *model.Endpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.started, ep.ID)
	return nil
}

func (m *Mock) AddTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[ep.ID] == nil {
		m.tunnels[ep.ID] = make(map[uint]*model.Tunnel)
		m.tunStatus[ep.ID] = make(map[uint]*TunnelStatus)
	}
	m.tunnels[ep.ID][t.ID] = t
	m.tunStatus[ep.ID][t.ID] = &TunnelStatus{State: "running", UpdateAt: time.Now()}
	return nil
}

func (m *Mock) RemoveTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[ep.ID] != nil {
		delete(m.tunnels[ep.ID], t.ID)
	}
	if m.tunStatus[ep.ID] != nil {
		delete(m.tunStatus[ep.ID], t.ID)
	}
	return nil
}

// UpdateTunnel re-records a tunnel; matches Embedded's "remove + add"
// semantics so callers see identical behaviour against either driver.
func (m *Mock) UpdateTunnel(ep *model.Endpoint, t *model.Tunnel) error {
	return m.AddTunnel(ep, t)
}

func (m *Mock) GetEndpointStatus(ep *model.Endpoint) (*EndpointStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := "disconnected"
	if m.started[ep.ID] {
		state = "connected"
	}
	return &EndpointStatus{State: state, UpdatedAt: time.Now()}, nil
}

func (m *Mock) GetTunnelStatus(ep *model.Endpoint, t *model.Tunnel) (*TunnelStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunStatus[ep.ID] != nil {
		if st, ok := m.tunStatus[ep.ID][t.ID]; ok {
			return st, nil
		}
	}
	return &TunnelStatus{State: "stopped", UpdateAt: time.Now()}, nil
}

func (m *Mock) Logs(_ *model.Endpoint, n int) ([]LogEntry, error) {
	return nil, nil
}
