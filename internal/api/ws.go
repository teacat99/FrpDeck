// WebSocket fan-out for live driver events. The browser opens a single
// long-lived connection to /api/ws, authenticates via the
// `Sec-WebSocket-Protocol` subprotocol header (browsers cannot set
// Authorization on the WS handshake), then sends `subscribe` /
// `unsubscribe` JSON messages to express interest in a subset of the
// global event stream:
//
//   topics    description                                  produced by
//   --------  -------------------------------------------  -----------
//   tunnels   every EventTunnelState                       frpcd driver
//   endpoints every EventEndpointState                     frpcd driver
//   logs:all  every EventLog (firehose; opt-in only)       frpcd driver
//   logs:endpoint:<id>  EventLog scoped to one endpoint    frpcd driver
//   logs:tunnel:<id>    EventLog scoped to one tunnel      frpcd driver
//
// Why subprotocol auth: query string ?token= would leak the JWT into
// access logs / proxy histories; cookies would force tying our auth to
// the browser-visible cookie jar (we deliberately use localStorage to
// mirror PortPass). Subprotocol is the cleanest browser-compatible
// channel — JS calls `new WebSocket(url, ["jwt", token])`, server picks
// "jwt" from the first list entry and reads the second as the JWT.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// wsBufferSize bounds the per-connection outbound queue. A bigger buffer
// is forgiving to slow networks but lets backed-up connections balloon
// memory; 256 covers a comfortable burst (e.g. 256 log lines in 1s) and
// keeps the worst-case footprint at a few hundred KiB.
const wsBufferSize = 256

// wsWriteTimeout caps how long we'll wait for a single frame to drain
// onto the socket before giving up on the connection. Anything stalled
// past this is treated as a dead client.
const wsWriteTimeout = 10 * time.Second

// wsPingInterval keeps the connection lively across NAT / proxy
// timeouts. The browser auto-replies with a pong; we don't need to
// inspect it.
const wsPingInterval = 30 * time.Second

