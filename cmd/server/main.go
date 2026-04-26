//go:build !wails

// Command frpdeck-server is the headless HTTP entry point for FrpDeck.
//
// Subcommands (kardianos/service-driven):
//
//	frpdeck-server                 # run as daemon (foreground or under
//	                                 systemd / Win SCM / launchd —
//	                                 service.Run autodetects)
//	frpdeck-server run             # explicit foreground equivalent
//	frpdeck-server install [...]   # register the platform service
//	frpdeck-server uninstall       # remove it
//	frpdeck-server start|stop|restart|status
//	frpdeck-server version
//
// The Wails desktop entry point lives behind `-tags wails` and skips
// this dispatcher — it always runs as a foreground GUI app.
package main

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/kardianos/service"

	"github.com/teacat99/FrpDeck/internal/frpcd"
)

func main() {
	cfg := buildServiceConfig()
	prg := newProgram()
	s, err := service.New(prg, cfg)
	if err != nil {
		log.Fatalf("service: %v", err)
	}

	// Subcommands that only talk to the OS service manager (or print
	// metadata) intentionally skip the env-file load so their stderr
	// stays free of "env file: ..." noise.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			runInstall(os.Args[2:])
			return
		case "uninstall":
			runUninstall(s)
			return
		case "start", "stop", "restart":
			runControl(s, os.Args[1])
			return
		case "status":
			runStatus(s)
			return
		case "run":
			// fall through to the daemon path
		case "version", "-v", "--version":
			runVersion()
			return
		case "help", "-h", "--help":
			runHelp()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
			runHelp()
			os.Exit(2)
		}
	}

	// Daemon path: load /etc/frpdeck/frpdeck.env (or the platform
	// equivalent) so config.Load() inside bootstrap sees installed
	// values. The non-overwrite policy keeps explicit shell
	// `FRPDECK_X=...` overrides working. Then service.Run blocks
	// until Stop fires (systemd / SCM / launchd or SIGINT/SIGTERM).
	if path, err := loadFirstEnvFile(); err != nil {
		log.Fatalf("env file %s: %v", path, err)
	} else if path != "" {
		log.Printf("env file: %s", path)
	}
	if err := s.Run(); err != nil {
		log.Fatalf("run: %v", err)
	}
}

func runVersion() {
	fmt.Printf("frpdeck-server (frp %s)\n", frpcd.BundledFrpVersion)
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("go %s\n", info.GoVersion)
	}
}

func runHelp() {
	fmt.Println(`frpdeck-server — multi-frps tunnel manager (FrpDeck)

Usage:
  frpdeck-server                  Run as daemon (foreground / under service manager)
  frpdeck-server run              Explicit daemon (used by service manager)
  frpdeck-server install [flags]  Register platform service (systemd / SCM / launchd)
  frpdeck-server uninstall        Remove platform service + env file (data preserved)
  frpdeck-server start            Start the registered service
  frpdeck-server stop             Stop the registered service
  frpdeck-server restart          Restart the registered service
  frpdeck-server status           Print {running|stopped|unknown} (exit 0/3/4)
  frpdeck-server version          Print version metadata
  frpdeck-server help             Show this message

Environment is loaded from /etc/frpdeck/frpdeck.env (Linux/macOS) or
%ProgramData%\frpdeck\frpdeck.env (Windows) when present. Shell-set
FRPDECK_* variables always override the file.`)
}
