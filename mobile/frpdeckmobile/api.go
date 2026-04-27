// Package frpdeckmobile is the gomobile bridge that exposes FrpDeck to
// Android (and future iOS) hosts.
//
// The package wires the same internal subsystems the headless and Wails
// builds use, but skips the kardianos/service + systray + Wails dependencies
// that have no place in a foreground Service:
//
//   - Config is built from explicit StartOptions instead of os.Getenv,
//     because Android passes parameters via the Java bridge rather than
//     through environment variables.
//   - The HTTP listener stays bound to 127.0.0.1:<port> so the embedded
//     WebView and the Native Compose UI talk to it via loopback. Other
//     devices on the LAN are intentionally NOT reachable — Android's
//     foreground-service model doesn't fit a "shared LAN dashboard"
//     posture, and accidental exposure on a public Wi-Fi would be a
//     genuine security regression.
//   - Logs are fanned to a Java callback (LogHandler) instead of stderr.
//
// The exported surface follows gomobile's restrictions: only basic types
// (string, int, bool) and a single LogHandler interface, so the generated
// Java/Kotlin bindings stay flat.
package frpdeckmobile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/api"
	"github.com/teacat99/FrpDeck/internal/auth"
	"github.com/teacat99/FrpDeck/internal/captcha"
	"github.com/teacat99/FrpDeck/internal/config"
	"github.com/teacat99/FrpDeck/internal/frpcd"
	"github.com/teacat99/FrpDeck/internal/lifecycle"
	"github.com/teacat99/FrpDeck/internal/model"
	"github.com/teacat99/FrpDeck/internal/notify"
	"github.com/teacat99/FrpDeck/internal/runtime"
	"github.com/teacat99/FrpDeck/internal/store"
	"github.com/teacat99/FrpDeck/web"
)

// LogHandler is the interface the Java side implements to receive log
// lines. gomobile generates a `LogHandler` Java interface with a single
// `onLog(String)` method; the Android Compose UI uses it to populate the
// log panel without polling.
type LogHandler interface {
	OnLog(line string)
}

// VpnRequestHandler is the Java-side interface that receives "this
// tunnel just went active and asks for device-level VPN routing"
// notifications (P6′/P7′). The Android shell installs at most one
// handler per process; the implementation typically:
//
//  1. Caches the latest socks5 URL for FrpDeckVpnService to consume.
//  2. If the user has already granted VpnService permission, posts a
//     foreground intent to start the service immediately.
//  3. Otherwise emits a small in-app toast / system notification asking
//     the user to open the WebView's "Android settings" page and tap
//     "Request VPN permission" once.
//
// `tunnelID` is widened to int because gomobile's Java mapping does not
// expose unsigned types — the conversion from uint is harmless because
// our IDs come from a SQLite autoincrement column that fits int64
// trivially.
//
// `socks5URL` is pre-formatted as `socks5://<host>:<port>`. The handler
// must NOT parse or reconstruct it; just forward as-is to tun2socks.
type VpnRequestHandler interface {
	OnVpnRequest(tunnelID int, tunnelName string, socks5URL string)
}

// state is the package-global runtime container. We deliberately keep it
// global because the Android host calls Start/Stop/IsRunning/AdminToken
// on a shared instance — there is no concept of "multiple FrpDeck servers
// inside one process" on mobile.
var (
	stateMu       sync.Mutex
	current       *mobileRuntime
	logSinks      sync.Map // *logSink -> struct{}
	vpnHandlerMu  sync.RWMutex
	vpnHandler    VpnRequestHandler
)

type mobileRuntime struct {
	cfg       *config.Config
	store     *store.Store
	driver    frpcd.FrpDriver
	lifecycle *lifecycle.Manager
	authn     *auth.Authenticator
	engine    *gin.Engine
	httpSrv   *http.Server

	adminID       uint
	adminUsername string

	rootCtx    context.Context
	cancelRoot context.CancelFunc
}

