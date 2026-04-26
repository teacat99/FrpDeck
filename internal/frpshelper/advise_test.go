package frpshelper

import (
	"strings"
	"testing"

	"github.com/teacat99/FrpDeck/internal/model"
)

// helperEP returns a baseline endpoint without auth/TLS so individual
// tests can opt-in to those branches without poking many fields.
func helperEP() *model.Endpoint {
	return &model.Endpoint{
		ID:   1,
		Name: "home-frps",
		Addr: "frps.example.com",
		Port: 7000,
	}
}

// findItem searches for an advice item with a matching field name.
// Helper used heavily by per-rule tests because order is severity-
// based and might shift as new rules are added.
func findItem(items []Item, field string) *Item {
	for i := range items {
		if items[i].Field == field {
			return &items[i]
		}
	}
	return nil
}

func TestAdvise_HTTPRequiresVhostPort(t *testing.T) {
	tn := &model.Tunnel{ID: 10, Type: "http", CustomDomains: "blog.example.com"}
	a := Advise(helperEP(), tn)
	got := findItem(a.Items, "vhostHTTPPort")
	if got == nil {
		t.Fatalf("expected vhostHTTPPort advice; items=%+v", a.Items)
	}
	if got.Severity != SeverityRequired {
		t.Errorf("expected required severity, got %q", got.Severity)
	}
	if got.Value != "80" {
		t.Errorf("expected default value 80, got %q", got.Value)
	}
	if !strings.Contains(a.TomlSnippet, "vhostHTTPPort = 80") {
		t.Errorf("snippet missing vhostHTTPPort: %q", a.TomlSnippet)
	}
}

func TestAdvise_HTTPSubdomainRequiresHost(t *testing.T) {
	tn := &model.Tunnel{Type: "http", Subdomain: "blog"}
	a := Advise(helperEP(), tn)
	if findItem(a.Items, "subdomainHost") == nil {
		t.Errorf("expected subdomainHost advice for subdomain-routed http tunnel")
	}
}

func TestAdvise_HTTPSSubdomainRequiresHost(t *testing.T) {
	tn := &model.Tunnel{Type: "https", Subdomain: "shop"}
	a := Advise(helperEP(), tn)
	if findItem(a.Items, "subdomainHost") == nil {
		t.Errorf("expected subdomainHost advice for subdomain-routed https tunnel")
	}
}

func TestAdvise_HTTPSRequiresVhostHTTPSPort(t *testing.T) {
	tn := &model.Tunnel{Type: "https", CustomDomains: "shop.example.com"}
	a := Advise(helperEP(), tn)
	got := findItem(a.Items, "vhostHTTPSPort")
	if got == nil || got.Severity != SeverityRequired || got.Value != "443" {
		t.Errorf("unexpected vhostHTTPSPort advice: %+v", got)
	}
	if len(a.Caveats) == 0 {
		t.Errorf("expected at least one caveat about TLS termination")
	}
}

func TestAdvise_TCPRecommendsAllowPorts(t *testing.T) {
	tn := &model.Tunnel{Type: "tcp", RemotePort: 22022}
	a := Advise(helperEP(), tn)
	got := findItem(a.Items, "allowPorts")
	if got == nil {
		t.Fatalf("expected allowPorts advice for tcp tunnel with remote_port")
	}
	if got.Severity != SeverityRecommended {
		t.Errorf("expected recommended severity, got %q", got.Severity)
	}
	if got.Value != "22022" {
		t.Errorf("expected echoed remote port, got %q", got.Value)
	}
}

func TestAdvise_STCPCaveatNoFrpsConfig(t *testing.T) {
	tn := &model.Tunnel{Type: "stcp", Role: "server", SK: "shared"}
	a := Advise(helperEP(), tn)
	if len(a.Caveats) == 0 {
		t.Fatalf("expected stcp caveat about sk match, got none")
	}
	for _, it := range a.Items {
		if it.Field == "vhostHTTPPort" || it.Field == "tcpmuxHTTPConnectPort" {
			t.Errorf("stcp should not require any vhost knob: %+v", it)
		}
	}
}

