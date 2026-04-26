// Command frpdeck-server is the headless HTTP entry point for FrpDeck.
//
// It boots the SQLite store, the in-process frp client driver (P1 will
// flip the default from `mock` to `embedded`), the lifecycle manager,
// the auth + captcha + ntfy infrastructure, and finally the Gin router.
// The Wails desktop entry point lives behind a build tag and reuses the
// same wiring helpers from this package.
package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "frpdeck.db")

	s, err := store.New(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	adminID, err := s.SeedAdminIfEmpty(cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		log.Fatalf("seed admin: %v", err)
	}
	adminUsername := cfg.AdminUsername
	if adminUsername == "" {
		adminUsername = store.DefaultAdminUsername
	}

	// P0 ships with the in-memory MockDriver. P1 flips the default to
	// `embedded` (in-process *frp/client.Service) and per-Endpoint rows
	// can later select `subprocess` for the bring-your-own-frpc story.
	drv, err := frpcd.NewDriver(cfg.FrpcdDriver)
	if err != nil {
		log.Fatalf("frpcd driver: %v", err)
	}
	log.Printf("frpcd driver: %s (frp %s)", drv.Name(), frpcd.BundledFrpVersion)

	lm := lifecycle.New(s, drv, 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := lm.Start(ctx); err != nil {
		log.Fatalf("lifecycle start: %v", err)
	}

	rt := runtime.New(cfg)
	if err := rt.LoadFromKV(s.LookupSetting); err != nil {
		log.Printf("[runtime] load persisted settings: %v (continuing with env defaults)", err)
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
	server := api.New(cfg, rt, s, lm, drv, authn, captchaSvc, notifier)
	server.Router(r)

	mountStatic(r)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("FrpDeck listening on %s (auth=%s, driver=%s)", cfg.Listen, cfg.AuthMode, drv.Name())
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down (frp tunnels retained for next boot)")

	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
	lm.Stop()
}

// mountStatic wires the embedded frontend assets on top of the Gin router.
// When no dist is present (dev mode before the frontend is built) a
// helpful placeholder is returned instead of a 404 so operators can tell
// the server is running.
func mountStatic(r *gin.Engine) {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		log.Printf("[web] embed not available: %v", err)
		r.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "FrpDeck backend is running. Build the frontend (`npm run build` in ./frontend) to mount the UI.") })
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
