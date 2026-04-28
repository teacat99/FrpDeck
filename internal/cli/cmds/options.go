// Package cmds holds the per-subcommand factories used by the CLI.
//
// Each factory returns a fully-wired *cobra.Command that the root
// constructor in internal/cli registers into the tree. Splitting one
// file per command (auth.go, user.go, db.go, doctor.go, ...) keeps
// each file under 200 lines and makes adding a new subcommand a
// matter of adding one file + one AddCommand line.
package cmds

import (
	"fmt"

	"github.com/teacat99/FrpDeck/internal/cli/dbopen"
	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/store"
)

// GlobalOptions carries the flags every subcommand cares about.
// One pointer per CLI invocation; mutated by the persistent
// PreRun hook so subcommand RunE bodies can read the resolved
// values directly.
type GlobalOptions struct {
	DataDir      string
	Output       string
	Format       output.Format
	NoHeaders    bool
	Yes          bool
	SocketClient *control.Client
}

// RenderOpts converts the global flag pair into the variadic
// Option list output.Render expects.
func (g *GlobalOptions) RenderOpts() []output.Option {
	if g.NoHeaders {
		return []output.Option{output.WithoutHeaders()}
	}
	return nil
}

// OpenStore is the shared "open the FrpDeck database" helper. It
// hides the dbopen wrapper behind a one-line call so subcommand
// bodies stay focused on business logic.
//
// The returned closer is best-effort: a SQLite close failure is
// rare enough that we surface it via fmt.Fprintln to stderr rather
// than failing the whole command, which would mask the actual
// business outcome.
func (g *GlobalOptions) OpenStore() (*store.Store, func(), error) {
	st, closer, err := dbopen.Open(g.DataDir)
	if err != nil {
		return nil, nil, err
	}
	return st, func() {
		if err := closer(); err != nil {
			fmt.Println("close store:", err)
		}
	}, nil
}

// UsageError marks errors that should map to exit code 2 (LSB
// "incorrect usage") so the cmd/cli main shim can honour the
// convention. Cobra's internal flag-parsing errors do not satisfy
// this interface, but command-specific validation errors that
// boil down to "user typed the wrong thing" should.
type UsageError struct{ Msg string }

func (u UsageError) Error() string { return u.Msg }
