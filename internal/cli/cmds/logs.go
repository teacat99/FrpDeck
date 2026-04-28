package cmds

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// NewLogsCmd builds the `frpdeck logs` command.
//
// Two modes:
//
//	frpdeck logs               # one-shot: render the events that
//	                             show up within --window (default
//	                             100ms) and exit. Useful for scripts
//	                             that want a quick "anything live?"
//	                             check without committing to a
//	                             follow loop.
//	frpdeck logs --follow      # stream until Ctrl+C. Same wire
//	                             protocol; just drops the window
//	                             timer and waits for SIGINT.
//
// The streaming machinery lives in internal/control; this file only
// shapes the user-facing knobs and the human-readable rendering.
func NewLogsCmd(opts *GlobalOptions) *cobra.Command {
	var (
		follow      bool
		typesCSV    string
		endpointRef string
		tunnelRef   string
		since       time.Duration
		windowDur   time.Duration
		level       string
	)
	c := &cobra.Command{
		Use:   "logs",
		Short: "Stream driver events (logs / state / expiring) from the daemon",
		Long: `Subscribes to the daemon's event bus over the local control socket.
Without --follow the command renders whatever shows up inside --window
(default 100ms) and exits, which makes it scriptable. With --follow it
runs until you Ctrl+C.

Filters --type / --endpoint / --tunnel are applied daemon-side so a
chatty bus does not flood the socket. --level filters log lines by
level (info/warn/error) on the CLI side.

--since "5m" replays nothing — the event bus is forward-only — but
the flag is reserved for forward compatibility once the daemon grows
a ring buffer.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, closer, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer closer()

			subOpts := control.SubscribeOptions{}
			if t := strings.TrimSpace(typesCSV); t != "" {
				for _, part := range strings.Split(t, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					subOpts.Types = append(subOpts.Types, part)
				}
			}
			if endpointRef != "" {
				ep, err := resolveEndpoint(st, endpointRef)
				if err != nil {
					return err
				}
				subOpts.EndpointID = ep.ID
			}
			if tunnelRef != "" {
				tn, err := resolveTunnel(st, tunnelRef)
				if err != nil {
					return err
				}
				subOpts.TunnelID = tn.ID
			}
			levelLower := strings.ToLower(strings.TrimSpace(level))
			if since > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "logs: --since is reserved for the future event ring buffer; ignored for now")
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			events, cancelSub, err := opts.SocketClient.Subscribe(ctx, subOpts)
			if err != nil {
				if errors.Is(err, control.ErrDaemonNotRunning) {
					return errors.New("daemon is not running; logs only stream when frpdeck-server is up")
				}
				return err
			}
			defer cancelSub()

			endpointNames, err := buildEndpointNameMap(st)
			if err != nil {
				return err
			}
			tunnelNames := map[uint]string{}
			if tunnels, err := st.ListTunnels(); err == nil {
				for _, t := range tunnels {
					tunnelNames[t.ID] = t.Name
				}
			}

			renderer := pickLogsRenderer(opts.Format)
			var deadline <-chan time.Time
			if !follow {
				if windowDur <= 0 {
					windowDur = 100 * time.Millisecond
				}
				deadline = time.After(windowDur)
			}

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-deadline:
					return nil
				case raw, ok := <-events:
					if !ok {
						if follow {
							return errors.New("daemon closed the subscription")
						}
						return nil
					}
					var ev frpcd.Event
					if err := json.Unmarshal(raw, &ev); err != nil {
						continue
					}
					if levelLower != "" && string(ev.Type) == "log" && !strings.EqualFold(ev.Level, levelLower) {
						continue
					}
					if err := renderer(cmd.OutOrStdout(), &ev, endpointNames, tunnelNames); err != nil {
						return err
					}
				}
			}
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "Stream until Ctrl+C (otherwise exit after --window)")
	c.Flags().StringVar(&typesCSV, "type", "", "Comma-separated EventType allow-list: log,tunnel_state,endpoint_state,tunnel_expiring")
	c.Flags().StringVar(&endpointRef, "endpoint", "", "Filter to a single endpoint (id or name)")
	c.Flags().StringVar(&tunnelRef, "tunnel", "", "Filter to a single tunnel (id or [endpoint/]name)")
	c.Flags().StringVar(&level, "level", "", "Filter log lines by level: info | warn | error (CLI-side filter)")
	c.Flags().DurationVar(&since, "since", 0, "(reserved) replay events newer than this duration")
	c.Flags().DurationVar(&windowDur, "window", 100*time.Millisecond, "One-shot mode: how long to listen before exiting")
	return c
}

// pickLogsRenderer returns the per-event formatter for the requested
// global output format. table / "" go through a coloured human
// renderer; json/yaml emit one structured record per line so logs
// can pipe into jq / yq without buffering.
func pickLogsRenderer(format output.Format) func(io.Writer, *frpcd.Event, map[uint]string, map[uint]string) error {
	switch format {
	case output.FormatJSON:
		return renderLogJSON
	case output.FormatYAML:
		return renderLogYAML
	default:
		return renderLogHuman
	}
}

func renderLogHuman(w io.Writer, ev *frpcd.Event, endpoints, tunnels map[uint]string) error {
	ts := ev.At.Local().Format("15:04:05.000")
	tag, color := eventTag(ev)
	scope := scopeLabel(ev, endpoints, tunnels)
	body := eventBody(ev)
	if scope != "" {
		_, err := fmt.Fprintf(w, "%s %s%s%s %s %s\n", ts, color, tag, ansiReset, scope, body)
		return err
	}
	_, err := fmt.Fprintf(w, "%s %s%s%s %s\n", ts, color, tag, ansiReset, body)
	return err
}

func renderLogJSON(w io.Writer, ev *frpcd.Event, _, _ map[uint]string) error {
	buf, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(buf))
	return err
}

func renderLogYAML(w io.Writer, ev *frpcd.Event, _, _ map[uint]string) error {
	// Custom mini-YAML: avoids pulling in yaml.v3 just for this
	// path and matches the tabular --type=log shape.
	parts := []string{}
	parts = append(parts, fmt.Sprintf("at: %q", ev.At.Format(time.RFC3339Nano)))
	parts = append(parts, fmt.Sprintf("type: %s", ev.Type))
	if ev.EndpointID != 0 {
		parts = append(parts, fmt.Sprintf("endpoint_id: %d", ev.EndpointID))
	}
	if ev.TunnelID != 0 {
		parts = append(parts, fmt.Sprintf("tunnel_id: %d", ev.TunnelID))
	}
	if ev.State != "" {
		parts = append(parts, fmt.Sprintf("state: %s", ev.State))
	}
	if ev.Level != "" {
		parts = append(parts, fmt.Sprintf("level: %s", ev.Level))
	}
	if ev.Msg != "" {
		parts = append(parts, fmt.Sprintf("msg: %q", ev.Msg))
	}
	if ev.Err != "" {
		parts = append(parts, fmt.Sprintf("err: %q", ev.Err))
	}
	_, err := fmt.Fprintln(w, "- "+strings.Join(parts, ", "))
	return err
}

// eventTag picks a fixed-width label + ANSI colour per EventType so
// the human-readable stream skim-reads at a glance. We pad to 6
// characters because that fits "EXPIRE" / "STATE " comfortably.
func eventTag(ev *frpcd.Event) (string, string) {
	switch ev.Type {
	case frpcd.EventLog:
		switch strings.ToLower(ev.Level) {
		case "error":
			return "ERROR ", ansiRed
		case "warn", "warning":
			return "WARN  ", ansiYellow
		default:
			return "INFO  ", ansiCyan
		}
	case frpcd.EventTunnelState:
		return "TUN   ", ansiGreen
	case frpcd.EventEndpointState:
		return "EP    ", ansiBlue
	case frpcd.EventTunnelExpiring:
		return "EXPIRE", ansiMagenta
	default:
		return "?     ", ""
	}
}

// scopeLabel renders "[endpoint/tunnel]" or "[endpoint]" depending
// on which IDs are present. Falls back to numeric IDs when names
// are unknown (e.g. tunnel created mid-stream).
func scopeLabel(ev *frpcd.Event, endpoints, tunnels map[uint]string) string {
	switch {
	case ev.EndpointID != 0 && ev.TunnelID != 0:
		ep := endpoints[ev.EndpointID]
		if ep == "" {
			ep = fmt.Sprintf("ep#%d", ev.EndpointID)
		}
		tn := tunnels[ev.TunnelID]
		if tn == "" {
			tn = fmt.Sprintf("t#%d", ev.TunnelID)
		}
		return fmt.Sprintf("[%s/%s]", ep, tn)
	case ev.EndpointID != 0:
		ep := endpoints[ev.EndpointID]
		if ep == "" {
			ep = fmt.Sprintf("ep#%d", ev.EndpointID)
		}
		return fmt.Sprintf("[%s]", ep)
	case ev.TunnelID != 0:
		tn := tunnels[ev.TunnelID]
		if tn == "" {
			tn = fmt.Sprintf("t#%d", ev.TunnelID)
		}
		return fmt.Sprintf("[%s]", tn)
	default:
		return ""
	}
}

// eventBody picks the most informative free-text field for a given
// event. Log events use Msg; state events use State + optional Err.
func eventBody(ev *frpcd.Event) string {
	switch ev.Type {
	case frpcd.EventLog:
		return ev.Msg
	case frpcd.EventTunnelExpiring:
		return fmt.Sprintf("%s seconds left", ev.State)
	default:
		if ev.Err != "" {
			return fmt.Sprintf("%s (%s)", ev.State, ev.Err)
		}
		return ev.State
	}
}

// ANSI colour codes. We deliberately keep the palette small (5
// colours + reset) because the goal is "easier to skim", not
// "syntax highlighted code". Emit them unconditionally — most
// terminals strip them when redirected to a file, and the JSON /
// YAML formatters skip the human renderer entirely.
const (
	ansiReset   = "\x1b[0m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)
