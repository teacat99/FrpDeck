// Package control implements the FrpDeck local control channel.
//
// The control channel is the bridge that lets the standalone `frpdeck`
// CLI poke the running daemon — without going through HTTP, without
// minting a JWT, without opening another TCP port. The daemon listens
// on a Unix domain socket (Linux/macOS) or a named pipe (Windows) and
// the CLI dials the same path; access control is delegated to the
// operating system via 0600 file permissions on the socket.
//
// The channel is intentionally narrow. CLI commands that *only* need to
// read or mutate the SQLite database open the database directly via
// `internal/cli/dbopen`; the control channel is reserved for telling
// the running engine to pick up state changes (e.g. after the CLI
// flipped `endpoints.enabled`). It also serves as a liveness probe so
// the CLI can give a clean "daemon running / not running" answer in
// `frpdeck doctor`.
//
// Wire protocol: one JSON request per line, one JSON response per
// line. UTF-8 only. The protocol is deliberately ad-hoc rather than
// gRPC/JSON-RPC because (a) the surface is tiny — five commands and
// growing slowly, and (b) every dependency added here also gets
// shipped in the daemon binary, so we keep it stdlib-only.
package control

import "encoding/json"

// SocketFilename is the well-known basename of the control socket
// inside the FrpDeck data directory. We keep the path under DataDir
// rather than `/run/frpdeck.sock` because the data directory is the
// only filesystem location guaranteed writable across every supported
// deployment (Docker, NAS, headless Linux, Windows %ProgramData%).
const SocketFilename = "frpdeck.sock"

// Command is the discriminator field on every Request. Keep this
// list short — additions cost backwards compatibility forever.
type Command string

const (
	// CmdPing is a liveness check. Daemons answer with their version
	// + listen address so callers can render a one-line status without
	// follow-up queries.
	CmdPing Command = "ping"

	// CmdReconcile asks the daemon to immediately re-run
	// lifecycle.Reconcile(), which diffs SQLite state against the
	// running driver and starts/stops tunnels accordingly. The CLI
	// fires this after every endpoint/tunnel mutation so the user
	// does not have to wait for the 30s tick.
	CmdReconcile Command = "reconcile"

	// CmdReloadRuntime asks the daemon to reload runtime.Settings
	// from the SQLite KV table. The CLI fires this after `runtime
	// set <key> <value>` so RateLimit / RetentionDays / etc. take
	// effect without restarting the service.
	CmdReloadRuntime Command = "reload_runtime"

	// CmdShutdown asks the daemon to begin a graceful shutdown.
	// Reserved — not exposed in the CLI yet — but defined here so
	// the wire protocol stays forward-compatible.
	CmdShutdown Command = "shutdown"

	// CmdSubscribe opens a one-way streaming channel: the daemon
	// writes one Response line per event until the client closes
	// the connection. Used by `frpdeck logs --follow` and the
	// internal poll loop of `frpdeck watch`.
	//
	// Filters live in Request.Args:
	//   - "type":        comma-separated EventType allow-list
	//                    (e.g. "log,tunnel_state"); empty = all
	//   - "endpoint_id": numeric filter; 0/empty = all endpoints
	//   - "tunnel_id":   numeric filter; 0/empty = all tunnels
	// Filtering happens daemon-side so noisy buses do not flood
	// the socket; the client trusts the daemon's filter output.
	CmdSubscribe Command = "subscribe"
)

// Request is a single line on the wire from CLI -> daemon.
//
// Args is intentionally a free-form string map: most commands need
// zero arguments, the few that do (e.g. a future "logs subscribe")
// can carry filters without forcing a typed payload per command.
type Request struct {
	Command Command           `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// Response is a single line on the wire from daemon -> CLI.
//
// On success Ok=true and Data carries any payload (e.g. version
// string for ping). On failure Ok=false and Error explains why; the
// CLI surfaces this to the user verbatim.
//
// Event is set on streaming responses (CmdSubscribe) and carries
// the raw JSON of one frpcd.Event so the control package itself
// does not need to depend on internal/frpcd. CLI consumers decode
// Event into the concrete Event struct via json.Unmarshal.
type Response struct {
	Ok    bool              `json:"ok"`
	Error string            `json:"error,omitempty"`
	Data  map[string]string `json:"data,omitempty"`
	Event json.RawMessage   `json:"event,omitempty"`
}

// Encode is a tiny helper so callers do not have to remember to
// append a newline — every wire frame must end in '\n' so the
// receiving side's bufio.Scanner / ReadString('\n') terminates.
func Encode(v any) ([]byte, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}
