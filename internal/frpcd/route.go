// Routing-mode helpers (P6′/P7′).
//
// FrpDeck has one cross-cutting question that the driver layer is the
// natural home for: "does this particular tunnel need the host platform
// to mount a system-wide route?". On Android that question drives the
// VpnService lifecycle; on desktop / Docker the question is asked anyway
// (so the API surface is uniform) but the answer has no practical
// consequence because there is no VpnService.
//
// The rule is a single function deliberately kept here in `internal/frpcd`
// rather than `internal/model` so it can evolve alongside the driver
// without bouncing through the persistence layer. Callers:
//
//   - internal/api/vpn.go         → exposes /api/system/vpn/required
//   - mobile/frpdeckmobile/api.go → notifies the Android shell via the
//     VpnRequestHandler bridge whenever a tunnel transitions to active
//
// See plan.md §11 row "P6′/P7′ Android UI 复用 + VPN 业务驱动" for the
// full design rationale (catch-all routing + lax detection + reasoning
// behind dropping the original CIDR/domain/allowedPackages knobs).

package frpcd

import (
	"strings"

	"github.com/teacat99/FrpDeck/internal/model"
)

// TunnelRequiresSystemRoute reports whether a tunnel asks the host
// platform for device-level VPN routing.
//
// Decision rule (lax on purpose):
//
//	role == "visitor" AND
//	    (plugin == "socks5"  // strict, frp v0.50+ canonical form
//	     OR name contains "socks"  // tolerates ad-hoc names
//	    )
//
// The name heuristic exists because frpc historically allowed running a
// stand-alone socks5 server inline without `plugin = socks5`, and users
// routinely name those tunnels `socks-home`, `Socks5`, etc. False
// positives are recoverable: tun2socks fails to dial the supposed
// SOCKS5 endpoint and the Android shell shows a toast. False negatives
// (a real SOCKS5 visitor that nobody flags) leave traffic unrouted with
// no signal — much worse.
func TunnelRequiresSystemRoute(t *model.Tunnel) bool {
	if t == nil {
		return false
	}
	if !strings.EqualFold(t.Role, "visitor") {
		return false
	}
	if strings.EqualFold(t.Plugin, "socks5") {
		return true
	}
	if strings.Contains(strings.ToLower(t.Name), "socks") {
		return true
	}
	return false
}
