//go:build !wails

// Thin wrapper around internal/envfile so the install / daemon paths
// keep their original local function names while the actual parsing
// + writing logic lives in internal/envfile and can be reused by
// the standalone `frpdeck` CLI.

package main

import (
	"os"
	"path/filepath"

	"github.com/teacat99/FrpDeck/internal/envfile"
)

// envFileCandidates returns the search order for the runtime config
// file. The first existing path wins; later paths are ignored.
//
// Order:
//  1. ./frpdeck.env (portable / dev convenience)
//  2. <executable dir>/frpdeck.env (common on Windows / macOS app bundles)
//  3. /etc/frpdeck/frpdeck.env (Linux / macOS FHS strict)
//  4. %ProgramData%/frpdeck/frpdeck.env (Windows)
func envFileCandidates() []string {
	out := []string{"frpdeck.env"}
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "frpdeck.env"))
	}
	out = append(out, "/etc/frpdeck/frpdeck.env")
	if pd := os.Getenv("ProgramData"); pd != "" {
		out = append(out, filepath.Join(pd, "frpdeck", "frpdeck.env"))
	}
	return out
}

// loadEnvFile delegates to envfile.LoadIntoProcess. Kept as a
// package-local function so the existing test file does not need
// to know the import path.
func loadEnvFile(path string) error { return envfile.LoadIntoProcess(path) }

// loadFirstEnvFile walks envFileCandidates and loads the first one
// that exists. Returns the path that was loaded, or "" if none.
func loadFirstEnvFile() (string, error) {
	for _, p := range envFileCandidates() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := loadEnvFile(p); err != nil {
			return p, err
		}
		return p, nil
	}
	return "", nil
}

// renderEnvFile delegates to envfile.Write. Same naming reason as
// loadEnvFile above.
func renderEnvFile(path string, env map[string]string) error { return envfile.Write(path, env) }

// needsQuoting kept for the existing test; mirrors envfile.NeedsQuoting.
func needsQuoting(v string) bool { return envfile.NeedsQuoting(v) }
