// Client side of the control channel — the CLI-facing half.
//
// The CLI uses Client to issue at most one RPC per command-line
// invocation, so we treat every call as fire-and-forget: dial,
// write request, read response, close. No connection pooling, no
// background reconnect, no retry — failure is bubbled up to the
// CLI verbatim so the operator sees exactly what happened.

package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is a one-shot dialer scoped to a single FrpDeck data
// directory. Construct one per CLI invocation; do not share between
// goroutines (it is technically safe — there is no shared mutable
// state — but the API is shaped for sequential use).
type Client struct {
	socketPath string
}

// NewClient builds a Client that will dial <dataDir>/<SocketFilename>.
// Validating the data directory is the caller's job; we leave it to
// the dial step so the CLI can present a single coherent error
// (rather than two separate "data dir missing" / "socket missing"
// failures).
func NewClient(dataDir string) *Client {
	return &Client{socketPath: filepath.Join(dataDir, SocketFilename)}
}

// SocketPath exposes the path the client will dial. Used by
// `frpdeck doctor` to render the actual location it tried.
func (c *Client) SocketPath() string { return c.socketPath }

// SocketExists reports whether the daemon's socket file is on disk.
// On Unix this is the cheapest way to give the doctor command a
// "daemon running / not running" answer without paying the cost of
// a full dial; on Windows the check always returns true (named
// pipes are not file-system-backed) and the caller falls through
// to a real Ping.
func (c *Client) SocketExists() bool {
	if runtimeIsWindows() {
		return true
	}
	_, err := os.Stat(c.socketPath)
	return err == nil
}

// Ping issues CmdPing and returns the daemon's version + listen
// address. Used by `frpdeck doctor` and as a precondition check
// before commands that require the daemon to be running (e.g.
// the CLI helper that tells the daemon to reconcile).
//
// On socket-not-found we return a sentinel error so callers can
// distinguish "daemon not running" from "daemon misbehaving" and
// degrade gracefully (most CLI commands continue Direct-DB even
// when Ping fails).
func (c *Client) Ping(ctx context.Context) (version, listen string, err error) {
	resp, err := c.do(ctx, Request{Command: CmdPing})
	if err != nil {
		return "", "", err
	}
	return resp.Data["version"], resp.Data["listen"], nil
}

// Reconcile asks the daemon to immediately re-run the lifecycle
// reconciliation. The CLI fires this after every endpoint/tunnel
// mutation so the user does not have to wait for the 30s tick.
//
// If the daemon is not running this returns ErrDaemonNotRunning;
// callers should treat that as "non-fatal — your DB change is
// persisted, just won't take effect until the daemon starts".
func (c *Client) Reconcile(ctx context.Context) error {
	_, err := c.do(ctx, Request{Command: CmdReconcile})
	return err
}

// ReloadRuntime asks the daemon to pull fresh runtime.Settings from
// SQLite. Same not-running semantics as Reconcile.
func (c *Client) ReloadRuntime(ctx context.Context) error {
	_, err := c.do(ctx, Request{Command: CmdReloadRuntime})
	return err
}

// Shutdown asks the daemon to begin a graceful shutdown. Reserved
// for an explicit CLI command later; not exposed yet but defined
// here so the wire protocol has full coverage.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.do(ctx, Request{Command: CmdShutdown})
	return err
}

// SubscribeOptions narrows the event stream daemon-side. All fields
// are optional; zero values mean "no filter on this dimension".
type SubscribeOptions struct {
	// Types restricts which EventType values are forwarded. An empty
	// slice means "all types".
	Types []string

	// EndpointID restricts events to a single endpoint. 0 = all.
	EndpointID uint

	// TunnelID restricts events to a single tunnel. 0 = all.
	TunnelID uint
}

