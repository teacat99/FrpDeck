//go:build !wails

// Subcommand handlers for service-lifecycle management.
//
// `install` is the heaviest one: it copies the running binary into
// a stable location, renders the env file with the user-provided
// settings, and asks kardianos/service to register the platform
// service (systemd unit / Win SCM entry / launchd plist).
//
// `uninstall` removes the service and the env file, but leaves the
// data directory in place — uninstalling the service should never
// silently destroy the SQLite database.
//
// `start` / `stop` / `restart` / `status` are thin wrappers around
// service.Control; they exit 0 on success and 1 on failure to fit
// shell-script callers.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
)

// Default install paths follow the FHS-strict convention chosen in
// plan.md §11. We expose them as variables so platform-specific
// logic in the future (e.g. macOS /usr/local/sbin) can override
// without restructuring the package.
var (
	defaultBinPath     = "/usr/local/bin/frpdeck-server"
	defaultDataDir     = "/var/lib/frpdeck"
	defaultEnvFilePath = "/etc/frpdeck/frpdeck.env"
)

func init() {
	if runtime.GOOS == "windows" {
		// %ProgramFiles%/FrpDeck/frpdeck-server.exe
		// %ProgramData%/frpdeck (data + env file)
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		defaultBinPath = filepath.Join(programFiles, "FrpDeck", "frpdeck-server.exe")
		defaultDataDir = filepath.Join(programData, "frpdeck")
		defaultEnvFilePath = filepath.Join(programData, "frpdeck", "frpdeck.env")
	}
}

// runInstall handles `frpdeck-server install [...flags...]`.
//
// The flow:
//  1. Parse user overrides for listen address, data directory, admin
//     credentials, JWT secret, run-as user.
//  2. Copy os.Executable() to the destination path.
//  3. Render the env file with the provided values.
//  4. Build a service.Config pointing at the destination binary and
//     hand it to kardianos/service.Control("install").
//  5. Hint the user about `systemctl start frpdeck` / equivalent.
func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	listen := fs.String("listen", "0.0.0.0:8080", "HTTP listen address (FRPDECK_LISTEN)")
	dataDir := fs.String("data", defaultDataDir, "Data directory (FRPDECK_DATA_DIR)")
	binPath := fs.String("bin", defaultBinPath, "Where to copy the running binary")
	envFile := fs.String("env-file", defaultEnvFilePath, "Path to write the environment file")
	authMode := fs.String("auth-mode", "password", "Auth mode (password / ipwhitelist / none)")
	adminUser := fs.String("admin-username", "admin", "Initial admin username (FRPDECK_ADMIN_USERNAME)")
	adminPass := fs.String("admin-password", "", "Initial admin password (FRPDECK_ADMIN_PASSWORD); leave empty to keep manual onboarding")
	jwtSecret := fs.String("jwt-secret", "", "JWT signing secret (FRPDECK_JWT_SECRET); auto-generated if empty")
	driver := fs.String("driver", "embedded", "Frpc driver (embedded / mock / subprocess)")
	runAs := fs.String("user", "", "System user to run the service as (Linux/macOS); empty = root")
	overwrite := fs.Bool("overwrite", false, "Overwrite an existing env file at --env-file")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	if err := mustBeRoot(); err != nil {
		exitf("install: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		exitf("install: locate self: %v", err)
	}

	if !*overwrite {
		if _, err := os.Stat(*envFile); err == nil {
			exitf("install: %s already exists; pass --overwrite to replace", *envFile)
		}
	}

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		exitf("install: create data dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*binPath), 0o755); err != nil {
		exitf("install: create bin dir: %v", err)
	}
	if err := copyFile(exe, *binPath, 0o755); err != nil {
		exitf("install: copy binary: %v", err)
	}
	fmt.Printf("install: binary -> %s\n", *binPath)

	if *jwtSecret == "" {
		*jwtSecret = randomHex(32)
	}

	envMap := map[string]string{
		"FRPDECK_LISTEN":         *listen,
		"FRPDECK_DATA_DIR":       *dataDir,
		"FRPDECK_AUTH_MODE":      *authMode,
		"FRPDECK_ADMIN_USERNAME": *adminUser,
		"FRPDECK_FRPCD_DRIVER":   *driver,
		"FRPDECK_JWT_SECRET":     *jwtSecret,
	}
	if *adminPass != "" {
		envMap["FRPDECK_ADMIN_PASSWORD"] = *adminPass
	}
	if err := renderEnvFile(*envFile, envMap); err != nil {
		exitf("install: write env file: %v", err)
	}
	fmt.Printf("install: env file -> %s (mode 0600)\n", *envFile)

	cfg := buildServiceConfig()
	cfg.Executable = *binPath
	cfg.WorkingDirectory = *dataDir
	if *runAs != "" {
		cfg.UserName = *runAs
	}

	prg := newProgram()
	s, err := service.New(prg, cfg)
	if err != nil {
		exitf("install: build service: %v", err)
	}
	if err := service.Control(s, "install"); err != nil {
		exitf("install: register service: %v", err)
	}

	fmt.Println()
	fmt.Println("install: service registered. Next steps:")
	switch runtime.GOOS {
	case "linux":
		fmt.Println("  systemctl start frpdeck")
		fmt.Println("  systemctl enable frpdeck")
		fmt.Println("  journalctl -u frpdeck -f   # follow logs")
	case "darwin":
		fmt.Println("  launchctl start frpdeck    # or reboot for auto-load")
	case "windows":
		fmt.Println("  net start frpdeck")
		fmt.Println("  Get-Service frpdeck       # PowerShell")
	}
}

