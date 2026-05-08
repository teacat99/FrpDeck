// Package main shares the version stamp between the headless and
// Wails-flavoured entry points.
//
// Why this lives in its own file:
//
//   - main.go is built with `//go:build !wails` (it owns the
//     kardianos/service subcommand dispatcher), so any `var` declared
//     there is invisible to main_wails.go (`//go:build wails`).
//   - main_wails.go calls `rt.StartControl(appVersion)` and therefore
//     needs the same symbol the headless build sees.
//
// Keeping the declaration tag-free here means both build flavours
// link against the same variable, and `-ldflags "-X
// 'main.appVersion=v0.7.0'"` plumbs through identically for Docker,
// systemd, NAS, and Wails artefacts. Bonus: tests built without
// either tag (e.g. `go test ./cmd/server/...`) also resolve cleanly.
package main

// appVersion is overridden at link time via
//
//	-ldflags "-X 'main.appVersion=v0.7.0'"
//
// so distribution channels (Docker images, GitHub Releases, NAS
// packages, Wails desktop bundles) can stamp the running binary with
// the same tag they were built from. Source-built / `go run`
// invocations keep the "dev" sentinel; runVersion in main.go falls
// back to debug.ReadBuildInfo() so users can still tell the dev
// build apart from a tagged release.
var appVersion = "dev"
