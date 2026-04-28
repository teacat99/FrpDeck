// Server side of the control channel — the daemon-facing half.
//
// One Server instance is owned by the bootstrap routine in
// cmd/server/bootstrap.go; it is started after lifecycle.Manager.Start
// (so a Reconcile call from a fast CLI cannot race the manager into
// existence) and stopped before lifecycle.Manager.Stop (so we stop
// accepting commands before tearing down the components those
// commands target).
//
// Concurrency model: one accept loop, one goroutine per connection.
// Connections are short-lived (fire-and-forget RPC), so no pooling
// or backpressure is needed. The handler function set passed to
// New() is called inline on the connection goroutine — handlers
// must not block indefinitely; they should kick off work
// asynchronously and return.

package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Handlers wires the daemon's actions to the wire commands. Every
// field is optional: a nil handler short-circuits with
// "command not supported" so the daemon can advertise a partial
// surface during early bring-up without crashing the CLI.
type Handlers struct {
	// Version returns the daemon's running version string. Used by
	// CmdPing to give the CLI a meaningful "doctor" line.
	Version func() string

	// ListenAddr returns the daemon's HTTP listen address (e.g.
	// "127.0.0.1:8080"). Surfaced via CmdPing for the doctor command.
	ListenAddr func() string

	// Reconcile triggers an immediate lifecycle reconciliation. It
	// runs synchronously from the CLI's perspective, but should
	// itself complete within seconds — this is just "diff DB vs
	// running drivers, start/stop the delta".
	Reconcile func(ctx context.Context) error

	// ReloadRuntime asks the runtime.Settings store to refresh from
	// SQLite KV. Synchronous; expected to complete in milliseconds.
	ReloadRuntime func(ctx context.Context) error

	// Shutdown begins graceful daemon shutdown. May return after
	// initiating shutdown — the daemon then exits asynchronously.
	Shutdown func(ctx context.Context) error
}

// Server accepts inbound control-channel connections and dispatches
// each line to the matching handler. Server is safe to use from a
// single goroutine (the bootstrap caller); methods are not designed
// to be called concurrently.
type Server struct {
	socketPath string
	handlers   Handlers
	listener   net.Listener
	mu         sync.Mutex
	closed     bool
	wg         sync.WaitGroup
}

// New builds a Server that will listen at <dataDir>/<SocketFilename>.
// The socket is not opened until Start is called — keeping the
// constructor side-effect-free makes wiring tests and shutdown
// sequencing straightforward.
//
// dataDir must already exist; the bootstrap routine calls os.MkdirAll
// on it before the store opens the database, so by the time we get
// here the directory is guaranteed.
func New(dataDir string, handlers Handlers) *Server {
	return &Server{
		socketPath: filepath.Join(dataDir, SocketFilename),
		handlers:   handlers,
	}
}

// SocketPath returns the on-disk path of the control socket. Used by
// tests + the doctor command to confirm the daemon's address.
func (s *Server) SocketPath() string { return s.socketPath }

// Start opens the socket and begins accepting connections. Returns
// an error if the socket cannot be created (most often because a
// stale socket file survived a crash and we cannot unlink it).
//
// Start is intentionally synchronous: it returns once the listener
// is bound, before the accept loop is fired up, so callers know the
// CLI can connect as soon as Start returns nil.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("control: server already closed")
	}

	// Best-effort cleanup of stale sockets. A leftover from a
	// previous crashed daemon would otherwise EADDRINUSE the
	// fresh listen call. Ignore "not exist"; report anything else.
	if err := removeStaleSocket(s.socketPath); err != nil {
		return err
	}

	ln, err := listenSocket(s.socketPath)
	if err != nil {
		return err
	}
	s.listener = ln

	s.wg.Add(1)
	go s.acceptLoop(ln)
	return nil
}