// Subscribe opens a streaming connection and returns a channel of
// raw event payloads (as JSON bytes). The CLI is responsible for
// decoding into the concrete frpcd.Event struct so this package
// stays free of a frpcd import.
//
// The returned cancel function closes the underlying connection,
// terminating the daemon-side push loop. Cancelling ctx has the
// same effect (a watcher goroutine watches ctx.Done()).
//
// If the daemon is not running the call returns ErrDaemonNotRunning;
// the channel is never produced.
func (c *Client) Subscribe(ctx context.Context, opts SubscribeOptions) (<-chan json.RawMessage, func(), error) {
	if !c.SocketExists() {
		return nil, nil, ErrDaemonNotRunning
	}
	conn, err := dialSocket(c.socketPath)
	if err != nil {
		if isNotRunningErr(err) {
			return nil, nil, ErrDaemonNotRunning
		}
		return nil, nil, err
	}

	args := map[string]string{}
	if len(opts.Types) > 0 {
		args["type"] = strings.Join(opts.Types, ",")
	}
	if opts.EndpointID != 0 {
		args["endpoint_id"] = strconv.FormatUint(uint64(opts.EndpointID), 10)
	}
	if opts.TunnelID != 0 {
		args["tunnel_id"] = strconv.FormatUint(uint64(opts.TunnelID), 10)
	}

	buf, err := Encode(Request{Command: CmdSubscribe, Args: args})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	// Allow up to 5s for the server to ack the subscription. Once
	// the ack is in we clear the deadline so events can stream
	// without artificial timeouts.
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("write subscribe: %w", err)
	}
	_ = conn.SetWriteDeadline(time.Time{})

	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	ackLine, err := r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read ack: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	var ack Response
	if err := json.Unmarshal([]byte(ackLine), &ack); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("decode ack: %w", err)
	}
	if !ack.Ok {
		conn.Close()
		return nil, nil, errors.New(ack.Error)
	}

	out := make(chan json.RawMessage, 64)
	cancelOnce := func() func() {
		var once sync.Once
		return func() { once.Do(func() { conn.Close() }) }
	}()

	go func() {
		<-ctx.Done()
		cancelOnce()
	}()

	go func() {
		defer close(out)
		defer cancelOnce()
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			var resp Response
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if !resp.Ok {
				return
			}
			if len(resp.Event) == 0 {
				continue
			}
			select {
			case out <- resp.Event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, cancelOnce, nil
}

// ErrDaemonNotRunning is returned when the socket file is not on
// disk (Unix) or the named pipe refuses connections (Windows).
// Callers test with errors.Is.
var ErrDaemonNotRunning = errors.New("frpdeck daemon is not running")

// do is the shared dial/write/read loop. The deadline is per-RPC
// rather than per-call-stack so a slow ctx does not gum up the
// underlying connection.
func (c *Client) do(ctx context.Context, req Request) (*Response, error) {
	if !c.SocketExists() {
		return nil, ErrDaemonNotRunning
	}

	conn, err := dialSocket(c.socketPath)
	if err != nil {
		// Treat ENOENT / connection-refused as "daemon down" so
		// callers can detect it cleanly. Other errors keep their
		// original message.
		if isNotRunningErr(err) {
			return nil, ErrDaemonNotRunning
		}
		return nil, err
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	buf, err := Encode(req)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(buf); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !resp.Ok {
		return &resp, errors.New(resp.Error)
	}
	return &resp, nil
}

// isNotRunningErr inspects a dial error and returns true when it
// corresponds to "nobody is listening". The exact wording differs
// between platforms (ECONNREFUSED on Linux/macOS, "no such file"
// when the socket has been cleaned up, "pipe not found" on
// Windows), so we match on substrings rather than typed errors.
func isNotRunningErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such file"),
		strings.Contains(msg, "cannot find the file"),
		strings.Contains(msg, "the system cannot find"),
		strings.Contains(msg, "pipe not found"),
		strings.Contains(msg, "no such device"):
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" && opErr.Err != nil {
			inner := strings.ToLower(opErr.Err.Error())
			if strings.Contains(inner, "refused") || strings.Contains(inner, "no such") {
				return true
			}
		}
	}
	return false
}
