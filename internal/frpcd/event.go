package frpcd

import (
	"sync"
	"time"
)

// EventType enumerates the kinds of asynchronous notifications a FrpDriver
// can emit. The WebSocket layer (P1-C) and the lifecycle reconciler both
// subscribe to drive UI updates and persist transitions back into SQLite.
type EventType string

const (
	// EventEndpointState reports that the connection state of an Endpoint
	// (the link between FrpDeck and one frps server) changed.
	EventEndpointState EventType = "endpoint_state"

	// EventTunnelState reports that the working phase of a single Tunnel
	// changed (e.g. wait_start → running, running → check_failed).
	EventTunnelState EventType = "tunnel_state"

	// EventLog carries one log line emitted by the underlying frpc engine.
	// EndpointID/TunnelID may be 0 because frp's logger is package-global
	// and does not tag every line with the originating Endpoint.
	EventLog EventType = "log"

	// EventTunnelExpiring is published by the lifecycle manager when a
	// temporary tunnel is approaching its ExpireAt boundary. The "lead"
	// time is configurable via the runtime setting
	// `tunnel_expiring_notify_minutes`. The State field carries the
	// remaining seconds as a decimal string so the frontend can render
	// "4m 30s left" without re-fetching the tunnel row.
	EventTunnelExpiring EventType = "tunnel_expiring"
)

// Event is a single asynchronous update emitted by the driver. The struct
// is tagged for direct JSON encoding so the WebSocket layer can forward it
// to the browser without an intermediate transformation.
type Event struct {
	Type       EventType `json:"type"`
	EndpointID uint      `json:"endpoint_id,omitempty"`
	TunnelID   uint      `json:"tunnel_id,omitempty"`
	State      string    `json:"state,omitempty"`
	Err        string    `json:"err,omitempty"`
	Level      string    `json:"level,omitempty"`
	Msg        string    `json:"msg,omitempty"`
	At         time.Time `json:"at"`
}

// EventBus is a fan-out pub/sub for driver events. Subscribers receive
// every published Event on a buffered channel; slow consumers are dropped
// rather than blocking the publisher because keeping the live frp engine
// responsive trumps log-line delivery.
type EventBus struct {
	mu      sync.RWMutex
	subs    map[chan Event]struct{}
	bufSize int
}

// NewEventBus returns a ready-to-use EventBus with a 64-slot per-subscriber
// buffer. The buffer is large enough to absorb short bursts (e.g. a fresh
// frps connection emitting many setup lines in quick succession) without
// forcing the publisher path to block.
func NewEventBus() *EventBus {
	return &EventBus{
		subs:    make(map[chan Event]struct{}),
		bufSize: 64,
	}
}

// Subscribe registers a new listener and returns the receive channel plus
// a cancel function. The cancel removes the subscription and closes the
// channel; calling it twice is safe.
func (b *EventBus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, b.bufSize)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish broadcasts an Event to all current subscribers. Subscribers
// whose buffer is full will silently miss this event; this is intentional
// (see EventBus doc).
func (b *EventBus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
