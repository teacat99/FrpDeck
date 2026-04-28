// Command frpdeck is the standalone local CLI for FrpDeck.
//
// The binary intentionally has no service-lifecycle subcommands —
// `install / uninstall / start / stop` continue to live on
// `frpdeck-server` so the existing systemd-style deployment story
// stays stable. This binary is the operator's day-to-day tool:
// changing a forgotten admin password, listing endpoints, importing
// frpc.toml, tailing logs, and so on.
//
// We keep main as small as possible — every interesting line is in
// internal/cli — so the entry-point file is purely about wiring
// argv into the cobra root and translating cobra errors into exit
// codes.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/teacat99/FrpDeck/internal/cli"
	"github.com/teacat99/FrpDeck/internal/cli/cmds"
)

func main() {
	root := cli.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "frpdeck:", err)
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		// Cobra reports parse errors via its own machinery and
		// returns a generic error here; map to exit 2 for "usage"
		// vs the default 1 for "command failed".
		var ue cmds.UsageError
		if errors.As(err, &ue) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
