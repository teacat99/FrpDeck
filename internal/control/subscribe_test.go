package control

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBus mimics the daemon's adapter goroutine: it lets the test
// inject events into the channel that the server's Subscribe
// handler receives, and provides a cancel func with idempotent
// semantics.
type fakeBus struct {
	ch chan json.RawMessage
}

func newFakeBus() *fakeBus {
	return &fakeBus{ch: make(chan json.RawMessage, 16)}
}

func (b *fakeBus) handler(ctx context.Context) (<-chan json.RawMessage, func()) {
	return b.ch, func() {
		// Closing twice would panic; the test publishes the close
		// itself when it wants to terminate the subscription.
	}
}

func TestSubscribeStreaming_ForwardsEvents(t *testing.T) {
	bus := newFakeBus()
	c, _ := newTestServer(t, Handlers{
		Subscribe: bus.handler,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, stop, err := c.Subscribe(ctx, SubscribeOptions{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	bus.ch <- json.RawMessage(`{"type":"log","msg":"hello"}`)
	bus.ch <- json.RawMessage(`{"type":"log","msg":"world"}`)

	got := drainN(t, events, 2, 2*time.Second)
	if string(got[0]) != `{"type":"log","msg":"hello"}` {
		t.Errorf("first event = %s", got[0])
	}
	if string(got[1]) != `{"type":"log","msg":"world"}` {
		t.Errorf("second event = %s", got[1])
	}
}

func TestSubscribeStreaming_TypeFilterAppliedDaemonSide(t *testing.T) {
	bus := newFakeBus()
	c, _ := newTestServer(t, Handlers{
		Subscribe: bus.handler,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, stop, err := c.Subscribe(ctx, SubscribeOptions{Types: []string{"log"}})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	bus.ch <- json.RawMessage(`{"type":"tunnel_state","state":"running"}`)
	bus.ch <- json.RawMessage(`{"type":"log","msg":"keep me"}`)

	got := drainN(t, events, 1, 2*time.Second)
	if string(got[0]) != `{"type":"log","msg":"keep me"}` {
		t.Errorf("kept = %s", got[0])
	}
}

func TestSubscribeStreaming_TunnelFilter(t *testing.T) {
	bus := newFakeBus()
	c, _ := newTestServer(t, Handlers{
		Subscribe: bus.handler,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, stop, err := c.Subscribe(ctx, SubscribeOptions{TunnelID: 42})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	bus.ch <- json.RawMessage(`{"type":"log","tunnel_id":1,"msg":"drop"}`)
	bus.ch <- json.RawMessage(`{"type":"log","tunnel_id":42,"msg":"keep"}`)

	got := drainN(t, events, 1, 2*time.Second)
	if string(got[0]) != `{"type":"log","tunnel_id":42,"msg":"keep"}` {
		t.Errorf("kept = %s", got[0])
	}
}

func TestSubscribeStreaming_NoHandlerRejects(t *testing.T) {
	c, _ := newTestServer(t, Handlers{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := c.Subscribe(ctx, SubscribeOptions{})
	if err == nil {
		t.Fatal("expected error when no Subscribe handler registered")
	}
}

func TestSubscribeStreaming_DaemonNotRunning(t *testing.T) {
	c := NewClient("/nonexistent/dir-for-cli-test")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, _, err := c.Subscribe(ctx, SubscribeOptions{})
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Fatalf("err = %v, want ErrDaemonNotRunning", err)
	}
}

func TestSubscribeStreaming_CancelStopsLoop(t *testing.T) {
	bus := newFakeBus()
	c, _ := newTestServer(t, Handlers{
		Subscribe: bus.handler,
	})
	ctx, cancel := context.WithCancel(context.Background())
	events, stop, err := c.Subscribe(ctx, SubscribeOptions{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	bus.ch <- json.RawMessage(`{"type":"log","msg":"first"}`)
	if got := drainN(t, events, 1, 2*time.Second); string(got[0]) == "" {
		t.Fatal("expected first event")
	}
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("event channel did not close after ctx cancel")
		}
	}
}

func TestSubscribeStreaming_DoesNotBlockPing(t *testing.T) {
	// Confirms streaming connections don't starve the accept loop:
	// even with a long-lived subscriber outstanding, ping should
	// still complete promptly.
	bus := newFakeBus()
	var pings atomic.Int32
	c, _ := newTestServer(t, Handlers{
		Subscribe: bus.handler,
		Version:   func() string { pings.Add(1); return "v0-test" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, stop, err := c.Subscribe(ctx, SubscribeOptions{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer stop()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPing()
	if _, _, err := c.Ping(pingCtx); err != nil {
		t.Fatalf("ping during subscribe: %v", err)
	}
	if got := pings.Load(); got != 1 {
		t.Fatalf("ping handler called %d times, want 1", got)
	}
	// And the subscriber still works after the unrelated ping.
	bus.ch <- json.RawMessage(`{"type":"log","msg":"after-ping"}`)
	got := drainN(t, events, 1, 2*time.Second)
	if string(got[0]) != `{"type":"log","msg":"after-ping"}` {
		t.Errorf("post-ping event = %s", got[0])
	}
}

// drainN reads exactly n messages from ch or fails after deadline.
func drainN(t *testing.T, ch <-chan json.RawMessage, n int, deadline time.Duration) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, n)
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for len(out) < n {
		select {
		case msg, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d/%d events", len(out), n)
			}
			out = append(out, msg)
		case <-timer.C:
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}
