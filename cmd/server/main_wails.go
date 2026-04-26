//go:build wails

// Command frpdeck-desktop is the Wails-flavoured entry point for
// FrpDeck. The build tag `wails` swaps it in for the headless main
// without forcing the standard `go build ./...` consumer (CI, server
// images) to depend on webkit2gtk / WebView2.
//
// The bootstrap function is shared with the headless build, so the
// only delta here is the surrounding lifecycle: instead of waiting
// for SIGINT we run a Wails window and a system tray; instead of
// listening on cfg.Listen we mount the gin engine into the
// AssetServer so the embedded webview talks to the API in-process.
//
// HTTP listener strategy: by default we still also bring up the TCP
// listener on cfg.Listen so the user can reach the dashboard from
// other devices on the LAN. Setting FRPDECK_DESKTOP_LISTEN=off (or an
// empty cfg.Listen) skips the listener and keeps the desktop a
// purely-local affair.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// desktopState lives for the life of the Wails app. It carries the
// bootstrap runtime, the optional LAN listener, and the wails ctx so
// tray callbacks (which fire on a different goroutine) can drive
// window operations safely.
type desktopState struct {
	rt      *Runtime
	httpSrv *http.Server

	mu       sync.RWMutex
	wailsCtx context.Context
}

func (d *desktopState) ctx() context.Context {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.wailsCtx
}

func (d *desktopState) setCtx(ctx context.Context) {
	d.mu.Lock()
	d.wailsCtx = ctx
	d.mu.Unlock()
}

// shouldListenLAN honours the FRPDECK_DESKTOP_LISTEN escape hatch so
// "single-user, never expose to LAN" deployments can opt out.
func shouldListenLAN() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FRPDECK_DESKTOP_LISTEN")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	}
	return true
}

func main() {
	rt, err := bootstrap()
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	state := &desktopState{rt: rt}

	if shouldListenLAN() && rt.Cfg.Listen != "" {
		state.httpSrv = startHTTP(rt)
	} else {
		log.Printf("desktop: LAN listener disabled (FRPDECK_DESKTOP_LISTEN)")
	}

	// Wails' SingleInstanceLock raises a UNIX socket / named mutex on
	// the unique id and forwards args to the running instance, then
	// exits. We do not need the args, just the "raise & exit" effect.
	singleInstance := &options.SingleInstanceLock{
		UniqueId: "io.teacat.frpdeck",
		OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
			ctx := state.ctx()
			if ctx == nil {
				return
			}
			wailsruntime.WindowShow(ctx)
			wailsruntime.WindowUnminimise(ctx)
		},
	}

	err = wails.Run(&options.App{
		Title:              "FrpDeck",
		Width:              1280,
		Height:             800,
		MinWidth:           1024,
		MinHeight:          640,
		HideWindowOnClose:  true,
		SingleInstanceLock: singleInstance,
		AssetServer: &assetserver.Options{
			// Reuse the gin engine assembled in bootstrap. All
			// `/api/...` requests, the `/api/ws` WebSocket, and the
			// embedded `dist/` static files share this handler.
			Handler: rt.Engine,
		},
		OnStartup: func(ctx context.Context) {
			state.setCtx(ctx)
			startTray(state)
		},
		OnShutdown: func(_ context.Context) {
			stopTray()
			if state.httpSrv != nil {
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = state.httpSrv.Shutdown(shutdown)
				cancel()
			}
			rt.Close()
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