// Start brings up the FrpDeck server on the supplied loopback address.
// All parameters are required (any empty string causes a descriptive error)
// except `instanceName`, which defaults to "frpdeck-android" for remote
// management invitations.
//
// Calling Start twice in a row without Stop returns an error — the Android
// foreground service is responsible for serialising the lifecycle.
//
// Errors carry plain English so they can be surfaced verbatim to the user
// via Toast/Snackbar; the Java side does not try to localise them.
func Start(dataDir, listenAddr, adminUsername, adminPassword, instanceName string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	if current != nil {
		return errors.New("frpdeck: already running; call Stop() first")
	}
	if dataDir == "" {
		return errors.New("frpdeck: dataDir required (use Context.getFilesDir())")
	}
	if listenAddr == "" {
		listenAddr = "127.0.0.1:18080"
	}
	if !strings.HasPrefix(listenAddr, "127.") && !strings.HasPrefix(listenAddr, "localhost:") {
		return fmt.Errorf("frpdeck: listenAddr must bind to loopback, got %q", listenAddr)
	}
	if adminUsername == "" {
		adminUsername = "admin"
	}
	if instanceName == "" {
		instanceName = "frpdeck-android"
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("frpdeck: create data dir: %w", err)
	}

	cfg := &config.Config{
		Listen:                  listenAddr,
		AuthMode:                config.AuthModePassword,
		AdminUsername:           adminUsername,
		AdminPassword:           adminPassword,
		FrpcdDriver:             "embedded",
		DataDir:                 dataDir,
		HistoryRetentionDays:    30,
		MaxDurationHours:        24,
		MaxRulesPerIP:           20,
		RateLimitPerMinutePerIP: 30,
		InstanceName:            instanceName,

		LoginFailMaxPerIP:      30,
		LoginFailWindowIPMin:   10,
		LoginFailMaxPerUser:    10,
		LoginFailWindowUserMin: 15,
		LoginLockoutIPMin:      5,
		LoginLockoutUserMin:    10,
		LoginMinPasswordLen:    6,
	}

	rt, err := startInternal(cfg)
	if err != nil {
		return err
	}
	current = rt
	return nil
}

// startInternal is the shared boot sequence. Pulled out of Start so the
// mutex stays narrowly scoped and so unit tests (built without the
// gomobile target) can drive the same path.
func startInternal(cfg *config.Config) (*mobileRuntime, error) {
	dbPath := filepath.Join(cfg.DataDir, "frpdeck.db")
	s, err := store.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("frpdeck: store: %w", err)
	}
	adminID, err := s.SeedAdminIfEmpty(cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return nil, fmt.Errorf("frpdeck: seed admin: %w", err)
	}
	adminUsername := cfg.AdminUsername
	if adminUsername == "" {
		adminUsername = store.DefaultAdminUsername
	}

	drv, err := frpcd.NewDriver(cfg.FrpcdDriver, frpcd.DriverOptions{DataDir: cfg.DataDir})
	if err != nil {
		return nil, fmt.Errorf("frpdeck: frpcd driver: %w", err)
	}

	rs := runtime.New(cfg)
	if err := rs.LoadFromKV(s.LookupSetting); err != nil {
		log.Printf("frpdeck: load persisted settings: %v", err)
	}

	lm := lifecycle.New(s, drv, 30*time.Second, &lifecycle.Options{
		Publish:         drv.PublishEvent,
		ExpiringMinutes: rs.TunnelExpiringNotifyMinutes,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := lm.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("frpdeck: lifecycle start: %w", err)
	}

	notifier := notify.New(rs)
	captchaSvc := captcha.New(rs, s)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	authn := auth.New(cfg, rs, s)
	authn.SetSystemAdmin(adminID, adminUsername)
	authn.SetCaptcha(captchaSvc)
	authn.SetNotifier(notifier)
	apiSrv := api.New(cfg, rs, s, lm, drv, authn, captchaSvc, notifier)
	apiSrv.Router(r)
	mountStatic(r)

	// Subscribe driver event bus to log handlers + VPN-request
	// dispatcher. The mobile UI surfaces logs as the live "logs"
	// panel without polling; VpnRequestHandler is consulted when a
	// tunnel transitions to active so the Android shell can bring
	// up VpnService for SOCKS5 visitor tunnels.
	go pumpEvents(ctx, drv, s)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		cancel()
		lm.Stop()
		return nil, fmt.Errorf("frpdeck: listen %s: %w", cfg.Listen, err)
	}
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("frpdeck: http: %v", err)
		}
	}()

	return &mobileRuntime{
		cfg:           cfg,
		store:         s,
		driver:        drv,
		lifecycle:     lm,
		authn:         authn,
		engine:        r,
		httpSrv:       httpSrv,
		adminID:       adminID,
		adminUsername: adminUsername,
		rootCtx:       ctx,
		cancelRoot:    cancel,
	}, nil
}

