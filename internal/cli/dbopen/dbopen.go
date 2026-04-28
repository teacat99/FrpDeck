// Package dbopen wraps internal/store with the safety checks the CLI
// needs but the daemon does not.
//
// The daemon trusts that nobody else is touching the SQLite file —
// it owns the data directory by convention. The CLI cannot make that
// assumption: a `frpdeck endpoint add` invocation might race against a
// running daemon, against another concurrent CLI call, or against a
// half-finished `db restore`. This package centralises the
// preconditions so each command stays a one-liner:
//
//	st, close, err := dbopen.Open(dataDir)
//	if err != nil { … }
//	defer close()
//
// Callers should not bypass this wrapper to call store.New directly.
package dbopen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teacat99/FrpDeck/internal/store"
)

// DefaultDataDir mirrors the install.go convention so users that
// follow the README + `frpdeck-server install` flow do not need
// `--data-dir` for every CLI invocation.
//
// The platform-specific override lives in default_paths_*.go.
var DefaultDataDir = defaultDataDir()

// Open validates the data directory and opens the SQLite store. The
// returned closer must be invoked by the caller (typically deferred);
// it surfaces the underlying GORM close error so the CLI can decide
// whether to log it.
//
// dataDir may be empty: in that case we fall back to DefaultDataDir.
// The directory must already exist; the CLI never auto-creates it
// because creating /var/lib/frpdeck silently would mask "you forgot
// --data-dir, you actually wanted /tmp/frpdeck-test/" mistakes.
func Open(dataDir string) (*store.Store, func() error, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir
	}
	if dataDir == "" {
		return nil, nil, errors.New("no data directory: pass --data-dir or set FRPDECK_DATA_DIR")
	}
	if err := assertDir(dataDir); err != nil {
		return nil, nil, err
	}
	dbPath := filepath.Join(dataDir, "frpdeck.db")
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("frpdeck.db not found in %s — has the daemon ever run here?", dataDir)
		}
		return nil, nil, fmt.Errorf("stat %s: %w", dbPath, err)
	}
	st, err := store.New(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	closer := func() error {
		sqlDB, err := st.DB().DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return st, closer, nil
}

// ResolveDir returns the data dir the CLI should use given the
// caller-supplied flag value. Pulled into its own function so
// commands can render "(using …)" hints without re-implementing
// the fallback chain.
func ResolveDir(flag string) string {
	if flag != "" {
		return flag
	}
	return DefaultDataDir
}

func assertDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data dir %s is not a directory", path)
	}
	return nil
}
