package cmds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/model"
)

// NewWatchCmd builds the `frpdeck watch` command tree. Two flavours:
//
//	frpdeck watch endpoints
//	frpdeck watch tunnels
//
// Both use the canonical clear-screen ANSI sequence (no full
// alt-screen switch — that breaks ttys without termcap) on a tick,
// and exit cleanly on Ctrl+C. The renderer reuses endpointRow /
// tunnelRow so the watch view stays in lockstep with `endpoint
// list` / `tunnel list`.
//
// Why poll instead of subscribe: SQLite is the source of truth for
// status and is updated by the lifecycle reconciler within seconds
// of any state change. A subscribe-driven watch would have to
// re-render every time a state event arrives, which is more
// complicated for no real benefit on this refresh cadence.
func NewWatchCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "watch",
		Short: "Live-refresh views of endpoints / tunnels",
	}
	c.AddCommand(newWatchEndpointsCmd(opts), newWatchTunnelsCmd(opts))
	return c
}

func newWatchEndpointsCmd(opts *GlobalOptions) *cobra.Command {
	var interval time.Duration
	c := &cobra.Command{
		Use:   "endpoints",
		Short: "Live-refresh endpoint table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatch(cmd, opts, interval, func(w io.Writer) error {
				st, closer, err := opts.OpenStore()
				if err != nil {
					return err
				}
				defer closer()
				eps, err := st.ListEndpoints()
				if err != nil {
					return err
				}
				rows := make([]map[string]any, len(eps))
				for i, e := range eps {
					rows[i] = endpointRow(&e)
				}
				cols := []output.Column{
					{Title: "ID", Key: "id"},
					{Title: "NAME", Key: "name"},
					{Title: "ADDR", Key: "addr"},
					{Title: "DRIVER", Key: "driver_mode"},
					{Title: "ENABLED", Key: "enabled"},
					{Title: "AUTO", Key: "auto_start"},
					{Title: "GROUP", Key: "group"},
				}
				return output.Render(w, output.FormatTable, cols, rows, opts.RenderOpts()...)
			})
		},
	}
	c.Flags().DurationVar(&interval, "interval", 5*time.Second, "Refresh interval")
	return c
}

func newWatchTunnelsCmd(opts *GlobalOptions) *cobra.Command {
	var interval time.Duration
	var endpointRef string
	c := &cobra.Command{
		Use:   "tunnels",
		Short: "Live-refresh tunnel table",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatch(cmd, opts, interval, func(w io.Writer) error {
				st, closer, err := opts.OpenStore()
				if err != nil {
					return err
				}
				defer closer()
				endpointNames, err := buildEndpointNameMap(st)
				if err != nil {
					return err
				}
				var tunnels []model.Tunnel
				if endpointRef != "" {
					ep, err := resolveEndpoint(st, endpointRef)
					if err != nil {
						return err
					}
					tunnels, err = st.ListTunnelsByEndpoint(ep.ID)
					if err != nil {
						return err
					}
				} else {
					tunnels, err = st.ListTunnels()
					if err != nil {
						return err
					}
				}
				rows := make([]map[string]any, len(tunnels))
				for i := range tunnels {
					rows[i] = tunnelRow(&tunnels[i], endpointNames)
				}
				cols := []output.Column{
					{Title: "ID", Key: "id"},
					{Title: "ENDPOINT", Key: "endpoint"},
					{Title: "NAME", Key: "name"},
					{Title: "TYPE", Key: "type"},
					{Title: "LOCAL", Key: "local"},
					{Title: "REMOTE", Key: "remote"},
					{Title: "STATUS", Key: "status"},
					{Title: "ENABLED", Key: "enabled"},
					{Title: "EXPIRE", Key: "expire_at"},
				}
				return output.Render(w, output.FormatTable, cols, rows, opts.RenderOpts()...)
			})
		},
	}
	c.Flags().DurationVar(&interval, "interval", 5*time.Second, "Refresh interval")
	c.Flags().StringVar(&endpointRef, "endpoint", "", "Filter to a single endpoint (id or name)")
	return c
}

// runWatch is the shared refresh loop. It clears the screen with the
// canonical ANSI sequence (ESC[H ESC[2J) before each render — works
// on every halfway-modern terminal we care about (xterm, kitty,
// Windows Terminal, gnome-terminal, ConEmu) without alt-screen
// switching. We deliberately avoid termui / tcell because (a) those
// pull in 1-2MB of additional binary, and (b) FrpDeck CLI users are
// far more likely to want to scroll back to a previous frame than
// they are to want full-screen interactivity.
func runWatch(cmd *cobra.Command, opts *GlobalOptions, interval time.Duration, render func(io.Writer) error) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	out := cmd.OutOrStdout()
	tick := time.NewTicker(interval)
	defer tick.Stop()

	if err := watchFrame(out, opts, render); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "")
			return nil
		case <-tick.C:
			if err := watchFrame(out, opts, render); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		}
	}
}

// watchFrame clears the screen and renders one frame. Header comes
// from the watch loop so per-view renderers stay generic.
func watchFrame(out io.Writer, opts *GlobalOptions, render func(io.Writer) error) error {
	if isInteractiveTTY(out) {
		fmt.Fprint(out, "\x1b[H\x1b[2J")
	}
	fmt.Fprintf(out, "frpdeck watch — %s — data-dir %s — Ctrl+C to exit\n\n",
		time.Now().Format("2006-01-02 15:04:05"), opts.DataDir)
	return render(out)
}

// isInteractiveTTY decides whether to emit clear-screen ANSI codes.
// When the output is not a real TTY (piped to a file / wc / less) we
// scroll instead so the file becomes a scrolling log of frames.
func isInteractiveTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}