// Stop shuts the server down. Safe to call multiple times — second
// invocation returns nil. Blocks for at most 10 seconds while in-flight
// HTTP requests drain.
func Stop() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if current == nil {
		return nil
	}
	rt := current
	current = nil

	if rt.httpSrv != nil {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = rt.httpSrv.Shutdown(shutdown)
		cancel()
	}
	if rt.cancelRoot != nil {
		rt.cancelRoot()
	}
	if rt.lifecycle != nil {
		rt.lifecycle.Stop()
	}
	return nil
}

// IsRunning reports whether Start has succeeded and Stop has not been
// called. Returns false during the brief window inside Start before the
// HTTP listener accepts.
func IsRunning() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return current != nil
}

// ListenAddr returns the bound loopback address (e.g. "127.0.0.1:18080")
// so the Compose UI knows which port to point Retrofit / WebView at.
// Empty when the server is not running.
func ListenAddr() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	if current == nil {
		return ""
	}
	return current.cfg.Listen
}

// AdminToken self-issues a 24h JWT for the seed admin so the WebView and
// Compose API client can skip the login flow. The returned string follows
// the standard `Bearer <token>` Authorization header convention (without
// the "Bearer " prefix).
//
// Returns "" if the server is not running or the JWT secret is missing.
func AdminToken() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	if current == nil {
		return ""
	}
	user := &model.User{
		ID:       current.adminID,
		Username: current.adminUsername,
		Role:     model.RoleAdmin,
	}
	tok, err := current.authn.IssueAccessToken(user)
	if err != nil {
		log.Printf("frpdeck: mint admin token: %v", err)
		return ""
	}
	return tok
}

// AddLogHandler registers a Java/Kotlin callback that fires for every
// driver event (endpoint state change, tunnel state change, log line).
// The handler is invoked on a background Go goroutine; the Java side
// must marshal back to the main thread if it touches UI.
//
// The same handler may be added multiple times; each registration is
// independent. RemoveLogHandler removes one registration. To detach all
// handlers (e.g. on Service.onDestroy) call ClearLogHandlers.
//
// Returns a non-empty handle string the caller can pass to
// RemoveLogHandler. The handle is opaque — do not parse it.
func AddLogHandler(h LogHandler) string {
	if h == nil {
		return ""
	}
	sink := &logSink{handler: h}
	logSinks.Store(sink, struct{}{})
	return fmt.Sprintf("%p", sink)
}

// RemoveLogHandler detaches a previously registered handler by handle.
// Unknown handles are silently ignored.
func RemoveLogHandler(handle string) {
	if handle == "" {
		return
	}
	logSinks.Range(func(k, _ any) bool {
		if fmt.Sprintf("%p", k) == handle {
			logSinks.Delete(k)
			return false
		}
		return true
	})
}

// ClearLogHandlers detaches every registered handler.
func ClearLogHandlers() {
	logSinks.Range(func(k, _ any) bool {
		logSinks.Delete(k)
		return true
	})
}

// SetVpnRequestHandler installs the single Java handler that receives
// "tunnel needs VPN" callbacks. Passing nil clears the handler. Setting
// a new handler over an existing one drops the previous registration —
// per-process semantics match the Android single-foreground-service
// model.
//
// Safe to call before or after Start; the handler is consulted only when
// a tunnel actually transitions to active. The Android shell typically
// registers in `FrpDeckForegroundService.onCreate` and clears in
// `onDestroy` to avoid leaks across service restarts.
func SetVpnRequestHandler(h VpnRequestHandler) {
	vpnHandlerMu.Lock()
	defer vpnHandlerMu.Unlock()
	vpnHandler = h
}

// ClearVpnRequestHandler is sugar for `SetVpnRequestHandler(nil)`. The
// distinct method exists because gomobile-generated bindings make
// nullable parameters awkward on the Java side, so callers prefer a
// no-arg method when they want to detach.
func ClearVpnRequestHandler() {
	vpnHandlerMu.Lock()
	defer vpnHandlerMu.Unlock()
	vpnHandler = nil
}

