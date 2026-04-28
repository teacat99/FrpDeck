// Package cli wires the cobra command tree for the standalone
// `frpdeck` CLI. The top-level binary in cmd/cli/main.go is a thin
// shim that calls into NewRootCmd; everything else — flag parsing,
// subcommand registration, output rendering, control-channel
// plumbing — lives here so it can be unit-tested without spinning up
// an external process.
package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/cmds"
	"github.com/teacat99/FrpDeck/internal/cli/dbopen"
	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// Version is overridden at link time via
//
//	-ldflags "-X 'github.com/teacat99/FrpDeck/internal/cli.Version=v0.6.0'"
//
// so distributors stamp the CLI binary the same way they stamp
// frpdeck-server. Source-built binaries fall back to "dev" plus the
// debug.ReadBuildInfo module string.
var Version = "dev"

// NewRootCmd assembles the full command tree. Tests construct one
// per case and call Execute() on a captured I/O pair to inspect
// behaviour without process boundaries.
func NewRootCmd() *cobra.Command {
	opts := &cmds.GlobalOptions{}

	root := &cobra.Command{
		Use:           "frpdeck",
		Short:         "FrpDeck local CLI",
		Long:          longDescription,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			format, err := output.ParseFormat(opts.Output)
			if err != nil {
				return err
			}
			opts.Format = format
			opts.DataDir = dbopen.ResolveDir(opts.DataDir)
			opts.SocketClient = control.NewClient(opts.DataDir)
			return nil
		},
	}

	root.PersistentFlags().StringVar(&opts.DataDir, "data-dir", "", fmt.Sprintf("FrpDeck data directory (default %q; honours $FRPDECK_DATA_DIR)", dbopen.DefaultDataDir))
	root.PersistentFlags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table | json | yaml")
	root.PersistentFlags().BoolVar(&opts.NoHeaders, "no-headers", false, "Suppress table header row")
	root.PersistentFlags().BoolVar(&opts.Yes, "yes", false, "Skip interactive confirmation prompts")

	root.AddCommand(
		newVersionCmd(),
		cmds.NewDoctorCmd(opts),
		cmds.NewAuthCmd(opts),
		cmds.NewUserCmd(opts),
		cmds.NewDBCmd(opts),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "frpdeck CLI %s (frp %s)\n", Version, frpcd.BundledFrpVersion)
			if info, ok := debug.ReadBuildInfo(); ok {
				fmt.Fprintf(out, "go %s\n", info.GoVersion)
				if info.Main.Path != "" {
					fmt.Fprintf(out, "module %s %s\n", info.Main.Path, info.Main.Version)
				}
			}
			return nil
		},
	}
}

const longDescription = `frpdeck — local CLI for FrpDeck.

Manages a single FrpDeck installation by reading and writing the
SQLite database under --data-dir. When the daemon is running, the
CLI also pokes a Unix-domain control socket so configuration changes
take effect immediately without waiting for the periodic reconcile
tick or restarting the service.

The CLI does NOT manage remote FrpDeck instances. Use the Web UI's
"Remote management" feature (or P5 invitations) for that.`
