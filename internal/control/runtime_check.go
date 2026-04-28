// Tiny build-tag-free helper that lets the cross-platform client
// code branch on "are we on Windows" without dragging in a runtime
// import everywhere.

package control

import "runtime"

func runtimeIsWindows() bool { return runtime.GOOS == "windows" }
