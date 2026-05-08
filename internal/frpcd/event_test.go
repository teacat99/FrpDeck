package frpcd

import (
	"testing"
	"time"
)

// TestEventBusReplayEmpty checks the no-history fast path. A fresh
// bus must return nil for Replay so callers can do `for _, ev := range bus.Replay(...)`
// without first having to Publish anything.
func TestEventBusReplayEmpty(t *testing.T) {
	b := NewEventBus()
	if got := b.Replay(time.Time{}); got != nil {
		t.Fatalf("fresh bus Replay = %v, want nil", got)
	}
}

// TestEventBusReplayBeforeFull verifies ordering & since-filtering
// while the ring is still partially filled. The replay slice must
// come back oldest-first regardless of the publish interleaving with
// the time cursor.
func TestEventBusReplayBeforeFull(t *testing.T) {
	b := NewEventBus()
	t0 := time.Now().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		b.Publish(Event{
			Type:  EventLog,
			Level: "info",
			Msg:   "msg",
			At:    t0.Add(time.Duration(i) * time.Second),
		})
	}

	all := b.Replay(time.Time{})
	if len(all) != 5 {
		t.Fatalf("Replay all = %d events, want 5", len(all))
	}
	for i := range all {
		want := t0.Add(time.Duration(i) * time.Second)
		if !all[i].At.Equal(want) {
			t.Fatalf("event[%d].At = %v, want %v", i, all[i].At, want)
		}
	}

	since := t0.Add(2*time.Second + time.Millisecond)
	tail := b.Replay(since)
	if len(tail) != 2 {
		t.Fatalf("Replay since=t+2.001s = %d events, want 2 (only t+3, t+4)", len(tail))
	}
	if !tail[0].At.Equal(t0.Add(3 * time.Second)) {
		t.Fatalf("tail[0].At = %v, want t0+3s", tail[0].At)
	}
}

// TestEventBusReplayWraps checks the canonical bug case: more events
// published than the ring holds means the oldest entries get
// overwritten and Replay must walk from (ringNext + size - fill) on,
// not from 0. Use a tiny custom-sized ring by reaching into the
// internals (we don't expose a constructor knob yet — this test
// guards the math, callers stay on defaultRingSize).
func TestEventBusReplayWraps(t *testing.T) {
	b := NewEventBus()
	b.ring = make([]Event, 4) // shrink for the test only
	b.ringNext = 0
	b.ringFill = 0

	t0 := time.Now()
	for i := 0; i < 7; i++ { // 7 publishes into a ring of 4: 3 oldest dropped
		b.Publish(Event{Type: EventLog, Msg: "n", At: t0.Add(time.Duration(i) * time.Millisecond)})
	}

	got := b.Replay(time.Time{})
	if len(got) != 4 {
		t.Fatalf("Replay len=%d, want 4 (ring capacity)", len(got))
	}
	wantStart := t0.Add(3 * time.Millisecond)
	if !got[0].At.Equal(wantStart) {
		t.Fatalf("got[0].At = %v, want %v (oldest after overwrite)", got[0].At, wantStart)
	}
	wantEnd := t0.Add(6 * time.Millisecond)
	if !got[3].At.Equal(wantEnd) {
		t.Fatalf("got[3].At = %v, want %v (newest)", got[3].At, wantEnd)
	}
}

// TestEventBusReplayDoesNotBlockSubscribers asserts that adding the
// ring write under the same critical section as the fan-out doesn't
// regress the "drop on full subscriber" guarantee — a slow consumer
// must still be tolerated, and the ring entry is recorded regardless.
func TestEventBusReplayDoesNotBlockSubscribers(t *testing.T) {
	b := NewEventBus()
	ch, cancel := b.Subscribe()
	defer cancel()

	for i := 0; i < b.bufSize+10; i++ {
		b.Publish(Event{Type: EventLog, Msg: "x", At: time.Now().Add(time.Duration(i) * time.Microsecond)})
	}

	if rep := b.Replay(time.Time{}); len(rep) != b.bufSize+10 {
		t.Fatalf("Replay = %d events, want %d (every Publish recorded)", len(rep), b.bufSize+10)
	}

	drained := 0
DRAIN:
	for {
		select {
		case <-ch:
			drained++
		default:
			break DRAIN
		}
	}
	if drained == 0 {
		t.Fatalf("subscriber drained 0 events, want >0 (publishers should not block on full)")
	}
}
