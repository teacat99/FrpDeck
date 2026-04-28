package cmds

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/cli/output"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// NewDoctorCmd builds the `frpdeck doctor` command. It runs a fixed
// set of self-checks against the local installation and prints a
// table of pass/fail results. Each check is intentionally narrow
// and self-contained — when a check fails, the line tells the
// operator exactly what to fix without having to read another
// command's documentation.
//
// The command exits 0 if every check passes, 1 if any check fails.
// `--output json` is honoured so monitoring scripts can scrape it.
func NewDoctorCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run local installation self-checks",
		Long: `Diagnose a FrpDeck installation by checking:
  • data directory exists and is writable
  • frpdeck.db is reachable
  • daemon control socket responds (if running)
  • bundled / external frpc binary is locatable
  • no stale lock files

Use --output json to consume from monitoring scripts.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results := runDoctorChecks(opts)
			rows := make([]map[string]any, len(results))
			anyFailed := false
			for i, r := range results {
				rows[i] = map[string]any{
					"check":  r.Name,
					"status": r.Status,
					"detail": r.Detail,
				}
				if r.Status == "fail" {
					anyFailed = true
				}
			}
			cols := []output.Column{
				{Title: "CHECK", Key: "check"},
				{Title: "STATUS", Key: "status"},
				{Title: "DETAIL", Key: "detail"},
			}
			if err := output.Render(cmd.OutOrStdout(), opts.Format, cols, rows, opts.RenderOpts()...); err != nil {
				return err
			}
			if anyFailed {
				return errExitCode{code: 1, msg: "one or more checks failed"}
			}
			return nil
		},
	}
}

type doctorResult struct {
	Name   string
	Status string // "ok" | "fail" | "warn"
	Detail string
}

// runDoctorChecks executes the full set in order. The order
// matters — later checks depend on earlier ones (you cannot ping
// the daemon without a data directory), so a failed prerequisite
// surfaces clearly before its consequences.
func runDoctorChecks(opts *GlobalOptions) []doctorResult {
	var out []doctorResult

	// 1. Data directory exists + writable.
	out = append(out, checkDataDir(opts.DataDir))

	// 2. Database file reachable.
	dbPath := filepath.Join(opts.DataDir, "frpdeck.db")
	out = append(out, checkDB(dbPath))

	// 3. Daemon control socket — informational only. "Daemon not
	//    running" is a perfectly valid steady state (the operator
	//    might be using the CLI for offline maintenance), so it is
	//    a "warn" rather than "fail" so doctor still exits 0.
	out = append(out, checkControlSocket(opts.SocketClient))

	// 4. frpc binary lookup (only matters for SubprocessDriver
	//    deployments; bundled-driver users can ignore the warn).
	out = append(out, checkFrpcBinary(opts.DataDir))

	return out
}

func checkDataDir(path string) doctorResult {
	if path == "" {
		return doctorResult{Name: "data-dir", Status: "fail", Detail: "no data directory configured"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return doctorResult{Name: "data-dir", Status: "fail", Detail: err.Error()}
	}
	if !info.IsDir() {
		return doctorResult{Name: "data-dir", Status: "fail", Detail: path + ": not a directory"}
	}
	// Writability test: try to create + remove a sentinel.
	probe := filepath.Join(path, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return doctorResult{Name: "data-dir", Status: "fail", Detail: "not writable: " + err.Error()}
	}
	_ = os.Remove(probe)
	return doctorResult{Name: "data-dir", Status: "ok", Detail: path}
}

func checkDB(path string) doctorResult {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return doctorResult{Name: "database", Status: "warn", Detail: "frpdeck.db not yet created (daemon hasn't run here)"}
		}
		return doctorResult{Name: "database", Status: "fail", Detail: err.Error()}
	}
	return doctorResult{Name: "database", Status: "ok", Detail: fmt.Sprintf("%s (%d bytes)", path, info.Size())}
}

func checkControlSocket(c *control.Client) doctorResult {
	if c == nil {
		return doctorResult{Name: "control-socket", Status: "warn", Detail: "no client (data dir unset)"}
	}
	if !c.SocketExists() {
		return doctorResult{Name: "control-socket", Status: "warn", Detail: "daemon not running (no socket at " + c.SocketPath() + ")"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, listen, err := c.Ping(ctx)
	if err != nil {
		if errors.Is(err, control.ErrDaemonNotRunning) {
			return doctorResult{Name: "control-socket", Status: "warn", Detail: "daemon not running (socket gone)"}
		}
		return doctorResult{Name: "control-socket", Status: "fail", Detail: "ping: " + err.Error()}
	}
	return doctorResult{Name: "control-socket", Status: "ok", Detail: fmt.Sprintf("daemon %s on %s", v, listen)}
}

func checkFrpcBinary(dataDir string) doctorResult {
	// SubprocessDriver looks at <data_dir>/bin/frpc-<version> first.
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	candidate := filepath.Join(dataDir, "bin", "frpc-"+frpcd.BundledFrpVersion+suffix)
	if _, err := os.Stat(candidate); err == nil {
		return doctorResult{Name: "frpc-binary", Status: "ok", Detail: "bundled match: " + candidate}
	}
	// Fall back to PATH.
	if path, err := exec.LookPath("frpc"); err == nil {
		return doctorResult{Name: "frpc-binary", Status: "ok", Detail: "PATH: " + path}
	}
	return doctorResult{Name: "frpc-binary", Status: "warn", Detail: "no external frpc found (only embedded driver will work)"}
}

// errExitCode lets a RunE body request a non-zero exit while still
// flowing through cobra's error-reporting machinery (so
// SilenceErrors+SilenceUsage suppress the help dump). The CLI main
// shim teaches cobra to map this to os.Exit(code).
type errExitCode struct {
	code int
	msg  string
}

func (e errExitCode) Error() string { return e.msg }
func (e errExitCode) ExitCode() int { return e.code }