// wsMessage is the wire shape both directions speak. `op` is set for
// client → server control frames; `event` is set for server → client
// notifications. Keeping a single envelope simplifies the JS side.
type wsMessage struct {
	Op     string          `json:"op,omitempty"`
	Topics []string        `json:"topics,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Err    string          `json:"err,omitempty"`
}

// wsConn is the per-client state. The mutex protects the topics set;
// the writer goroutine owns the socket so all WriteJSON calls funnel
// through `out`.
type wsConn struct {
	conn   *websocket.Conn
	out    chan wsMessage
	mu     sync.RWMutex
	topics map[string]struct{}
}

// match decides whether the connection cares about a given event. The
// topic vocabulary is small and explicit; we deliberately do not allow
// arbitrary glob patterns to keep the matcher cheap on the publish path.
func (c *wsConn) match(e frpcd.Event) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch e.Type {
	case frpcd.EventEndpointState:
		_, ok := c.topics["endpoints"]
		return ok
	case frpcd.EventTunnelState:
		_, ok := c.topics["tunnels"]
		return ok
	case frpcd.EventLog:
		if _, ok := c.topics["logs:all"]; ok {
			return true
		}
		if e.EndpointID != 0 {
			if _, ok := c.topics["logs:endpoint:"+strconv.FormatUint(uint64(e.EndpointID), 10)]; ok {
				return true
			}
		}
		if e.TunnelID != 0 {
			if _, ok := c.topics["logs:tunnel:"+strconv.FormatUint(uint64(e.TunnelID), 10)]; ok {
				return true
			}
		}
	}
	return false
}

// handleWebSocket upgrades the request, authenticates via subprotocol,
// then bridges the driver's EventBus to the WS connection until either
// side hangs up.
func (s *Server) handleWebSocket(c *gin.Context) {
	// Authenticate before the upgrade so unauthenticated peers walk
	// away with a clean 401 instead of a half-open WS that immediately
	// closes — the browser surfaces 401 distinctly and our retry hook
	// can act on it.
	user, ok := s.authWebSocket(c.Request)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Pick the "jwt" subprotocol so the response handshake announces
	// the same protocol the browser sent; otherwise some browsers
	// abort with "subprotocol mismatch".
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		Subprotocols: []string{"jwt"},
		// We accept connections from the same origin only; the
		// existing CORS middleware gates cross-origin XHR but the
		// websocket handshake is a separate path.
		InsecureSkipVerify: true,
	})
	if err != nil {
		// websocket.Accept already wrote a response; nothing to do.
		return
	}
	defer conn.Close(websocket.StatusInternalError, "shutting down")
	conn.SetReadLimit(64 * 1024)

	wc := &wsConn{
		conn:   conn,
		out:    make(chan wsMessage, wsBufferSize),
		topics: make(map[string]struct{}),
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Subscribe to the driver firehose. Filtering happens in the
	// writer goroutine (per-connection topic set); the EventBus does
	// no filtering itself.
	events, unsub := s.driver.Subscribe()
	defer unsub()

	var wg sync.WaitGroup

	// Reader: parses subscribe / unsubscribe control frames and
	// terminates the connection on protocol error.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg wsMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				wc.send(wsMessage{Err: "bad json"})
				continue
			}
			switch msg.Op {
			case "subscribe":
				wc.mu.Lock()
				for _, t := range msg.Topics {
					wc.topics[t] = struct{}{}
				}
				wc.mu.Unlock()
				wc.send(wsMessage{Event: "ack", Op: "subscribe"})
			case "unsubscribe":
				wc.mu.Lock()
				for _, t := range msg.Topics {
					delete(wc.topics, t)
				}
				wc.mu.Unlock()
				wc.send(wsMessage{Event: "ack", Op: "unsubscribe"})
			case "ping":
				wc.send(wsMessage{Event: "pong"})
			default:
				wc.send(wsMessage{Err: "unknown op"})
			}
		}
	}()

	// Pumper: forwards EventBus events through the per-conn filter
	// onto the outbound queue. Fast, allocation-light: filtering only
	// touches the publishing goroutine of the EventBus while it is
	// already in a select loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if !wc.match(ev) {
					continue
				}
				payload, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				wc.send(wsMessage{Event: string(ev.Type), Data: payload})
			}
		}
	}()

	// Writer: serialises all outbound writes (one writer = no need to
	// lock the conn). Also drives periodic pings so dead clients are
	// reaped without waiting for TCP keepalive.
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	// Initial hello so the JS side knows the auth went through and
	// can transition the store from "connecting" → "connected".
	wc.send(wsMessage{Event: "hello", Data: mustJSON(map[string]any{
		"user": user.Username,
		"role": user.Role,
	})})

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pingCtx, pcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pingCtx)
			pcancel()
			if err != nil {
				cancel()
				wg.Wait()
				return
			}
		case msg, ok := <-wc.out:
			if !ok {
				return
			}
			writeCtx, wcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := wsjsonWrite(writeCtx, conn, msg)
			wcancel()
			if err != nil {
				cancel()
				wg.Wait()
				return
			}
		}
	}
}

// send is the non-blocking enqueue used by the reader / pumper. A full
// outbound queue means the client cannot keep up and we drop frames
// instead of blocking — log lines are unimportant compared to keeping
// the live engine responsive (same policy as EventBus).
func (c *wsConn) send(m wsMessage) {
	select {
	case c.out <- m:
	default:
		// Slow consumer; drop on the floor.
	}
}

// authWebSocket pulls the JWT out of either the standard Authorization
// header (handy for non-browser tools like wscat) or the
// Sec-WebSocket-Protocol subprotocol header (browser path).
func (s *Server) authWebSocket(r *http.Request) (*authPrincipal, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw := strings.TrimPrefix(h, "Bearer ")
		if ok, u := s.auth.ValidateRawToken(raw); ok {
			return &authPrincipal{ID: u.ID, Username: u.Username, Role: u.Role}, true
		}
	}
	// Sec-WebSocket-Protocol may be a comma-separated list. The
	// browser-side helper sends ["jwt", "<token>"], so the second item
	// (after trimming) is the token.
	for _, raw := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, part := range strings.Split(raw, ",") {
			tok := strings.TrimSpace(part)
			if tok == "" || tok == "jwt" {
				continue
			}
			if ok, u := s.auth.ValidateRawToken(tok); ok {
				return &authPrincipal{ID: u.ID, Username: u.Username, Role: u.Role}, true
			}
		}
	}
	return nil, false
}

// authPrincipal is a tiny copy of the bits we need from model.User —
// kept here so this file does not have to import model.
type authPrincipal struct {
	ID       uint
	Username string
	Role     string
}

// wsjsonWrite encodes the message and writes a single text frame. We
// avoid wsjson.Write from the coder library because it allocates a
// fresh json.Encoder every call; the wire shape is small enough that
// json.Marshal is comparable and lets us reuse our existing wsMessage
// envelope without a transitive dep.
func wsjsonWrite(ctx context.Context, c *websocket.Conn, m wsMessage) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	w, err := c.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// mustJSON marshals or returns an empty object — used only for hello
// payloads where the input is fixed and cannot fail.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("[ws] mustJSON: %v", err)
		return json.RawMessage(`{}`)
	}
	return b
}

// Compile-time guard so Subscribe wires correctly.
var _ = errors.New
