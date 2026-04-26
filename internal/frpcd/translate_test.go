package frpcd

import (
	"reflect"
	"testing"

	v1 "github.com/fatedier/frp/pkg/config/v1"

	"github.com/teacat99/FrpDeck/internal/model"
)

func TestBuildProxy_Tcp(t *testing.T) {
	tn := &model.Tunnel{
		ID: 1, Name: "web", Type: "tcp",
		LocalIP: "127.0.0.1", LocalPort: 8080, RemotePort: 12080,
		Encryption: true, Compression: true,
	}
	c, err := BuildProxy(tn)
	if err != nil {
		t.Fatalf("BuildProxy: %v", err)
	}
	tcp, ok := c.(*v1.TCPProxyConfig)
	if !ok {
		t.Fatalf("expected *v1.TCPProxyConfig, got %T", c)
	}
	if tcp.Name != "web" || tcp.Type != "tcp" || tcp.RemotePort != 12080 {
		t.Fatalf("unexpected tcp config: %+v", tcp)
	}
	if !tcp.Transport.UseEncryption || !tcp.Transport.UseCompression {
		t.Fatalf("transport flags not propagated: %+v", tcp.Transport)
	}
	if tcp.LocalIP != "127.0.0.1" || tcp.LocalPort != 8080 {
		t.Fatalf("local backend not set: %+v", tcp.ProxyBackend)
	}
}

func TestBuildProxy_HttpFields(t *testing.T) {
	tn := &model.Tunnel{
		ID: 2, Name: "web-vhost", Type: "http",
		LocalIP: "127.0.0.1", LocalPort: 80,
		CustomDomains: " a.example.com , b.example.com ",
		Subdomain:     "demo",
		Locations:     "/api,/static",
		HTTPUser:      "u", HTTPPassword: "p",
		HostHeaderRewrite: "internal.svc",
	}
	c, err := BuildProxy(tn)
	if err != nil {
		t.Fatalf("BuildProxy: %v", err)
	}
	hp, ok := c.(*v1.HTTPProxyConfig)
	if !ok {
		t.Fatalf("expected *v1.HTTPProxyConfig, got %T", c)
	}
	wantDomains := []string{"a.example.com", "b.example.com"}
	if !reflect.DeepEqual(hp.CustomDomains, wantDomains) {
		t.Fatalf("custom domains: got %v want %v", hp.CustomDomains, wantDomains)
	}
	if hp.SubDomain != "demo" {
		t.Fatalf("subdomain: %q", hp.SubDomain)
	}
	wantLocs := []string{"/api", "/static"}
	if !reflect.DeepEqual(hp.Locations, wantLocs) {
		t.Fatalf("locations: got %v want %v", hp.Locations, wantLocs)
	}
	if hp.HTTPUser != "u" || hp.HTTPPassword != "p" || hp.HostHeaderRewrite != "internal.svc" {
		t.Fatalf("http auth/rewrite mismatch: %+v", hp)
	}
}

func TestBuildProxy_Stcp(t *testing.T) {
	tn := &model.Tunnel{
		ID: 3, Name: "secret", Type: "stcp",
		LocalIP: "127.0.0.1", LocalPort: 22,
		SK:         "topsecret",
		AllowUsers: "alice, bob",
	}
	c, err := BuildProxy(tn)
	if err != nil {
		t.Fatalf("BuildProxy: %v", err)
	}
	st, ok := c.(*v1.STCPProxyConfig)
	if !ok {
		t.Fatalf("expected *v1.STCPProxyConfig, got %T", c)
	}
	if st.Secretkey != "topsecret" {
		t.Fatalf("sk: %q", st.Secretkey)
	}
	if !reflect.DeepEqual(st.AllowUsers, []string{"alice", "bob"}) {
		t.Fatalf("allow_users: %v", st.AllowUsers)
	}
}

func TestBuildVisitor_Stcp(t *testing.T) {
	tn := &model.Tunnel{
		ID: 4, Name: "demo-visitor", Type: "stcp", Role: "visitor",
		LocalIP: "127.0.0.1", LocalPort: 9001,
		SK:         "topsecret",
		ServerName: "secret",
	}
	v, err := BuildVisitor(tn)
	if err != nil {
		t.Fatalf("BuildVisitor: %v", err)
	}
	sv, ok := v.(*v1.STCPVisitorConfig)
	if !ok {
		t.Fatalf("expected *v1.STCPVisitorConfig, got %T", v)
	}
	if sv.SecretKey != "topsecret" || sv.ServerName != "secret" {
		t.Fatalf("visitor secret/server: %+v", sv)
	}
	if sv.BindAddr != "127.0.0.1" || sv.BindPort != 9001 {
		t.Fatalf("visitor bind: %+v", sv)
	}
	if p, _ := BuildProxy(tn); p != nil {
		t.Fatalf("visitor must not yield a proxy configurer, got %T", p)
	}
}

func TestBuildProxy_AllTypes(t *testing.T) {
	types := []string{"tcp", "udp", "http", "https", "tcpmux", "stcp", "xtcp", "sudp"}
	for _, ty := range types {
		tn := &model.Tunnel{ID: 1, Name: "x-" + ty, Type: ty, LocalIP: "127.0.0.1", LocalPort: 1}
		if _, err := BuildProxy(tn); err != nil {
			t.Errorf("type %s: %v", ty, err)
		}
	}
}

func TestEndpointCommon_DefaultsAndAuth(t *testing.T) {
	ep := &model.Endpoint{
		Addr: "frps.example.com", Port: 7000, User: "u",
		Token: "tok", PoolCount: 3, HeartbeatInterval: 15, HeartbeatTimeout: 60,
	}
	c := EndpointCommon(ep)
	if c.ServerAddr != "frps.example.com" || c.ServerPort != 7000 {
		t.Fatalf("server: %+v", c)
	}
	if c.Auth.Token != "tok" || c.Auth.Method != v1.AuthMethodToken {
		t.Fatalf("auth: %+v", c.Auth)
	}
	if c.Transport.PoolCount != 3 || c.Transport.HeartbeatInterval != 15 || c.Transport.HeartbeatTimeout != 60 {
		t.Fatalf("transport: %+v", c.Transport)
	}
	if c.LoginFailExit == nil || *c.LoginFailExit != false {
		t.Fatalf("LoginFailExit must be false to keep retrying")
	}
}
