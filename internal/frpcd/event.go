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

// defaultRingSize is the per-bus history depth used when no override is
// configured. 1024 covers ~5 minutes of moderately-busy frpc output (a
// burst of ~3 events/sec sustained) on top of the existing 64-slot
// per-subscriber buffer. Larger histories trade memory for replay
// reach; smaller ones reduce the chance that `frpdeck logs --since 5m`
// can answer correctly during a noisy storm.
const defaultRingSize = 1024

// EventBus is a fan-out pub/sub for driver events. Subscribers receive
// every published Event on a buffered channel; slow consumers are dropped
// rather than blocking the publisher because keeping the live frp engine
// responsive trumps log-line delivery.
//
// Each EventBus also keeps a bounded ring buffer of recent events so
// that `frpdeck logs --since 5m` (and equivalent CLI flags) can answer
// "what happened in the last N minutes?" without forcing every
// subscriber to live forever. The ring is independent from the
// per-subscriber channel — it holds whatever Publish saw, regardless
// of whether anybody was listening.
type EventBus struct {
	mu      sync.RWMutex
	subs    map[chan Event]struct{}
	bufSize int

	// Ring buffer state. ringNext is the index that will be written
	// next; (ringNext + ringSize - ringFill) % ringSize is the
	// oldest entry. We keep ringFill rather than treating "full"
	// implicitly so callers can iterate without ambiguity when the
	// bus has just started up.
	ring     []Event
	ringNext int
	ringFill int
}

// NewEventBus returns a ready-to-use EventBus with a 64-slot per-subscriber
// buffer and a default-sized history ring (see defaultRingSize). The
// buffer is large enough to absorb short bursts (e.g. a fresh frps
// connection emitting many setup lines in quick succession) without
// forcing the publisher path to block.
func NewEventBus() *EventBus {
	return &EventBus{
		subs:    make(map[chan Event]struct{}),
		bufSize: 64,
		ring:    make([]Event, defaultRingSize),
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
// (see EventBus doc). Every published event is also recorded in the
// ring buffer so Replay can return it later.
func (b *EventBus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b.mu.Lock()
	if len(b.ring) > 0 {
		b.ring[b.ringNext] = e
		b.ringNext = (b.ringNext + 1) % len(b.ring)
		if b.ringFill < len(b.ring) {
			b.ringFill++
		}
	}
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
	b.mu.Unlock()
}

// Replay returns events stored in the ring buffer whose timestamp is
// newer than `since`. The result is a fresh slice ordered oldest to
// newest, so callers can stream it directly to a subscriber without
// further sorting. A zero `since` means "everything in the ring",
// which is what `frpdeck logs --since 0` should yield.
//
// The bus may overwrite ring entries while the caller iterates the
// returned slice (Replay copies the snapshot under the lock to keep
// callers safe), so the snapshot is independent of further activity.
func (b *EventBus) Replay(since time.Time) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ringFill == 0 || len(b.ring) == 0 {
		return nil
	}
	// Walk from oldest to newest; the oldest sits at
	// (ringNext + ringSize - ringFill) % ringSize.
	out := make([]Event, 0, b.ringFill)
	start := (b.ringNext + len(b.ring) - b.ringFill) % len(b.ring)
	for i := 0; i < b.ringFill; i++ {
		idx := (start + i) % len(b.ring)
		ev := b.ring[idx]
		if !since.IsZero() && !ev.At.After(since) {
			continue
		}
		out = append(out, ev)
	}
	return out
}