func TestAdvise_XTCPRequiresStunServer(t *testing.T) {
	tn := &model.Tunnel{Type: "xtcp", Role: "server", SK: "shared"}
	a := Advise(helperEP(), tn)
	if findItem(a.Items, "natHoleStunServer") == nil {
		t.Errorf("xtcp must surface natHoleStunServer advice")
	}
}

func TestAdvise_TokenPropagatesToAuthSection(t *testing.T) {
	ep := helperEP()
	ep.Token = "secret"
	a := Advise(ep, &model.Tunnel{Type: "tcp", RemotePort: 1234})
	got := findItem(a.Items, "auth.token")
	if got == nil || got.Severity != SeverityRequired {
		t.Errorf("expected required auth.token advice, got %+v", got)
	}
}

func TestAdvise_TLSPropagatesToTransportSection(t *testing.T) {
	ep := helperEP()
	ep.TLSEnable = true
	a := Advise(ep, &model.Tunnel{Type: "tcp", RemotePort: 1234})
	got := findItem(a.Items, "transport.tls.force")
	if got == nil || got.Severity != SeverityRecommended || got.Value != "true" {
		t.Errorf("expected recommended transport.tls.force=true, got %+v", got)
	}
	if !strings.Contains(a.TomlSnippet, "transport.tls.force = true") {
		t.Errorf("snippet missing TLS toggle: %q", a.TomlSnippet)
	}
}

func TestAdvise_VisitorEmitsInfo(t *testing.T) {
	tn := &model.Tunnel{Type: "stcp", Role: "visitor", ServerName: "peer", SK: "shared"}
	a := Advise(helperEP(), tn)
	hasVisitorInfo := false
	for _, it := range a.Items {
		if it.Severity == SeverityInfo && strings.Contains(it.Title, "Visitor") {
			hasVisitorInfo = true
			break
		}
	}
	if !hasVisitorInfo {
		t.Errorf("expected visitor info item, items=%+v", a.Items)
	}
}

func TestAdvise_ItemsAreSeveritySorted(t *testing.T) {
	ep := helperEP()
	ep.Token = "secret"
	ep.TLSEnable = true
	tn := &model.Tunnel{Type: "http", CustomDomains: "blog.example.com", Subdomain: "blog"}
	a := Advise(ep, tn)

	rank := map[Severity]int{
		SeverityRequired: 0, SeverityRecommended: 1,
		SeverityInfo: 2, SeverityWarn: 3,
	}
	last := -1
	for _, it := range a.Items {
		r := rank[it.Severity]
		if r < last {
			t.Errorf("items not severity-sorted: %+v", a.Items)
			break
		}
		last = r
	}
}

func TestAdvise_SnippetContainsBindPort(t *testing.T) {
	tn := &model.Tunnel{Type: "tcp", RemotePort: 22}
	a := Advise(helperEP(), tn)
	if !strings.Contains(a.TomlSnippet, "bindPort = 7000") {
		t.Errorf("snippet missing bindPort, got %q", a.TomlSnippet)
	}
}

func TestAdvise_NoUnnecessaryItemsForBareTCP(t *testing.T) {
	tn := &model.Tunnel{Type: "tcp"} // no remote_port
	a := Advise(helperEP(), tn)
	if findItem(a.Items, "allowPorts") != nil {
		t.Errorf("should not surface allowPorts when remote_port is unset")
	}
}

func TestAdvise_PluginEmitsInfo(t *testing.T) {
	tn := &model.Tunnel{Type: "tcp", Plugin: "socks5", RemotePort: 1080}
	a := Advise(helperEP(), tn)
	hasPluginInfo := false
	for _, it := range a.Items {
		if it.Severity == SeverityInfo && strings.Contains(it.Title, "socks5") {
			hasPluginInfo = true
			break
		}
	}
	if !hasPluginInfo {
		t.Errorf("expected plugin info item, items=%+v", a.Items)
	}
}
