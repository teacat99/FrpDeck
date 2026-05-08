// Pin golang.org/x/mobile/bind so module-aware gomobile/gobind keeps it in
// go.mod. Without an explicit reference, `go mod tidy` would treat this
// dependency as unused (FrpDeck never imports the binding API directly)
// and the next `gomobile bind` invocation would fail with:
//
//	unable to import bind: no Go package in golang.org/x/mobile/bind
//
// The package is platform-agnostic (it only declares the binding API
// surface; runtime glue lives in bind/java, bind/objc etc.), so the
// blank import compiles cleanly on every Go target FrpDeck ships:
// linux/darwin/windows for Docker / NAS / Wails, plus the gomobile
// android/arm64 cross-compile path.
package frpdeckmobile

import _ "golang.org/x/mobile/bind"