// dispatchVpnRequest fires the registered VpnRequestHandler if any. The
// caller is expected to have already filtered down to tunnels that
// actually need device-level routing (frpcd.TunnelRequiresSystemRoute).
//
// The dispatch is best-effort: panics inside the Java handler are
// recovered so a buggy implementation cannot crash the engine goroutine.
func dispatchVpnRequest(t *model.Tunnel) {
	if t == nil {
		return
	}
	vpnHandlerMu.RLock()
	h := vpnHandler
	vpnHandlerMu.RUnlock()
	if h == nil {
		return
	}
	ip := t.LocalIP
	if ip == "" {
		ip = "127.0.0.1"
	}
	if t.LocalPort <= 0 {
		// No port bound yet — the user is likely still configuring the
		// tunnel. Skipping dispatch is correct because the eventual
		// transition to a real port will re-trigger this codepath.
		return
	}
	url := fmt.Sprintf("socks5://%s:%d", ip, t.LocalPort)
	go func(tunnelID int, tunnelName, socks5URL string) {
		defer func() { _ = recover() }()
		h.OnVpnRequest(tunnelID, tunnelName, socks5URL)
	}(int(t.ID), t.Name, url)
}

type logSink struct {
	handler LogHandler
}

// pumpEvents fans the driver bus to every registered LogHandler. Slow
// handlers do not back-pressure the bus (the bus itself drops slow
// consumers, see frpcd/event.go), so we keep this goroutine simple.
//
// In addition to fanning out log lines, this loop watches for tunnel
// transitions to active/running and — when the tunnel matches the
// "needs system route" rule — fires the VpnRequestHandler so the
// Android shell can bring VpnService up. The check is cheap (single DB
// row lookup) and only runs on tunnel-state events, so the embedded
// driver's 3-second poll cadence dictates the upper bound on latency.
func pumpEvents(ctx context.Context, drv frpcd.FrpDriver, st *store.Store) {
	ch, cancel := drv.Subscribe()
	defer cancel()

	flush := func(line string) {
		logSinks.Range(func(k, _ any) bool {
			sink := k.(*logSink)
			func() {
				defer func() { _ = recover() }()
				sink.handler.OnLog(line)
			}()
			return true
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			line := formatEvent(ev)
			if line != "" {
				flush(line)
			}
			if ev.Type == frpcd.EventTunnelState && ev.TunnelID > 0 && isLiveState(ev.State) {
				if t, err := st.GetTunnel(ev.TunnelID); err == nil && t != nil && frpcd.TunnelRequiresSystemRoute(t) {
					dispatchVpnRequest(t)
				}
			}
		}
	}
}

// isLiveState returns true for the frp/lifecycle phase strings that
// indicate "the tunnel is operational and accepting traffic now". The
// list mirrors the StatusActive normalisation done by the embedded /
// subprocess drivers (see internal/frpcd/embedded.go::translateState).
func isLiveState(state string) bool {
	switch state {
	case "running", "active", "ready", "up":
		return true
	}
	return false
}

func formatEvent(ev frpcd.Event) string {
	switch ev.Type {
	case frpcd.EventLog:
		if ev.Level != "" {
			return fmt.Sprintf("[%s] %s", ev.Level, ev.Msg)
		}
		return ev.Msg
	case frpcd.EventTunnelState:
		if ev.Err != "" {
			return fmt.Sprintf("[tunnel %d] %s — %s", ev.TunnelID, ev.State, ev.Err)
		}
		return fmt.Sprintf("[tunnel %d] %s", ev.TunnelID, ev.State)
	case frpcd.EventEndpointState:
		if ev.Err != "" {
			return fmt.Sprintf("[endpoint %d] %s — %s", ev.EndpointID, ev.State, ev.Err)
		}
		return fmt.Sprintf("[endpoint %d] %s", ev.EndpointID, ev.State)
	case frpcd.EventTunnelExpiring:
		return fmt.Sprintf("[tunnel %d] expiring in %ss", ev.TunnelID, ev.State)
	default:
		return ""
	}
}

// mountStatic re-implements the same fallback served by the headless
// build, ensuring the embedded `dist/` is reachable through the loopback
// listener so the Android WebView can render the UI directly.
func mountStatic(r *gin.Engine) {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		r.NoRoute(func(c *gin.Context) {
			c.String(http.StatusOK, "FrpDeck mobile: frontend assets not embedded")
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
		c.String(http.StatusOK, "FrpDeck mobile is running.")
	})
}

// Version returns a short string describing the bundled frp version
// the embedded driver speaks. The Android UI uses this to render
// "FrpDeck — frp v0.68.1" in the about screen.
func Version() string {
	return "frp " + frpcd.BundledFrpVersion
}
