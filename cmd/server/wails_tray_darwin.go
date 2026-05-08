//go:build wails && darwin

// macOS system tray menu for FrpDeck desktop.
//
// Why a hand-rolled cgo bridge instead of fyne.io/systray?
//
// fyne.io/systray on darwin spins up its own NSApplication and locks
// the main thread for the duration of systray.Run. Wails already owns
// the main thread (NSApplicationMain), so the two cannot coexist.
// AppKit, however, is perfectly happy with extra NSStatusItems being
// added from any thread as long as the actual UIKit/AppKit calls are
// dispatched onto the main queue, which is what wails_tray_darwin.m
// does for every entry point below.
//
// Scope
// -----
// The v0.1 darwin menu carries the same window-control + page-jump
// surface as the Linux/Windows tray (Show / Hide / Open Dashboard|
// Endpoints|Tunnels|Users|Settings|Audit / Quit) and a 5-second
// status summary that mirrors the wails_tray.go count line. Per-
// endpoint and per-tunnel quick actions stay on the Linux/Windows
// build for now — they require a Cocoa-side delegate that can be
// rebuilt every tick, which is a v0.2 polish item tracked under
// §14.2.3 B「macOS 托盘 endpoint/tunnel submenu」.
//
// The action callback (goTrayDarwinAction) is the only Go entry the
// ObjC layer pokes; we keep the surface tiny on purpose so the
// generated _cgo_export.h header stays trivial and the linker has no
// trouble resolving symbols across the cgo boundary.
package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

extern void tray_darwin_init(const char* title, const char* tooltip);
extern void tray_darwin_set_status(const char* status);
extern void tray_darwin_quit(void);
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/teacat99/FrpDeck/internal/model"
)

const trayDarwinRefreshInterval = 5 * time.Second

// Action IDs match the menu items wired up in wails_tray_darwin.m.
// Keep these in sync with that file — the values are part of the
// implicit ABI between Go and ObjC.
const (
	trayDarwinActionShow int = 1
	trayDarwinActionHide int = 2
	trayDarwinActionQuit int = 3

	trayDarwinActionOpenDashboard int = 10
	trayDarwinActionOpenEndpoints int = 11
	trayDarwinActionOpenTunnels   int = 12
	trayDarwinActionOpenUsers     int = 13
	trayDarwinActionOpenSettings  int = 14
	trayDarwinActionOpenAudit     int = 15
)

var (
	trayDarwinState  atomic.Pointer[desktopState]
	trayDarwinCancel context.CancelFunc
	trayDarwinMu     sync.Mutex
)

// startTray boots the Cocoa NSStatusItem and kicks off the status
// summary refresh loop. Called from the Wails OnStartup hook, which
// already runs on a goroutine spawned by Wails — we can therefore
// kick off long-lived loops without worrying about blocking the main
// run loop.
func startTray(d *desktopState) {
	trayDarwinState.Store(d)

	title := C.CString("FrpDeck")
	tooltip := C.CString("FrpDeck — frpc manager")
	defer C.free(unsafe.Pointer(title))
	defer C.free(unsafe.Pointer(tooltip))
	C.tray_darwin_init(title, tooltip)

	ctx, cancel := context.WithCancel(context.Background())
	trayDarwinMu.Lock()
	if trayDarwinCancel != nil {
		trayDarwinCancel()
	}
	trayDarwinCancel = cancel
	trayDarwinMu.Unlock()

	go trayDarwinRefreshLoop(ctx, d)
}

// stopTray tears the NSStatusItem down. Wails calls this from
// OnShutdown; we mirror the Linux/Windows contract by being safe to
// invoke multiple times.
func stopTray() {
	trayDarwinMu.Lock()
	if trayDarwinCancel != nil {
		trayDarwinCancel()
		trayDarwinCancel = nil
	}
	trayDarwinMu.Unlock()
	C.tray_darwin_quit()
}

// trayDarwinRefreshLoop owns the status-summary line. Per-endpoint /
// per-tunnel rows are intentionally absent on darwin v0.1 — see the
// package doc.
func trayDarwinRefreshLoop(ctx context.Context, d *desktopState) {
	tick := time.NewTicker(trayDarwinRefreshInterval)
	defer tick.Stop()

	render := func() {
		eps, err := d.rt.Store.ListEndpoints()
		if err != nil {
			log.Printf("tray(darwin): list endpoints: %v", err)
			return
		}
		tns, err := d.rt.Store.ListTunnels()
		if err != nil {
			log.Printf("tray(darwin): list tunnels: %v", err)
			return
		}
		enabled := 0
		for _, e := range eps {
			if e.Enabled {
				enabled++
			}
		}
		running := 0
		for _, t := range tns {
			if t.Status == model.StatusActive {
				running++
			}
		}
		summary := fmt.Sprintf("端点: %d/%d 启用 · 隧道: %d/%d 运行",
			enabled, len(eps), running, len(tns))
		c := C.CString(summary)
		C.tray_darwin_set_status(c)
		C.free(unsafe.Pointer(c))
	}

	render()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			render()
		}
	}
}

//export goTrayDarwinAction
func goTrayDarwinAction(actionID C.int) {
	d := trayDarwinState.Load()
	if d == nil {
		return
	}
	wctx := d.ctx()
	if wctx == nil {
		return
	}
	switch int(actionID) {
	case trayDarwinActionShow:
		wailsruntime.WindowShow(wctx)
		wailsruntime.WindowUnminimise(wctx)
	case trayDarwinActionHide:
		wailsruntime.WindowHide(wctx)
	case trayDarwinActionQuit:
		wailsruntime.Quit(wctx)
	case trayDarwinActionOpenDashboard:
		trayDarwinNavigate(wctx, "/")
	case trayDarwinActionOpenEndpoints:
		trayDarwinNavigate(wctx, "/endpoints")
	case trayDarwinActionOpenTunnels:
		trayDarwinNavigate(wctx, "/tunnels")
	case trayDarwinActionOpenUsers:
		trayDarwinNavigate(wctx, "/users")
	case trayDarwinActionOpenSettings:
		trayDarwinNavigate(wctx, "/settings")
	case trayDarwinActionOpenAudit:
		trayDarwinNavigate(wctx, "/audit")
	default:
		log.Printf("tray(darwin): unknown action %d", int(actionID))
	}
}

func trayDarwinNavigate(wctx context.Context, path string) {
	wailsruntime.WindowShow(wctx)
	wailsruntime.WindowUnminimise(wctx)
	js := fmt.Sprintf("window.location.assign(%q)", path)
	wailsruntime.WindowExecJS(wctx, js)
}
