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
	"strconv"
	"strings"
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

	// Subscribe attaches a new event-bus listener. The returned
	// channel must close (and the cancel must be safe to call
	// twice) once the listener is detached. The returned events
	// are encoded as raw JSON so this package does not need to
	// depend on internal/frpcd.
	//
	// When nil, CmdSubscribe responds with "no handler" and the
	// connection closes immediately — keeping the protocol
	// forward-compatible with daemon builds that do not link the
	// driver event bus.
	Subscribe func(ctx context.Context) (<-chan json.RawMessage, func())

	// Invoke dispatches a typed business RPC. method names a row in
	// the daemon-side dispatch table (e.g. "remote.invite"); body
	// is the JSON-encoded args for that method. The returned bytes
	// are placed verbatim into Response.Result for the CLI to
	// decode into its method-specific result struct.
	//
	// nil handler returns "invoke: no handler" so an old daemon
	// gracefully refuses RPCs from a newer CLI.
	Invoke func(ctx context.Context, method string, body json.RawMessage) (json.RawMessage, error)
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

	// One-shot RPCs use the 5s deadline below. CmdSubscribe is the
	// only streaming command and clears the deadline before
	// entering its push loop — see s.handleSubscribe.
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

	if req.Command == CmdSubscribe {
		s.handleSubscribe(conn, req)
		return
	}

	// CmdInvoke routes typed business RPCs that may touch the
	// driver (e.g. remote.invite pushes a fresh stcp tunnel into
	// frpc) — give those a longer ceiling than the cheap RPCs.
	timeout := 5 * time.Second
	if req.Command == CmdInvoke {
		timeout = 30 * time.Second
	}
	// Reset the read+write deadline so a slow Invoke handler is
	// not killed by the per-conn 5s deadline set above.
	_ = conn.SetDeadline(time.Now().Add(timeout + 5*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

// handleSubscribe drives the streaming path. The flow is:
//
//  1. Drop the per-RPC deadline; the connection now lives until
//     the client closes it (or the daemon shuts down).
//  2. Send a single ack line so the client knows the subscription
//     is live. After this, every newline-delimited Response carries
//     one event in Response.Event.
//  3. Loop, forwarding events with a short per-write deadline so a
//     stuck client cannot wedge the daemon's event bus indefinitely.
//  4. Watch for client disconnect on a background goroutine — we
//     read into a 1-byte sink so EOF flips a flag the writer
//     polls between events. Without this, a client that simply
//     closes its socket would leave the goroutine blocked on the
//     next Publish.
func (s *Server) handleSubscribe(conn net.Conn, req Request) {
	if s.handlers.Subscribe == nil {
		writeErr(conn, "subscribe: no handler")
		return
	}
	_ = conn.SetDeadline(time.Time{}) // clear

	allow := parseTypeFilter(req.Args["type"])
	endpointID := parseUintArg(req.Args["endpoint_id"])
	tunnelID := parseUintArg(req.Args["tunnel_id"])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsub := s.handlers.Subscribe(ctx)
	defer unsub()

	// Ack: tells the CLI "subscription live". Errors here are
	// terminal — if we cannot even send the ack, the connection is
	// already gone.
	if buf, err := Encode(Response{Ok: true}); err == nil {
		if _, err := conn.Write(buf); err != nil {
			return
		}
	}

	// Detect client-side disconnect by reading from the connection
	// in the background; any read activity (including EOF) cancels
	// our context so the writer loop exits.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if !subscribePassesFilter(ev, allow, endpointID, tunnelID) {
				continue
			}
			frame, err := Encode(Response{Ok: true, Event: ev})
			if err != nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := conn.Write(frame); err != nil {
				return
			}
		}
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
	case CmdInvoke:
		return s.handleInvoke(ctx, req)
	default:
		return Response{Ok: false, Error: "unknown command: " + string(req.Command)}
	}
}

// handleInvoke is the generic business-RPC dispatcher. It pulls
// method + body off the request, hands them to the registered Invoke
// handler, and packages the result JSON into the response. Errors
// from the handler are surfaced verbatim — handlers should already
// have shaped their error strings for human consumption (the CLI
// prints them as-is).
func (s *Server) handleInvoke(ctx context.Context, req Request) Response {
	if s.handlers.Invoke == nil {
		return Response{Ok: false, Error: "invoke: no handler"}
	}
	method := strings.TrimSpace(req.Args["method"])
	if method == "" {
		return Response{Ok: false, Error: "invoke: method required"}
	}
	var body json.RawMessage
	if raw := req.Args["body"]; raw != "" {
		body = json.RawMessage(raw)
	}
	result, err := s.handlers.Invoke(ctx, method, body)
	if err != nil {
		return Response{Ok: false, Error: err.Error()}
	}
	return Response{Ok: true, Result: result}
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

// parseTypeFilter splits the "type" arg on subscribe into a
// non-empty allow-list. Empty input returns nil (= "no filter,
// pass everything"); an explicit list of unknown values still
// returns a non-nil set so the comparison is unambiguous.
func parseTypeFilter(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseUintArg returns 0 on absence/parse-failure; subscribe filters
// treat 0 as "no filter on this dimension".
func parseUintArg(raw string) uint {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return uint(v)
}

// subscribePassesFilter decodes just enough of the raw event JSON to
// answer the per-subscriber filter questions, then re-uses the raw
// bytes for the wire frame. Decoding into a private struct avoids
// pulling in the frpcd type from this layer.
func subscribePassesFilter(raw json.RawMessage, allow map[string]struct{}, endpointID, tunnelID uint) bool {
	if len(allow) == 0 && endpointID == 0 && tunnelID == 0 {
		return true
	}
	var hdr struct {
		Type       string `json:"type"`
		EndpointID uint   `json:"endpoint_id,omitempty"`
		TunnelID   uint   `json:"tunnel_id,omitempty"`
	}
	if err := json.Unmarshal(raw, &hdr); err != nil {
		return true // malformed JSON: pass through, CLI surfaces it
	}
	if len(allow) > 0 {
		if _, ok := allow[hdr.Type]; !ok {
			return false
		}
	}
	if endpointID != 0 && hdr.EndpointID != endpointID {
		return false
	}
	if tunnelID != 0 && hdr.TunnelID != tunnelID {
		return false
	}
	return true
}
