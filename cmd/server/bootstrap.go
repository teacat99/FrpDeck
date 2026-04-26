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
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/notify"
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

	// AdminID / AdminUsername identify the seed administrator. Wails
	// uses these to mint an in-process JWT for tray-driven HTTP calls
	// so the audit trail stays attributed to "admin (desktop)" rather
	// than skipping the API entirely.
	AdminID       uint
	AdminUsername string

	// rootCtx fans out to the lifecycle manager and any tunnel runners
	// the driver spawns. Cancelling it is the canonical signal for
	// "we're shutting down, drain everything cleanly".
	rootCtx    context.Context
	cancelRoot context.CancelFunc
}

// Close tears the stack down in reverse order of bootstrap. Safe to
// call once; idempotent on the second call.
func (r *Runtime) Close() {
	if r.cancelRoot != nil {
		r.cancelRoot()
		r.cancelRoot = nil
	}
	if r.Lifecycle != nil {
		r.Lifecycle.Stop()
	}
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
