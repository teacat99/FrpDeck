// Package web embeds the built frontend assets into the Go binary.
//
// The frontend build is expected to emit files into `web/dist`. Until the
// frontend is wired up (M3), a placeholder is kept so the embed directive
// compiles cleanly.
package web

import "embed"

//go:embed all:dist
var FS embed.FS
