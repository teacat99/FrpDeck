// bootstrap wires every subsystem FrpDeck needs. The headless `main`
// (default build) and the Wails-flavoured entry point (`-tags wails`)
// both call into this file so the boot path is a single source of
// truth — only the surrounding lifecycle (signal vs. window) differs.
//
// The function intentionally keeps everything assembled so callers
// stay shaped like a thin wrapper:
//   - bootstrap() returns a *Runtime carrying the live router,
//     lifecycle manager, and a Close hook that tears the whole stack
//     down in the right order.
//   - The headless main starts an *http.Server around r.Engine.
//   - The Wails main can either start the same TCP listener for LAN
//     access OR mount r.Engine as the AssetServer.Handler — both
//     paths share the assembled state.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/api"
	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/captcha"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/control"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/notify"
	"github.com/teacat99/FrpDeck/internal/remoteops"
	"github.com/teacat99/FrpDeck/internal/runtime"
	"github.com/teacat99/FrpDeck/internal/store"
	"github.com/teacat99/FrpDeck/web"
)

// Runtime bundles the long-lived collaborators produced by bootstrap.
// The Wails entry point holds a reference so it can address
// driver/lifecycle directly (e.g. "stop tunnel from tray menu") even
// though the bulk of the operation still goes through the HTTP API.
type Runtime struct {
	Cfg       *config.Config
	Engine    *gin.Engine
	Store     *store.Store
	Lifecycle *lifecycle.Manager
	Driver    frpcd.FrpDriver
	Auth      *auth.Authenticator
	Settings  *runtime.Settings
	// RemoteOps owns the mutating P5 flows. Same instance is used by
	// the HTTP handlers (api.Server.RemoteOps()) and the control
	// socket invoke dispatcher, so both transports share state.
	RemoteOps *remoteops.Service

	// AdminID / AdminUsername identify the seed administrator. Wails
	// uses these to mint an in-process JWT for tray-driven HTTP calls
	// so the audit trail stays attributed to "admin (desktop)" rather
	// than skipping the API entirely.
	AdminID       uint
	AdminUsername string

	// Control is the local Unix-socket RPC server the standalone
	// `frpdeck` CLI uses to ask the running daemon to reconcile
	// state after Direct-DB mutations. Only started when bootstrap
	// runs inside a real daemon (StartControl is opt-in).
	Control *control.Server

	// rootCtx fans out to the lifecycle manager and any tunnel runners
	// the driver spawns. Cancelling it is the canonical signal for
	// "we're shutting down, drain everything cleanly".
	rootCtx    context.Context
	cancelRoot context.CancelFunc
}

// Close tears the stack down in reverse order of bootstrap. Safe to
// call once; idempotent on the second call.
func (r *Runtime) Close() {
	if r.Control != nil {
		_ = r.Control.Close()
		r.Control = nil
	}
	if r.cancelRoot != nil {
		r.cancelRoot()
		r.cancelRoot = nil
	}
	if r.Lifecycle != nil {
		r.Lifecycle.Stop()
	}
}

// StartControl opens the local control socket so the standalone
// `frpdeck` CLI can ping us / trigger reconciliation. Returns nil
// if the socket cannot be opened — a missing control channel must
// not prevent the daemon from serving HTTP, since the user can
// still administer via the Web UI.
//
// Called from main()/main_wails() AFTER bootstrap so the failure
// path is opt-in and the headless smoke tests (which never call
// StartControl) keep their bootstrap output stable.
func (r *Runtime) StartControl(version string) {
	srv := control.New(r.Cfg.DataDir, control.Handlers{
		Version:    func() string { return version },
		ListenAddr: func() string { return r.Cfg.Listen },
		Reconcile: func(_ context.Context) error {
			if r.Lifecycle == nil {
				return fmt.Errorf("lifecycle not running")
			}
			return r.Lifecycle.Reconcile()
		},
		ReloadRuntime: func(_ context.Context) error {
			if r.Settings == nil {
				return fmt.Errorf("runtime settings not loaded")
			}
			return r.Settings.LoadFromKV(r.Store.LookupSetting)
		},
		Subscribe: func(ctx context.Context) (<-chan json.RawMessage, func()) {
			if r.Driver == nil {
				ch := make(chan json.RawMessage)
				close(ch)
				return ch, func() {}
			}
			return adaptDriverSubscribe(ctx, r.Driver)
		},
		Invoke: r.dispatchInvoke,
	})
	if err := srv.Start(); err != nil {
		log.Printf("[control] disabled: %v", err)
		return
	}
	r.Control = srv
	log.Printf("[control] socket ready at %s", srv.SocketPath())
}

