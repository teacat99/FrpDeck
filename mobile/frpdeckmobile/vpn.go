// VPN bridge for the gomobile frpdeckmobile package.
//
// xjasonlyu/tun2socks/v2 exposes a tiny package surface (engine.Insert
// + engine.Start/Stop) that maps cleanly onto a Java/Kotlin
// foreground/VpnService. This file wraps those calls so the Android
// side only sees four mobile-friendly entry points:
//
//	VpnAttach(fd, mtu, socks5Addr) error
//	VpnDetach() error
//	VpnIsRunning() bool
//	VpnEngineVersion() string
//
// The xjasonlyu engine accepts `fd://<int>` for `Device`, dup()ing the
// file descriptor itself (it does NOT close the original), so the Java
// side keeps owning the ParcelFileDescriptor. That ownership rule is
// the single most error-prone part of integrating tun2socks with
// VpnService — keep the doc above the implementation in sync if the
// upstream behaviour changes.
package frpdeckmobile

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

var (
	vpnMu      sync.Mutex
	vpnRunning bool
)

// VpnAttach hands the tun fd to xjasonlyu/tun2socks and starts the
// engine. The proxy URL must be reachable from the FrpDeck process —
// the typical pattern is to point it at a SOCKS5 listener exposed by a
// frpc visitor (for example "socks5://127.0.0.1:1080"). When the proxy
// is empty we default to "direct://" which routes packets back to the
// host network unchanged; useful for smoke-testing the tun fd path.
//
// Calling VpnAttach when the engine is already running returns an
// error — call VpnDetach first. The error is plain English so the
// Android Toast can surface it verbatim.
func VpnAttach(fd int, mtu int, proxyURL string) error {
	vpnMu.Lock()
	defer vpnMu.Unlock()
	if vpnRunning {
		return errors.New("frpdeck-vpn: engine already running")
	}
	if fd <= 0 {
		return fmt.Errorf("frpdeck-vpn: invalid tun fd %d", fd)
	}
	if mtu <= 0 {
		mtu = 1500
	}
	proxy := strings.TrimSpace(proxyURL)
	if proxy == "" {
		proxy = "direct://"
	}

	// Insert overwrites any previous Key the engine had cached.
	engine.Insert(&engine.Key{
		Device:   fmt.Sprintf("fd://%d", fd),
		MTU:      mtu,
		Proxy:    proxy,
		LogLevel: "warning",
	})
	engine.Start()
	vpnRunning = true
	return nil
}

// VpnDetach stops the engine; safe to call when not attached. Does not
// close the tun fd — the Java side closes the ParcelFileDescriptor in
// FrpDeckVpnService.stopVpn().
func VpnDetach() error {
	vpnMu.Lock()
	defer vpnMu.Unlock()
	if !vpnRunning {
		return nil
	}
	engine.Stop()
	vpnRunning = false
	return nil
}

// VpnIsRunning lets the Android FrpDeckVpnService / WebView about
// panel reflect engine state without needing a parallel kotlin
// StateFlow. Cheap — pure bool read under the same mutex
// VpnAttach/VpnDetach use.
func VpnIsRunning() bool {
	vpnMu.Lock()
	defer vpnMu.Unlock()
	return vpnRunning
}

// VpnEngineVersion returns a short marker for the about screen so QA
// can confirm which tun2socks revision is bundled.
func VpnEngineVersion() string {
	return "xjasonlyu/tun2socks v2 (gvisor-stack)"
}
