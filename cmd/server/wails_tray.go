//go:build wails && (linux || windows)

// System tray menu for FrpDeck desktop.
//
// Tray actions are implemented in two flavours:
//
//  1. Window operations (show/hide, navigate to a route) ride the
//     Wails runtime API — they only need the wails ctx.
//  2. Domain operations (start/stop tunnel, toggle endpoint enabled)
//     are issued as in-process HTTP requests against the embedded gin
//     engine using a self-issued admin JWT. This keeps the audit log
//     and ntfy notifications consistent with browser usage.
//
// The menu is rebuilt every refreshInterval so endpoint/tunnel lists
// reflect freshly added or removed records without restarting the
// desktop. Live state (running/stopped/connected/...) updates ride on
// the same tick — we deliberately keep the tray decoupled from the
// WebSocket bus to avoid backpressure complications inside the menu
// thread.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/teacat99/FrpDeck/internal/model"
)

const trayRefreshInterval = 5 * time.Second

var (
	trayState  atomic.Pointer[desktopState]
	trayCancel context.CancelFunc
	trayMu     sync.Mutex
	trayToken  atomic.Pointer[string]
)

// startTray boots the tray on a dedicated goroutine. fyne.io/systray
// internally locks an OS thread, which is fine here because we never
// touch the wails main thread from inside the tray handlers.
func startTray(d *desktopState) {
	trayState.Store(d)
	mintTrayToken(d)

	go systray.Run(onTrayReady, onTrayExit)
}

// stopTray asks systray to tear down its menu. Called from
// OnShutdown. systray.Quit() is safe to call multiple times.
func stopTray() {
	trayMu.Lock()
	if trayCancel != nil {
		trayCancel()
		trayCancel = nil
	}
	trayMu.Unlock()
	systray.Quit()
}

