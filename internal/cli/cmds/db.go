package cmds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/teacat99/FrpDeck/internal/control"
)

// NewDBCmd builds the `frpdeck db` command tree.
//
// `db backup` is safe to run with the daemon up — SQLite WAL gives
// us a consistent snapshot even mid-write. We use Go's stdlib io.Copy
// instead of `sqlite3 .backup` because the latter requires the
// sqlite3 CLI which is not always installed.
//
// `db restore` refuses to overwrite a live database: replacing the
// file out from under the daemon would leave it pointing at a stale
// inode. The CLI checks via the control socket; if the daemon is
// not running it proceeds. Operators who really know what they are
// doing can pass --force to skip the check.
func NewDBCmd(opts *GlobalOptions) *cobra.Command {
	db := &cobra.Command{
		Use:   "db",
		Short: "Database backup and restore",
	}
	db.AddCommand(newDBBackupCmd(opts), newDBRestoreCmd(opts))
	return db
}

func newDBBackupCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "backup <output-path>",
		Short: "Snapshot the SQLite database to a file",
		Long: `Copies the live frpdeck.db to the requested output path. SQLite's
WAL mode lets us read a consistent snapshot even while the daemon
is writing, but if you want a fully quiesced backup stop the
service first.

The file is created with mode 0600 because it carries the JWT
secret-derived password hashes; chmod loosely if you really want
to share it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outPath := args[0]
			srcPath := filepath.Join(opts.DataDir, "frpdeck.db")
			if _, err := os.Stat(srcPath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("frpdeck.db not found in %s", opts.DataDir)
				}
				return err
			}
			n, err := copyFile(srcPath, outPath, 0o600)
			if err != nil {
				return fmt.Errorf("backup: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "db: backed up %s -> %s (%d bytes)\n", srcPath, outPath, n)
			return nil
		},
	}
}

func newDBRestoreCmd(opts *GlobalOptions) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "restore <input-path>",
		Short: "Replace the live database with a backup file (daemon must be stopped)",
		Long: `Overwrites frpdeck.db with the supplied backup. The CLI refuses
unless either:
  • the daemon is not running (the control socket does not respond), OR
  • --force is passed.

Restoring under a running daemon is unsafe: the daemon is holding
an open file descriptor on the old inode and continues to read /
write that inode after the rename. The result is silent
divergence between disk and engine state.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inPath := args[0]
			if _, err := os.Stat(inPath); err != nil {
				return fmt.Errorf("restore: input %s: %w", inPath, err)
			}
			dstPath := filepath.Join(opts.DataDir, "frpdeck.db")

			if !force {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				_, _, err := opts.SocketClient.Ping(ctx)
				if err == nil {
					return errors.New("refusing to restore while daemon is running; stop the service first or pass --force")
				}
				if !errors.Is(err, control.ErrDaemonNotRunning) {
					// Daemon is dead or unreachable — restore is safe.
					_ = err
				}
			}
			if !opts.Yes {
				if err := confirm(cmd.OutOrStdout(), fmt.Sprintf("Restore %s -> %s? [y/N]: ", inPath, dstPath)); err != nil {
					return err
				}
			}
			n, err := copyFile(inPath, dstPath, 0o600)
			if err != nil {
				return fmt.Errorf("restore: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "db: restored %s -> %s (%d bytes)\n", inPath, dstPath, n)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "Restore even when the daemon is running (unsafe)")
	return c
}

// copyFile shells out to a stream copy + atomic rename. mode is
// applied after the rename so the destination's effective bits
// match the request.
func copyFile(src, dst string, mode os.FileMode) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return n, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	return n, nil
}
