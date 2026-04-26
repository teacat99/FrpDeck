//go:build wails && darwin

// macOS placeholder for the system tray.
//
// fyne.io/systray on macOS requires control of the main thread,
// which Wails already owns. Mixing the two reliably needs a custom
// NSApplication delegate that we do not want to hand-roll for v0.1.
//
// The desktop binary still works — users can drive the app from the
// window and the dock icon. A follow-up task in plan.md tracks the
// proper Cocoa-side integration (likely via `LSUIElement` + a
// Wails-native NSStatusItem helper).
package main

import "log"

func startTray(_ *desktopState) {
	log.Printf("desktop: tray menu unavailable on darwin (tracked: plan.md P1-D macOS tray)")
}

func stopTray() {}
