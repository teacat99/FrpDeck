// Package api — VPN-routing detection (P6′/P7′).
//
// Android shells use this to decide whether the device-level VpnService
// should be brought up alongside frpc. Other deployment shapes (desktop /
// Docker / headless service) consume the same endpoint but ignore the
// result — there is no system-level routing on those targets.
//
// The detection rule itself lives in `internal/frpcd.TunnelRequiresSystemRoute`
// so the gomobile bridge can share it; this file only owns the HTTP
// shape. See plan.md §11 row "P6′/P7′ Android UI 复用 + VPN 业务驱动"
// for the design rationale.

package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/teacat99/FrpDeck/internal/frpcd"
)

// vpnRequiredTunnel is the JSON shape returned by the system VPN-required
// endpoint. We deliberately strip secrets (SK, HTTPPassword) and only
// expose what the Android shell needs to wire tun2socks: the tunnel
// identity plus the SOCKS5 hint string the shell will dial.
type vpnRequiredTunnel struct {
	ID         uint   `json:"id"`
	EndpointID uint   `json:"endpoint_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	Plugin     string `json:"plugin"`
	// Socks5URL is a convenience field of the form `socks5://<ip>:<port>`
	// derived from LocalIP+LocalPort. Empty when LocalPort is 0 (the user
	// hasn't bound a port yet — likely still configuring the tunnel).
	Socks5URL string `json:"socks5_url,omitempty"`
}

// vpnRequiredResponse is what `GET /api/system/vpn/required` returns. The
// boolean is the tl;dr that the Android shell uses to short-circuit;
// the array gives full context for the eventual UI badge / log line.
type vpnRequiredResponse struct {
	Required bool                `json:"required"`
	Tunnels  []vpnRequiredTunnel `json:"tunnels"`
}

// handleVPNRequired enumerates active+pending tunnels and reports which
// of them want the device-level VPN. Only `status=active` rows actually
// have live frpc proxies; `pending` rows are returned too so the Android
// shell can prompt the user for VpnService permission *before* the
// engine flips them to active (avoiding a midstream system dialog).
//
// The endpoint sits under the authenticated `/api` tree because tunnel
// names + ports are not public information.
func (s *Server) handleVPNRequired(c *gin.Context) {
	all, err := s.store.ListActiveTunnels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]vpnRequiredTunnel, 0, len(all))
	for i := range all {
		t := &all[i]
		if !frpcd.TunnelRequiresSystemRoute(t) {
			continue
		}
		row := vpnRequiredTunnel{
			ID:         t.ID,
			EndpointID: t.EndpointID,
			Name:       t.Name,
			Type:       t.Type,
			Status:     t.Status,
			LocalIP:    t.LocalIP,
			LocalPort:  t.LocalPort,
			Plugin:     t.Plugin,
		}
		if t.LocalPort > 0 {
			ip := t.LocalIP
			if ip == "" {
				ip = "127.0.0.1"
			}
			row.Socks5URL = "socks5://" + ip + ":" + strconv.Itoa(t.LocalPort)
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, vpnRequiredResponse{
		Required: len(out) > 0,
		Tunnels:  out,
	})
}