// bootstrap performs the full boot sequence. Failure here is fatal —
// the caller logs and exits.
func bootstrap() (*Runtime, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "frpdeck.db")

	s, err := store.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	adminID, err := s.SeedAdminIfEmpty(cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return nil, fmt.Errorf("seed admin: %w", err)
	}
	adminUsername := cfg.AdminUsername
	if adminUsername == "" {
		adminUsername = store.DefaultAdminUsername
	}

	drv, err := frpcd.NewDriver(cfg.FrpcdDriver, frpcd.DriverOptions{DataDir: cfg.DataDir})
	if err != nil {
		return nil, fmt.Errorf("frpcd driver: %w", err)
	}
	log.Printf("frpcd driver: %s (frp %s)", drv.Name(), frpcd.BundledFrpVersion)

	rt := runtime.New(cfg)
	if err := rt.LoadFromKV(s.LookupSetting); err != nil {
		log.Printf("[runtime] load persisted settings: %v (continuing with env defaults)", err)
	}

	// Construct lifecycle AFTER runtime so we can wire the dynamic
	// expiring-notify threshold. Driver.PublishEvent is the same bus
	// the WebSocket fan-out reads from, so `tunnel_expiring` events
	// reach the browser through the existing channel.
	lm := lifecycle.New(s, drv, 30*time.Second, &lifecycle.Options{
		Publish:         drv.PublishEvent,
		ExpiringMinutes: rt.TunnelExpiringNotifyMinutes,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := lm.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("lifecycle start: %w", err)
	}

	notifier := notify.New(rt)
	captchaSvc := captcha.New(rt, s)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	authn := auth.New(cfg, rt, s)
	authn.SetSystemAdmin(adminID, adminUsername)
	authn.SetCaptcha(captchaSvc)
	authn.SetNotifier(notifier)
	apiSrv := api.New(cfg, rt, s, lm, drv, authn, captchaSvc, notifier)
	apiSrv.Router(r)
	mountStatic(r)

	return &Runtime{
		Cfg:           cfg,
		Engine:        r,
		Store:         s,
		Lifecycle:     lm,
		Driver:        drv,
		Auth:          authn,
		Settings:      rt,
		RemoteOps:     apiSrv.RemoteOps(),
		AdminID:       adminID,
		AdminUsername: adminUsername,
		rootCtx:       ctx,
		cancelRoot:    cancel,
	}, nil
}

// startHTTP spins up the standard TCP listener around the engine. It
// is shared by the headless and Wails entry points: the headless one
// always uses it, and Wails uses it when the user wants other
// devices on the LAN to reach the dashboard.
func startHTTP(rt *Runtime) *http.Server {
	httpSrv := &http.Server{
		Addr:              rt.Cfg.Listen,
		Handler:           rt.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("FrpDeck listening on %s (auth=%s, driver=%s)", rt.Cfg.Listen, rt.Cfg.AuthMode, rt.Driver.Name())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()
	return httpSrv
}

// dispatchInvoke is the daemon-side method dispatch table for
// CmdInvoke calls. New business RPCs land here as a single case
// each; the table is the source of truth — protocol.go does not
// need to know about per-method names.
//
// Method names use a "<resource>.<verb>" convention so the CLI's
// flag-to-method mapping reads naturally
// (`frpdeck remote invite` -> "remote.invite").
func (r *Runtime) dispatchInvoke(ctx context.Context, method string, body json.RawMessage) (json.RawMessage, error) {
	if r.RemoteOps == nil {
		return nil, fmt.Errorf("remote ops service not initialised")
	}
	switch method {
	case "remote.invite":
		var args remoteops.CreateInviteArgs
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				return nil, fmt.Errorf("decode args: %w", err)
			}
		}
		res, err := r.RemoteOps.CreateInvitation(ctx, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	case "remote.refresh":
		var args remoteops.RefreshInviteArgs
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				return nil, fmt.Errorf("decode args: %w", err)
			}
		}
		res, err := r.RemoteOps.RefreshInvitation(ctx, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	case "remote.revoke-mgmt-token":
		var args remoteops.NodeArgs
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				return nil, fmt.Errorf("decode args: %w", err)
			}
		}
		res, err := r.RemoteOps.RevokeMgmtToken(ctx, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	case "remote.revoke":
		var args remoteops.NodeArgs
		if len(body) > 0 {
			if err := json.Unmarshal(body, &args); err != nil {
				return nil, fmt.Errorf("decode args: %w", err)
			}
		}
		res, err := r.RemoteOps.RevokeRemoteNode(ctx, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

// adaptDriverSubscribe bridges the driver's typed Event channel to
// the json.RawMessage channel the control package consumes. We
// marshal each event in the bridge goroutine so the control server
// stays free of an internal/frpcd import; the trade-off (one extra
// allocation per event) is negligible compared to the 64-slot
// EventBus buffer.
func adaptDriverSubscribe(ctx context.Context, drv frpcd.FrpDriver) (<-chan json.RawMessage, func()) {
	src, unsub := drv.Subscribe()
	out := make(chan json.RawMessage, 64)
	go func() {
		defer close(out)
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-src:
				if !ok {
					return
				}
				raw, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				select {
				case out <- raw:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, unsub
}

// mountStatic serves the embedded frontend assets. The fallback
// message keeps `frpdeck-server` informative when the frontend has not
// been built yet — useful while iterating on backend in isolation.
func mountStatic(r *gin.Engine) {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		log.Printf("[web] embed not available: %v", err)
		r.NoRoute(func(c *gin.Context) {
			c.String(http.StatusOK, "FrpDeck backend is running. Build the frontend (`npm run build` in ./frontend) to mount the UI.")
		})
		return
	}
	r.NoRoute(func(c *gin.Context) {
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := sub.Open(path)
		if err == nil {
			stat, _ := f.Stat()
			if stat != nil && !stat.IsDir() {
				http.ServeFileFS(c.Writer, c.Request, sub, path)
				return
			}
		}
		if data, err := fs.ReadFile(sub, "index.html"); err == nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
			return
		}
		c.String(http.StatusOK, "FrpDeck backend is running. Frontend assets not yet built.")
	})
}
