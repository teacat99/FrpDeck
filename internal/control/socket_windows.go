//go:build windows

// Windows-flavoured socket handling.
//
// We use AF_UNIX rather than a named pipe because:
//  1. Windows 10 build 17063 (Apr 2018) and Windows Server 2019 added
//     native AF_UNIX support — far older than FrpDeck's Win10+ baseline.
//  2. Reusing the same listener / dialer code as Unix avoids forking
//     the protocol path for one platform.
//  3. We avoid pulling in Microsoft/go-winio just for one syscall.
//
// File permission is enforced by the parent directory's ACL (the
// service installer puts the data dir under %ProgramData%/frpdeck
// which is admin-writable only by default); chmod is a no-op on
// Windows so we deliberately skip it here.

package control

import (
	"fmt"
	"net"
	"os"
)

func listenSocket(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	// Best-effort: stamp 0600 so the inherited DACL gets a hint
	// even if the parent directory permissions are loose. Errors
	// are non-fatal — Windows file modes are advisory and most
	// callers will not be on a filesystem that honours them.
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

func dialSocket(path string) (net.Conn, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial unix %s: %w", path, err)
	}
	return c, nil
}