// Close stops accepting new connections and waits for in-flight
// handlers to complete (best-effort, with a short timeout). Safe to
// call multiple times.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	s.listener = nil
	s.mu.Unlock()

	var firstErr error
	if ln != nil {
		if err := ln.Close(); err != nil {
			firstErr = err
		}
	}
	// Connection goroutines exit naturally once the listener is
	// closed; bound the wait so a wedged client cannot block daemon
	// shutdown forever.
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		log.Printf("control: shutdown timeout — leaving %d connection goroutines behind", connGoroutineCount(&s.wg))
	}
	// Best-effort socket cleanup. On Windows the named pipe vanishes
	// when the listener closes, so this is a no-op there.
	_ = removeStaleSocket(s.socketPath)
	return firstErr
}

func (s *Server) acceptLoop(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			// net.ErrClosed is the canonical "we shut down on
			// purpose" signal post-Go-1.16.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("control: accept: %v", err)
			// Brief sleep so a pathological transient error does
			// not spin a tight loop (e.g. EMFILE).
			time.Sleep(50 * time.Millisecond)
			continue
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	// Each connection carries one request/response pair. The 5s
	// deadline makes a stuck CLI invisible to the daemon almost
	// immediately; legitimate handlers (Reconcile, ReloadRuntime)
	// finish in well under a second.
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		writeErr(conn, "read request: "+err.Error())
		return
	}
	if line == "" {
		writeErr(conn, "empty request")
		return
	}

	var req Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		writeErr(conn, "invalid request: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp := s.dispatch(ctx, req)

	buf, err := Encode(resp)
	if err != nil {
		log.Printf("control: encode response: %v", err)
		return
	}
	if _, err := conn.Write(buf); err != nil {
		log.Printf("control: write response: %v", err)
	}
}

func (s *Server) dispatch(ctx context.Context, req Request) Response {
	switch req.Command {
	case CmdPing:
		data := map[string]string{}
		if s.handlers.Version != nil {
			data["version"] = s.handlers.Version()
		}
		if s.handlers.ListenAddr != nil {
			data["listen"] = s.handlers.ListenAddr()
		}
		return Response{Ok: true, Data: data}
	case CmdReconcile:
		if s.handlers.Reconcile == nil {
			return Response{Ok: false, Error: "reconcile: no handler"}
		}
		if err := s.handlers.Reconcile(ctx); err != nil {
			return Response{Ok: false, Error: err.Error()}
		}
		return Response{Ok: true}
	case CmdReloadRuntime:
		if s.handlers.ReloadRuntime == nil {
			return Response{Ok: false, Error: "reload_runtime: no handler"}
		}
		if err := s.handlers.ReloadRuntime(ctx); err != nil {
			return Response{Ok: false, Error: err.Error()}
		}
		return Response{Ok: true}
	case CmdShutdown:
		if s.handlers.Shutdown == nil {
			return Response{Ok: false, Error: "shutdown: no handler"}
		}
		if err := s.handlers.Shutdown(ctx); err != nil {
			return Response{Ok: false, Error: err.Error()}
		}
		return Response{Ok: true}
	default:
		return Response{Ok: false, Error: "unknown command: " + string(req.Command)}
	}
}

func writeErr(conn net.Conn, msg string) {
	buf, err := Encode(Response{Ok: false, Error: msg})
	if err != nil {
		return
	}
	_, _ = conn.Write(buf)
}

// removeStaleSocket unlinks an existing socket file so a fresh
// listen call does not EADDRINUSE. We only treat "not exist" as a
// non-error; everything else (permissions, busy file) deserves to
// surface to the operator.
func removeStaleSocket(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// connGoroutineCount is a tiny helper so the timeout warning above
// can show "N goroutines abandoned" without exposing a raw counter.
// Using sync.WaitGroup as an opaque counter is hacky; a proper
// solution would track the count explicitly. Kept as a stub so we
// can swap implementations without touching the call site.
func connGoroutineCount(_ *sync.WaitGroup) int { return 0 }