// onTrayReady builds the static skeleton (header + actions that never
// change) and kicks off the refresh loop that owns the dynamic
// submenus (per-endpoint, per-tunnel, status summary).
func onTrayReady() {
	d := trayState.Load()
	if d == nil {
		return
	}

	systray.SetIcon(trayIconPNG())
	systray.SetTitle("FrpDeck")
	systray.SetTooltip("FrpDeck — frpc manager")

	header := systray.AddMenuItem("FrpDeck", "FrpDeck desktop")
	header.Disable()
	statusItem := systray.AddMenuItem("…", "Endpoint / tunnel summary")
	statusItem.Disable()
	systray.AddSeparator()

	showItem := systray.AddMenuItem("显示窗口 / Show", "Bring the FrpDeck window to the foreground")
	hideItem := systray.AddMenuItem("隐藏窗口 / Hide", "Hide the FrpDeck window (tray stays alive)")
	systray.AddSeparator()

	// Submenus whose items we rebuild every tick. systray does not
	// expose "remove all children", so we keep parent menus stable
	// and clear/reapply children inside the refresh loop.
	endpointsRoot := systray.AddMenuItem("端点 / Endpoints", "Per-endpoint quick actions")
	tunnelsRoot := systray.AddMenuItem("隧道 / Tunnels", "Per-tunnel quick actions")
	systray.AddSeparator()

	pagesRoot := systray.AddMenuItem("打开页面 / Open page", "Navigate the FrpDeck window")
	page("/", pagesRoot, "概览 / Dashboard")
	page("/endpoints", pagesRoot, "端点 / Endpoints")
	page("/tunnels", pagesRoot, "隧道 / Tunnels")
	page("/users", pagesRoot, "用户 / Users")
	page("/settings", pagesRoot, "设置 / Settings")
	page("/audit", pagesRoot, "审计 / Audit log")
	systray.AddSeparator()

	quitItem := systray.AddMenuItem("退出 / Quit", "Close FrpDeck completely")

	go func() {
		for {
			select {
			case <-showItem.ClickedCh:
				if ctx := d.ctx(); ctx != nil {
					wailsruntime.WindowShow(ctx)
					wailsruntime.WindowUnminimise(ctx)
				}
			case <-hideItem.ClickedCh:
				if ctx := d.ctx(); ctx != nil {
					wailsruntime.WindowHide(ctx)
				}
			case <-quitItem.ClickedCh:
				if ctx := d.ctx(); ctx != nil {
					wailsruntime.Quit(ctx)
				}
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	trayMu.Lock()
	trayCancel = cancel
	trayMu.Unlock()

	go refreshLoop(ctx, d, statusItem, endpointsRoot, tunnelsRoot)
}

func onTrayExit() {
	// nothing to do; the wails OnShutdown hook owns the runtime
}

// page registers a navigation submenu item that rewrites the embedded
// webview's location when clicked. The hash routing assumption keeps
// the snippet simple — if we ever switch back to history mode this
// becomes window.history.pushState + a popstate event.
func page(path string, parent *systray.MenuItem, label string) {
	item := parent.AddSubMenuItem(label, "")
	go func() {
		for range item.ClickedCh {
			d := trayState.Load()
			if d == nil {
				continue
			}
			ctx := d.ctx()
			if ctx == nil {
				continue
			}
			wailsruntime.WindowShow(ctx)
			wailsruntime.WindowUnminimise(ctx)
			js := fmt.Sprintf("window.location.assign(%q)", path)
			wailsruntime.WindowExecJS(ctx, js)
		}
	}()
}

// refreshLoop owns the dynamic parts of the menu: status summary +
// endpoint/tunnel submenus. We rebuild the children every tick — that
// is cheaper than diffing and avoids subtle races with click handler
// goroutines that may still be sleeping on a removed item.
func refreshLoop(ctx context.Context, d *desktopState, status *systray.MenuItem, endpointsRoot, tunnelsRoot *systray.MenuItem) {
	tick := time.NewTicker(trayRefreshInterval)
	defer tick.Stop()

	var (
		epItems  []*systray.MenuItem
		tnItems  []*systray.MenuItem
		clickMu  sync.Mutex
		clickGen int64
	)

	rebuild := func() {
		eps, err := d.rt.Store.ListEndpoints()
		if err != nil {
			log.Printf("tray: list endpoints: %v", err)
			return
		}
		tns, err := d.rt.Store.ListTunnels()
		if err != nil {
			log.Printf("tray: list tunnels: %v", err)
			return
		}
		sort.Slice(eps, func(i, j int) bool { return eps[i].Name < eps[j].Name })
		sort.Slice(tns, func(i, j int) bool {
			if tns[i].EndpointID != tns[j].EndpointID {
				return tns[i].EndpointID < tns[j].EndpointID
			}
			return tns[i].Name < tns[j].Name
		})

		clickMu.Lock()
		clickGen++
		gen := clickGen
		// Hide previous items so their click goroutines see the new
		// generation and exit. systray cannot delete items but Hide
		// removes them from the visible menu, which is what users
		// expect after a rebuild.
		for _, it := range epItems {
			it.Hide()
		}
		for _, it := range tnItems {
			it.Hide()
		}
		epItems = epItems[:0]
		tnItems = tnItems[:0]
		clickMu.Unlock()

		// Status summary
		runningTunnels := 0
		for _, t := range tns {
			if t.Status == model.StatusActive {
				runningTunnels++
			}
		}
		enabledEndpoints := 0
		for _, e := range eps {
			if e.Enabled {
				enabledEndpoints++
			}
		}
		status.SetTitle(fmt.Sprintf("端点: %d/%d 启用 · 隧道: %d/%d 运行",
			enabledEndpoints, len(eps), runningTunnels, len(tns)))

		// Endpoints submenu
		if len(eps) == 0 {
			noop := endpointsRoot.AddSubMenuItem("（无端点）", "")
			noop.Disable()
			epItems = append(epItems, noop)
		}
		for _, ep := range eps {
			ep := ep
			label := fmt.Sprintf("%s — %s", endpointStateGlyph(ep), ep.Name)
			parent := endpointsRoot.AddSubMenuItem(label, fmt.Sprintf("%s:%d", ep.Addr, ep.Port))
			toggle := parent.AddSubMenuItem(toggleLabel(ep.Enabled), "Toggle enabled flag")
			restart := parent.AddSubMenuItem("重启 / Restart", "Restart the endpoint connection")
			epItems = append(epItems, parent, toggle, restart)

			go func(genWanted int64) {
				for {
					select {
					case <-toggle.ClickedCh:
						if currentGen(&clickMu, &clickGen) != genWanted {
							return
						}
						doToggleEndpoint(d, ep.ID, !ep.Enabled)
					case <-restart.ClickedCh:
						if currentGen(&clickMu, &clickGen) != genWanted {
							return
						}
						doRestartEndpoint(d, ep.ID)
					case <-ctx.Done():
						return
					}
				}
			}(gen)
		}

		// Tunnels submenu
		if len(tns) == 0 {
			noop := tunnelsRoot.AddSubMenuItem("（无隧道）", "")
			noop.Disable()
			tnItems = append(tnItems, noop)
		}
		for _, tn := range tns {
			tn := tn
			label := fmt.Sprintf("%s — %s/%s", tunnelStateGlyph(tn), endpointName(eps, tn.EndpointID), tn.Name)
			parent := tunnelsRoot.AddSubMenuItem(label, tn.Type)
			start := parent.AddSubMenuItem("启动 / Start", "Start the tunnel")
			stop := parent.AddSubMenuItem("停止 / Stop", "Stop the tunnel")
			tnItems = append(tnItems, parent, start, stop)

			switch tn.Status {
			case model.StatusActive:
				start.Disable()
			case model.StatusStopped, model.StatusExpired, model.StatusFailed:
				stop.Disable()
			}

			go func(genWanted int64) {
				for {
					select {
					case <-start.ClickedCh:
						if currentGen(&clickMu, &clickGen) != genWanted {
							return
						}
						doStartTunnel(d, tn.ID)
					case <-stop.ClickedCh:
						if currentGen(&clickMu, &clickGen) != genWanted {
							return
						}
						doStopTunnel(d, tn.ID)
					case <-ctx.Done():
						return
					}
				}
			}(gen)
		}
	}

	rebuild()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			rebuild()
		}
	}
}

func currentGen(mu *sync.Mutex, gen *int64) int64 {
	mu.Lock()
	defer mu.Unlock()
	return *gen
}

func endpointName(eps []model.Endpoint, id uint) string {
	for _, e := range eps {
		if e.ID == id {
			return e.Name
		}
	}
	return fmt.Sprintf("endpoint#%d", id)
}

// endpointStateGlyph mirrors the badge palette used in the web UI.
func endpointStateGlyph(e model.Endpoint) string {
	if !e.Enabled {
		return "○"
	}
	return "●"
}

func tunnelStateGlyph(t model.Tunnel) string {
	switch t.Status {
	case model.StatusActive:
		return "●"
	case model.StatusFailed:
		return "✗"
	case model.StatusExpired:
		return "⌛"
	default:
		return "○"
	}
}

func toggleLabel(enabled bool) string {
	if enabled {
		return "禁用 / Disable"
	}
	return "启用 / Enable"
}

// ----- HTTP self-call helpers ------------------------------------

// mintTrayToken self-issues a 24h JWT for the seed admin so tray
// actions can call /api/* with normal Authorization headers. Refresh
// is on-demand via call() to keep the path linear.
func mintTrayToken(d *desktopState) {
	user := &model.User{
		ID:       d.rt.AdminID,
		Username: d.rt.AdminUsername,
		Role:     model.RoleAdmin,
	}
	tok, err := d.rt.Auth.IssueAccessToken(user)
	if err != nil {
		log.Printf("tray: mint token: %v", err)
		return
	}
	trayToken.Store(&tok)
}

// call performs an in-process HTTP request via the gin engine using
// httptest.ResponseRecorder; this avoids relying on the LAN listener
// (which the user may have disabled) and bypasses the network stack.
func call(d *desktopState, method, path string, body any) (int, []byte, error) {
	tokPtr := trayToken.Load()
	if tokPtr == nil {
		mintTrayToken(d)
		tokPtr = trayToken.Load()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if tokPtr != nil {
		req.Header.Set("Authorization", "Bearer "+*tokPtr)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	d.rt.Engine.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		// Token may have expired (rare for desktop session, but we
		// re-mint and retry once).
		mintTrayToken(d)
		tokPtr = trayToken.Load()
		req2 := httptest.NewRequest(method, path, reader)
		if tokPtr != nil {
			req2.Header.Set("Authorization", "Bearer "+*tokPtr)
		}
		if body != nil {
			req2.Header.Set("Content-Type", "application/json")
		}
		rec = httptest.NewRecorder()
		d.rt.Engine.ServeHTTP(rec, req2)
	}
	return rec.Code, rec.Body.Bytes(), nil
}

func doToggleEndpoint(d *desktopState, id uint, enabled bool) {
	ep, err := d.rt.Store.GetEndpoint(id)
	if err != nil || ep == nil {
		log.Printf("tray: toggle endpoint %d lookup: %v", id, err)
		return
	}
	payload := map[string]any{
		"name":               ep.Name,
		"group":              ep.Group,
		"addr":               ep.Addr,
		"port":               ep.Port,
		"protocol":           ep.Protocol,
		"user":               ep.User,
		"tls_enable":         ep.TLSEnable,
		"tls_config":         ep.TLSConfig,
		"pool_count":         ep.PoolCount,
		"heartbeat_interval": ep.HeartbeatInterval,
		"heartbeat_timeout":  ep.HeartbeatTimeout,
		"driver_mode":        ep.DriverMode,
		"subprocess_path":    ep.SubprocessPath,
		"enabled":            enabled,
		"auto_start":         ep.AutoStart,
	}
	code, body, err := call(d, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", id), payload)
	if err != nil || code >= 400 {
		log.Printf("tray: toggle endpoint %d → %d %s err=%v", id, code, string(body), err)
	}
}

func doRestartEndpoint(d *desktopState, id uint) {
	if d.rt.Driver == nil {
		return
	}
	ep, err := d.rt.Store.GetEndpoint(id)
	if err != nil || ep == nil {
		log.Printf("tray: restart endpoint %d lookup: %v", id, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.rt.Driver.Stop(ctx, ep); err != nil {
		log.Printf("tray: restart endpoint %d stop: %v", id, err)
	}
	if !ep.Enabled {
		return
	}
	if err := d.rt.Driver.Start(ctx, ep); err != nil {
		log.Printf("tray: restart endpoint %d start: %v", id, err)
	}
}

func doStartTunnel(d *desktopState, id uint) {
	code, body, err := call(d, http.MethodPost, fmt.Sprintf("/api/tunnels/%d/start", id), nil)
	if err != nil || code >= 400 {
		log.Printf("tray: start tunnel %d → %d %s err=%v", id, code, string(body), err)
	}
}

func doStopTunnel(d *desktopState, id uint) {
	code, body, err := call(d, http.MethodPost, fmt.Sprintf("/api/tunnels/%d/stop", id), nil)
	if err != nil || code >= 400 {
		log.Printf("tray: stop tunnel %d → %d %s err=%v", id, code, string(body), err)
	}
}

// ----- Icon -------------------------------------------------------

// trayIconPNG produces a 64x64 PNG of the FrpDeck wordmark stripe so
// we don't have to ship a binary asset just for the tray. The colours
// echo the Tailwind palette used in the web UI: slate/violet rim with
// an accent bar.
func trayIconPNG() []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 0x1f, G: 0x29, B: 0x37, A: 0xff}
	accent := color.RGBA{R: 0x7c, G: 0x3a, B: 0xed, A: 0xff}
	tip := color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := bg
			if x > size/2-4 && x < size/2+4 {
				c = accent
			}
			if y > 8 && y < 14 && x > 8 && x < size-8 {
				c = tip
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
