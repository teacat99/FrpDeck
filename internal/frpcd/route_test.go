package frpcd

import (
	"testing"

	"github.com/teacat99/FrpDeck/internal/model"
)

func TestTunnelRequiresSystemRoute(t *testing.T) {
	cases := []struct {
		name string
		in   *model.Tunnel
		want bool
	}{
		{"nil", nil, false},
		{"empty", &model.Tunnel{}, false},
		{"server-side stcp", &model.Tunnel{Role: "server", Type: "stcp", Plugin: "", Name: "ssh-server"}, false},
		{"visitor without socks", &model.Tunnel{Role: "visitor", Type: "stcp", Plugin: "", Name: "rdp"}, false},
		{"visitor + plugin=socks5 strict", &model.Tunnel{Role: "visitor", Plugin: "socks5", Name: "anything"}, true},
		{"visitor + name 'socks' lax", &model.Tunnel{Role: "visitor", Type: "stcp", Plugin: "", Name: "home-socks"}, true},
		{"visitor + name 'SOCKS' case-insensitive", &model.Tunnel{Role: "visitor", Plugin: "", Name: "SOCKS5-home"}, true},
		{"server-side with socks in name (NOT visitor)", &model.Tunnel{Role: "server", Plugin: "socks5", Name: "socks5-relay"}, false},
		{"role mixed case", &model.Tunnel{Role: "Visitor", Plugin: "socks5", Name: "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TunnelRequiresSystemRoute(tc.in)
			if got != tc.want {
				t.Fatalf("TunnelRequiresSystemRoute(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
