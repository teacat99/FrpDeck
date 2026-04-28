//go:build !wails

// kardianos/service adapter for FrpDeck. The same `program` runs
// under systemd / Windows SCM / launchd as well as in the foreground
// — service.Service.Run() picks the right path automatically.
//
// Lifecycle contract from kardianos/service:
//   - Start MUST return promptly (the service manager waits).
//   - Heavy work goes in a goroutine started from Start.
//   - Stop is called when the manager wants the service down. We
//     have ~10s on Linux / ~30s on Windows before SIGKILL, so we
//     mirror cmd/server/main.go's shutdown timeout.
//
// We deliberately reuse bootstrap()/startHTTP() so the daemon
// binary, the desktop binary, and the service variant boot from a
// single source of truth.

package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/kardianos/service"
)

const (
	serviceName        = "frpdeck"
	serviceDisplayName = "FrpDeck"
	serviceDescription = "FrpDeck — multi-frps tunnel manager (cross-platform frpc)"
)

// program is the *service.Interface implementation passed to
// service.New. It owns the bootstrap/runtime lifecycle so Start and
// Stop have well-defined hand-offs.
type program struct {
	mu      sync.Mutex
	rt      *Runtime
	httpSrv *http.Server

	startErr chan error
	stopOnce sync.Once
}

func newProgram() *program {
	return &program{startErr: make(chan error, 1)}
}

// Start fires the bootstrap on a goroutine so the service manager
// gets its prompt return. If bootstrap fails, the error is captured
// in startErr — the foreground variant surfaces it via Run() (which
// blocks until Stop) by polling at the top of Run(); the systemd
// variant logs and exits.
func (p *program) Start(_ service.Service) error {
	go p.run()
	return nil
}

// run executes the full boot sequence. Errors are logged and stored
// so the foreground daemon can fail-fast cleanly.
func (p *program) run() {
	rt, err := bootstrap()
	if err != nil {
		log.Printf("bootstrap: %v", err)
		select {
		case p.startErr <- err:
		default:
		}
		return
	}

	srv := startHTTP(rt)
	rt.StartControl(daemonVersion())

	p.mu.Lock()
	p.rt = rt
	p.httpSrv = srv
	p.mu.Unlock()
}

// Stop is called once by the service manager. We protect against
// double-call (Stop is also invoked from the foreground signal path)
// with sync.Once.
func (p *program) Stop(_ service.Service) error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		srv, rt := p.httpSrv, p.rt
		p.mu.Unlock()

		if srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
		}
		if rt != nil {
			rt.Close()
		}
	})
	return nil
}

// buildServiceConfig produces the Config used both at install time
// (to render the systemd unit / Win SCM entry / launchd plist) and
// at runtime (to identify the service inside service.Run). The
// install command may decorate it with extra fields (UserName,
// Executable copy path) before invoking service.Control.
func buildServiceConfig() *service.Config {
	return &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		Arguments:   []string{"run"},
		Option: service.KeyValue{
			// systemd: pull EnvironmentFile + restart-on-failure
			// + always-use-our-template — the default kardianos
			// systemd unit does not honour EnvironmentFile and we
			// rely on /etc/frpdeck/frpdeck.env to ship secrets.
			"SystemdScript": systemdUnitTemplate,
			// Linux & macOS: keep the open-file ceiling sane for
			// long-running connection pools.
			"LimitNOFILE": 32768,
			// macOS launchd: keep the daemon up.
			"KeepAlive": true,
			"RunAtLoad": true,
			// Windows SCM:
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetPeriod":   60,
			"DelayedAutoStart":       false,
			"StartType":              "automatic",
		},
	}
}

// systemdUnitTemplate is rendered by kardianos/service at install
// time. The placeholders `{{.Name}}`, `{{.Description}}`,
// `{{.Path}}`, `{{.Arguments}}`, `{{.UserName}}` etc. are filled in
// by the library — see kardianos/service/service_systemd_linux.go for
// the data model.
//
// We deviate from the upstream default in two key spots:
//   - EnvironmentFile=/etc/frpdeck/frpdeck.env (write-once via the
//     install subcommand, edit-and-restart afterwards)
//   - Restart=always with a 5s back-off so transient panics during
//     reconcile do not require manual `systemctl restart`.
const systemdUnitTemplate = `[Unit]
Description={{.Description}}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/frpdeck/frpdeck.env
{{if .UserName}}User={{.UserName}}{{end}}
{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory}}{{end}}
ExecStart={{.Path}} {{range .Arguments}}{{.}} {{end}}
Restart=always
RestartSec=5s
LimitNOFILE=32768
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
