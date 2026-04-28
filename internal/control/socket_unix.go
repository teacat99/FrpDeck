//go:build !windows

// Unix-flavoured socket handling. On Linux, macOS, and the BSDs the
// control channel is a regular Unix domain socket protected by file
// permissions: 0600 means only the daemon's effective UID (and root)
// can dial it. We rely on the OS for access control because mixing a
// home-grown auth scheme into a local-only RPC would just multiply
// failure modes — the file system already does this job perfectly.

package control

import (
	"fmt"
	"net"
	"os"
)

// listenSocket creates the Unix domain socket and tightens its
// permissions to owner-only. We chmod after Listen because Listen
// honours the process umask, which we cannot control without
// affecting the rest of the daemon.
func listenSocket(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	return ln, nil
}

// dialSocket opens the client side of the Unix domain socket. The
// CLI passes the absolute path it computed from --data-dir; no
// further normalisation here.
func dialSocket(path string) (net.Conn, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial unix %s: %w", path, err)
	}
	return c, nil
}