// runUninstall removes the service entry + the env file. The data
// directory is preserved so users do not lose their SQLite state.
func runUninstall(s service.Service) {
	if err := mustBeRoot(); err != nil {
		exitf("uninstall: %v", err)
	}
	// Best-effort stop before uninstall — the platform service
	// manager will refuse to remove a running service on some
	// systems (notably old systemd).
	_ = service.Control(s, "stop")
	if err := service.Control(s, "uninstall"); err != nil {
		exitf("uninstall: %v", err)
	}
	if err := os.Remove(defaultEnvFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "uninstall: remove env file: %v\n", err)
	}
	fmt.Println("uninstall: service removed; data directory preserved.")
}

// runControl runs one of {start, stop, restart}. We translate the
// kardianos error into a non-zero exit code so callers can `||
// true` cleanly when desired.
func runControl(s service.Service, action string) {
	if err := mustBeRoot(); err != nil {
		exitf("%s: %v", action, err)
	}
	if err := service.Control(s, action); err != nil {
		exitf("%s: %v", action, err)
	}
	fmt.Printf("%s: ok\n", action)
}

// runStatus prints a one-line summary {running|stopped|unknown} and
// exits 0 if running, 3 if stopped, 4 if unknown — these codes
// follow LSB convention for `systemctl status`.
func runStatus(s service.Service) {
	st, err := s.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: %v\n", err)
		os.Exit(4)
	}
	switch st {
	case service.StatusRunning:
		fmt.Println("running")
	case service.StatusStopped:
		fmt.Println("stopped")
		os.Exit(3)
	default:
		fmt.Println("unknown")
		os.Exit(4)
	}
}

// ---------- helpers --------------------------------------------------

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func mustBeRoot() error {
	if runtime.GOOS == "windows" {
		return nil // SCM enforces its own ACL check
	}
	if os.Geteuid() != 0 {
		return errors.New("must run as root (try `sudo`)")
	}
	return nil
}

// randomHex returns a hex-encoded random string of the given byte
// length using crypto/rand. Used for default JWT secrets so the
// installed deployment is not stuck with a guessable value.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failure is essentially impossible on a
		// modern OS; surface the error so the install loudly aborts
		// instead of writing a zero-byte secret.
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
