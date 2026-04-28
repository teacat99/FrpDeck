package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer spins up a control Server inside a fresh temp dir,
// starts it, registers a cleanup to close it, and returns the
// matching client + dataDir. Failures are fatal because there is
// nothing meaningful the test can do with a half-built server.
func newTestServer(t *testing.T, h Handlers) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	s := New(dir, h)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewClient(dir), dir
}

func TestPingRoundTrip(t *testing.T) {
	c, _ := newTestServer(t, Handlers{
		Version:    func() string { return "v0-test" },
		ListenAddr: func() string { return "127.0.0.1:1234" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, l, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if v != "v0-test" || l != "127.0.0.1:1234" {
		t.Fatalf("unexpected version=%q listen=%q", v, l)
	}
}

func TestReconcileInvokesHandler(t *testing.T) {
	var called atomic.Int32
	c, _ := newTestServer(t, Handlers{
		Reconcile: func(ctx context.Context) error {
			called.Add(1)
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("handler called %d times, want 1", got)
	}
}

func TestReconcileSurfacesHandlerError(t *testing.T) {
	c, _ := newTestServer(t, Handlers{
		Reconcile: func(ctx context.Context) error { return errors.New("boom") },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Reconcile(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "boom" {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestUnknownHandlerReportsClearly(t *testing.T) {
	// All handler fields nil; reconcile should fail with the
	// "no handler" sentinel, not crash with a nil deref.
	c, _ := newTestServer(t, Handlers{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Reconcile(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDaemonNotRunning(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := c.Ping(ctx)
	if !errors.Is(err, ErrDaemonNotRunning) {
		t.Fatalf("expected ErrDaemonNotRunning, got %v", err)
	}
}

func TestSocketPermissions(t *testing.T) {
	// Windows uses named pipes; the chmod test does not apply.
	if runtime.GOOS == "windows" {
		t.Skip("named pipes do not have unix-style permissions")
	}
	c, dir := newTestServer(t, Handlers{Version: func() string { return "v" }})
	_ = c
	info, err := os.Stat(filepath.Join(dir, SocketFilename))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", mode)
	}
}

func TestStaleSocketIsCleanedUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes vanish with the listener; nothing to clean")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, SocketFilename)
	// Drop a stale plain file at the socket path. Start should
	// remove it rather than EADDRINUSE.
	if err := os.WriteFile(socket, []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	s := New(dir, Handlers{Version: func() string { return "v" }})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	c := NewClient(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
