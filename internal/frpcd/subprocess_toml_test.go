package frpcd

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/teacat99/FrpDeck/internal/model"
)

// asMap parses a rendered TOML doc back to a map for assertion. We round-
// trip through go-toml so the test covers both the serialiser AND that
// the result is a syntactically valid frpc-style TOML file.
func asMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := toml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("toml unmarshal failed: %v\nRAW:\n%s", err, raw)
	}
	return m
}

func TestBuildSubprocessTOML_BasicTCP(t *testing.T) {
	ep := &model.Endpoint{
		ID:        1,
		Addr:      "frps.example.com",
		Port:      7000,
		Token:     "supersecret",
		User:      "alice",
		Protocol:  "kcp",
		TLSEnable: true,
	}
	tn := &model.Tunnel{
		ID:         11,
		EndpointID: 1,
		Name:       "ssh",
		Type:       "tcp",
		LocalIP:    "127.0.0.1",
		LocalPort:  22,
		RemotePort: 7022,
		Encryption: true,
		Compression: true,
		Enabled:    true,
	}
	raw, err := BuildSubprocessTOML(SubprocessConfig{
		Endpoint:      ep,
		Tunnels:       []*model.Tunnel{tn},
		AdminAddr:     "127.0.0.1",
		AdminPort:     7400,
		AdminUser:     "admin",
		AdminPassword: "p4ss",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	m := asMap(t, raw)
	if got, want := m["serverAddr"], "frps.example.com"; got != want {
		t.Errorf("serverAddr=%v want %v", got, want)
	}
	if got, want := m["serverPort"], int64(7000); got != want {
		t.Errorf("serverPort=%v want %v", got, want)
	}
	if got, want := m["user"], "alice"; got != want {
		t.Errorf("user=%v want %v", got, want)
	}
	auth := m["auth"].(map[string]any)
	if auth["method"] != "token" || auth["token"] != "supersecret" {
		t.Errorf("auth=%v", auth)
	}
	transport := m["transport"].(map[string]any)
	if transport["protocol"] != "kcp" {
		t.Errorf("transport.protocol=%v", transport["protocol"])
	}
	tls := transport["tls"].(map[string]any)
	if tls["enable"] != true {
		t.Errorf("transport.tls.enable=%v", tls["enable"])
	}
	web := m["webServer"].(map[string]any)
	if web["port"] != int64(7400) || web["user"] != "admin" {
		t.Errorf("webServer=%v", web)
	}
	proxies := m["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("proxies len=%d", len(proxies))
	}
	p := proxies[0].(map[string]any)
	if p["name"] != "ssh" || p["type"] != "tcp" {
		t.Errorf("proxy[0]=%v", p)
	}
	if p["localPort"] != int64(22) || p["remotePort"] != int64(7022) {
		t.Errorf("proxy ports=%v / %v", p["localPort"], p["remotePort"])
	}
	pt := p["transport"].(map[string]any)
	if pt["useEncryption"] != true || pt["useCompression"] != true {
		t.Errorf("proxy.transport=%v", pt)
	}
}

func TestBuildSubprocessTOML_HTTPSubdomainAndPlugin(t *testing.T) {
	ep := &model.Endpoint{ID: 2, Addr: "frps.example.com", Port: 7000}
	tn := &model.Tunnel{
		ID:            21,
		Name:          "site",
		Type:          "http",
		LocalIP:       "127.0.0.1",
		LocalPort:     8080,
		Subdomain:     "blog",
		HTTPUser:      "u",
		HTTPPassword:  "p",
		Plugin:        "static_file",
		PluginConfig:  "local_path = \"/srv/www\"\nstrip_prefix = \"static\"",
		Enabled:       true,
	}
	raw, err := BuildSubprocessTOML(SubprocessConfig{Endpoint: ep, Tunnels: []*model.Tunnel{tn}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := asMap(t, raw)
	proxy := m["proxies"].([]any)[0].(map[string]any)
	if proxy["subdomain"] != "blog" {
		t.Errorf("subdomain=%v", proxy["subdomain"])
	}
	if proxy["httpUser"] != "u" || proxy["httpPassword"] != "p" {
		t.Errorf("http auth=%v / %v", proxy["httpUser"], proxy["httpPassword"])
	}
	plugin := proxy["plugin"].(map[string]any)
	if plugin["type"] != "static_file" {
		t.Errorf("plugin.type=%v", plugin["type"])
	}
	if plugin["local_path"] != "/srv/www" || plugin["strip_prefix"] != "static" {
		t.Errorf("plugin fields=%v", plugin)
	}
}

func TestBuildSubprocessTOML_StcpVisitor(t *testing.T) {
	ep := &model.Endpoint{ID: 3, Addr: "frps.example.com", Port: 7000}
	tn := &model.Tunnel{
		ID:         31,
		Name:       "secret",
		Type:       "stcp",
		Role:       "visitor",
		ServerName: "secret",
		ServerUser: "alice",
		SK:         "shared",
		LocalIP:    "127.0.0.1",
		LocalPort:  9000,
		Enabled:    true,
	}
	raw, err := BuildSubprocessTOML(SubprocessConfig{Endpoint: ep, Tunnels: []*model.Tunnel{tn}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := asMap(t, raw)
	if _, ok := m["proxies"]; ok {
		t.Errorf("visitors should not appear under proxies")
	}
	visitors := m["visitors"].([]any)
	if len(visitors) != 1 {
		t.Fatalf("visitors len=%d", len(visitors))
	}
	v := visitors[0].(map[string]any)
	if v["name"] != "secret" || v["type"] != "stcp" || v["role"] != "visitor" {
		t.Errorf("visitor base=%v", v)
	}
	if v["serverName"] != "secret" || v["serverUser"] != "alice" || v["secretKey"] != "shared" {
		t.Errorf("visitor secret=%v", v)
	}
	if v["bindAddr"] != "127.0.0.1" || v["bindPort"] != int64(9000) {
		t.Errorf("visitor bind=%v / %v", v["bindAddr"], v["bindPort"])
	}
}

func TestBuildSubprocessTOML_DisabledTunnelSkipped(t *testing.T) {
	ep := &model.Endpoint{ID: 4, Addr: "frps.example.com", Port: 7000}
	on := &model.Tunnel{ID: 41, Name: "on", Type: "tcp", LocalPort: 80, RemotePort: 8080, Enabled: true}
	off := &model.Tunnel{ID: 42, Name: "off", Type: "tcp", LocalPort: 81, RemotePort: 8081, Enabled: false}
	raw, err := BuildSubprocessTOML(SubprocessConfig{Endpoint: ep, Tunnels: []*model.Tunnel{on, off}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := asMap(t, raw)
	proxies := m["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("proxies len=%d (disabled should be skipped)", len(proxies))
	}
	if proxies[0].(map[string]any)["name"] != "on" {
		t.Errorf("wrong proxy survived: %v", proxies[0])
	}
}

func TestBuildSubprocessTOML_AdminBlockOmittedWhenZero(t *testing.T) {
	ep := &model.Endpoint{ID: 5, Addr: "frps.example.com", Port: 7000}
	raw, err := BuildSubprocessTOML(SubprocessConfig{Endpoint: ep})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := asMap(t, raw)
	if _, ok := m["webServer"]; ok {
		t.Errorf("webServer should be omitted when AdminPort=0")
	}
}

func TestBuildPluginTable_RejectsTypeOverride(t *testing.T) {
	tbl, err := buildPluginTable("static_file", `type = "user_override"
local_path = "/srv"`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got, want := tbl["type"], "static_file"; got != want {
		t.Errorf("type=%v want %v (user override should be ignored)", got, want)
	}
	if tbl["local_path"] != "/srv" {
		t.Errorf("local_path lost: %v", tbl)
	}
}

func TestBuildPluginTable_InvalidToml(t *testing.T) {
	_, err := buildPluginTable("static_file", "= broken =")
	if err == nil || !strings.Contains(err.Error(), "invalid plugin_config") {
		t.Errorf("expected invalid plugin_config err, got %v", err)
	}
}
