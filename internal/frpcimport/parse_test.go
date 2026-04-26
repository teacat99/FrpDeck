package frpcimport

import (
	"strings"
	"testing"
)

// findTunnel returns the first tunnel with the given name or t.Fatal()s.
// Test helpers like this keep the actual table-driven tests below short
// and focused on the assertion at hand.
func findTunnel(t *testing.T, plan *Plan, name string) *TunnelDraft {
	t.Helper()
	for i := range plan.Tunnels {
		if plan.Tunnels[i].Name == name {
			return &plan.Tunnels[i]
		}
	}
	t.Fatalf("tunnel %q not found in plan (got %d tunnels)", name, len(plan.Tunnels))
	return nil
}

// TestParseTOML_FullCoverage feeds a multi-section TOML representative
// of a realistic frpc.toml: token + tls + every proxy type + an stcp
// visitor + one plugin. Asserts both the endpoint draft and each
// tunnel draft are mapped with the right field semantics.
func TestParseTOML_FullCoverage(t *testing.T) {
	src := `
serverAddr = "frp.example.com"
serverPort = 7000
user = "alice"

[auth]
method = "token"
token = "S3cret"

[transport]
protocol = "tcp"
poolCount = 5
heartbeatInterval = 20
heartbeatTimeout = 60

[transport.tls]
enable = false

[[proxies]]
name = "web"
type = "http"
localIP = "127.0.0.1"
localPort = 8080
customDomains = ["a.example.com", "b.example.com"]
locations = ["/api", "/static"]
httpUser = "u"
httpPassword = "p"
hostHeaderRewrite = "internal.example"

[[proxies]]
name = "ssh"
type = "tcp"
localPort = 22
remotePort = 6022
transport.useEncryption = true
transport.useCompression = true

[[proxies]]
name = "rdp"
type = "tcpmux"
localPort = 3389
customDomains = ["rdp.example.com"]
httpUser = "u"
httpPassword = "p"
multiplexer = "httpconnect"

[[proxies]]
name = "secret-tcp"
type = "stcp"
localPort = 5432
secretKey = "shh"
allowUsers = ["alice", "bob"]

[[proxies]]
name = "p2p-tcp"
type = "xtcp"
localPort = 6443
secretKey = "shh"

[[proxies]]
name = "udp-svc"
type = "udp"
localPort = 5353
remotePort = 6053

[[proxies]]
name = "secret-udp"
type = "sudp"
localPort = 53
secretKey = "shh"

[[proxies]]
name = "static"
type = "tcp"
remotePort = 6080
[proxies.plugin]
type = "static_file"
localPath = "/srv/site"
stripPrefix = "static"

[[visitors]]
name = "secret-tcp-visitor"
type = "stcp"
serverName = "secret-tcp"
secretKey = "shh"
bindAddr = "127.0.0.1"
bindPort = 9000
`
	plan, err := Parse([]byte(src), "frpc.toml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if plan.Format != "toml" {
		t.Errorf("Format=%q want toml", plan.Format)
	}
	ep := plan.Endpoint
	if ep == nil {
		t.Fatal("Endpoint nil")
	}
	if ep.Addr != "frp.example.com" || ep.Port != 7000 {
		t.Errorf("addr/port: %q/%d", ep.Addr, ep.Port)
	}
	if ep.User != "alice" || ep.Token != "S3cret" {
		t.Errorf("user/token: %q/%q", ep.User, ep.Token)
	}
	if ep.PoolCount != 5 || ep.HeartbeatInterval != 20 || ep.HeartbeatTimeout != 60 {
		t.Errorf("pool/heartbeat: %+v", ep)
	}
	if ep.TLSEnable {
		t.Error("TLSEnable: explicit false should win")
	}
	if !ep.Enabled || !ep.AutoStart {
		t.Errorf("default flags: enabled=%v auto_start=%v", ep.Enabled, ep.AutoStart)
	}

	if got := len(plan.Tunnels); got != 9 {
		t.Fatalf("tunnels: got %d want 9", got)
	}

	web := findTunnel(t, plan, "web")
	if web.Type != "http" || web.LocalPort != 8080 || web.HTTPUser != "u" || web.HTTPPassword != "p" {
		t.Errorf("http: %+v", web)
	}
	if web.CustomDomains != "a.example.com,b.example.com" {
		t.Errorf("custom_domains: %q", web.CustomDomains)
	}
	if web.Locations != "/api,/static" {
		t.Errorf("locations: %q", web.Locations)
	}
	if web.HostHeaderRewrite != "internal.example" {
		t.Errorf("host_header_rewrite: %q", web.HostHeaderRewrite)
	}

	ssh := findTunnel(t, plan, "ssh")
	if ssh.Type != "tcp" || ssh.LocalPort != 22 || ssh.RemotePort != 6022 {
		t.Errorf("tcp: %+v", ssh)
	}
	if !ssh.Encryption || !ssh.Compression {
		t.Errorf("transport flags: %+v", ssh)
	}

	rdp := findTunnel(t, plan, "rdp")
	if rdp.Type != "tcpmux" || rdp.CustomDomains != "rdp.example.com" {
		t.Errorf("tcpmux: %+v", rdp)
	}

	stcp := findTunnel(t, plan, "secret-tcp")
	if stcp.Type != "stcp" || stcp.SK != "shh" || stcp.AllowUsers != "alice,bob" {
		t.Errorf("stcp: %+v", stcp)
	}

	xtcp := findTunnel(t, plan, "p2p-tcp")
	if xtcp.Type != "xtcp" || xtcp.SK != "shh" {
		t.Errorf("xtcp: %+v", xtcp)
	}

	udp := findTunnel(t, plan, "udp-svc")
	if udp.Type != "udp" || udp.RemotePort != 6053 {
		t.Errorf("udp: %+v", udp)
	}

	sudp := findTunnel(t, plan, "secret-udp")
	if sudp.Type != "sudp" || sudp.SK != "shh" {
		t.Errorf("sudp: %+v", sudp)
	}

	stat := findTunnel(t, plan, "static")
	if stat.Plugin != "static_file" {
		t.Errorf("plugin name: %q", stat.Plugin)
	}
	if !strings.Contains(stat.PluginConfig, "localPath") || !strings.Contains(stat.PluginConfig, "/srv/site") {
		t.Errorf("plugin config TOML missing localPath: %q", stat.PluginConfig)
	}
	// plugin warning carries the runtime caveat
	foundCaveat := false
	for _, w := range stat.Warnings {
		if strings.Contains(w, "plugin overrides local_ip/local_port") {
			foundCaveat = true
		}
	}
	if !foundCaveat {
		t.Errorf("plugin warnings missing override caveat: %v", stat.Warnings)
	}

	visitor := findTunnel(t, plan, "secret-tcp-visitor")
	if visitor.Role != "visitor" || visitor.Type != "stcp" {
		t.Errorf("visitor role/type: %+v", visitor)
	}
	if visitor.SK != "shh" || visitor.ServerName != "secret-tcp" {
		t.Errorf("visitor sk/server_name: %+v", visitor)
	}
	if visitor.LocalPort != 9000 || visitor.LocalIP != "127.0.0.1" {
		t.Errorf("visitor bindAddr/bindPort: %+v", visitor)
	}
}

// TestParseTOML_TLSDefaults verifies that an unset transport.tls.enable
// honours frp's v0.50+ default of true. Importing must not silently
// disable TLS just because the user omitted the field.
func TestParseTOML_TLSDefaults(t *testing.T) {
	src := `
serverAddr = "frp.example.com"
serverPort = 7000

[[proxies]]
name = "n"
type = "tcp"
localPort = 1
remotePort = 1
`
	plan, err := Parse([]byte(src), "frpc.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Endpoint.TLSEnable {
		t.Errorf("TLSEnable should default to true (frp >= 0.50)")
	}
}

// TestParseTOML_Warnings asserts we surface the right top-level
// warnings for fields we deliberately drop. Without these, an importer
// would produce a silent "looks fine" preview that diverges from the
// user's actual frpc.toml.
func TestParseTOML_Warnings(t *testing.T) {
	src := `
serverAddr = "frp.example.com"
serverPort = 7000
includes = ["./extra.toml"]
start = ["a", "b"]

[auth]
method = "oidc"

[webServer]
addr = "0.0.0.0"
port = 7400

[[proxies]]
name = "n"
type = "tcp"
localPort = 1
remotePort = 1
`
	plan, err := Parse([]byte(src), "frpc.toml")
	if err != nil {
		t.Fatal(err)
	}
	hits := map[string]bool{}
	for _, w := range plan.Warnings {
		switch {
		case strings.Contains(w, "auth.method"):
			hits["method"] = true
		case strings.Contains(w, "includes is not imported"):
			hits["includes"] = true
		case strings.Contains(w, "webServer.* is intentionally not imported"):
			hits["webserver"] = true
		case strings.Contains(w, "start[]"):
			hits["start"] = true
		}
	}
	for k := range map[string]struct{}{"method": {}, "includes": {}, "webserver": {}, "start": {}} {
		if !hits[k] {
			t.Errorf("missing warning %s in %v", k, plan.Warnings)
		}
	}
}

// TestParseTOML_INIRejected confirms we send users away with a clear
// message for legacy INI configs (frp dropped support in v0.52.0). We
// detect by extension; the raw parser would also fail.
func TestParseTOML_INIRejected(t *testing.T) {
	_, err := Parse([]byte("[common]\nserver_addr = a\n"), "frpc.ini")
	if err == nil {
		t.Fatal("expected error for ini")
	}
	if !strings.Contains(err.Error(), "ini") {
		t.Errorf("error should mention ini: %v", err)
	}
}

// TestParseTOML_Empty fails fast on empty buffers. The HTTP layer
// relies on this to reject empty uploads with a helpful message rather
// than emitting an empty Plan.
func TestParseTOML_Empty(t *testing.T) {
	if _, err := Parse(nil, "frpc.toml"); err == nil {
		t.Fatal("expected error for empty data")
	}
}

// TestParseYAML_BasicRoundtrip exercises the YAML branch by feeding the
// equivalent of the full TOML (just the bits unique to YAML parsing).
// We do not need to re-test every field — `LoadConfigure` normalises
// across formats — but a smoke test guards against detection regressions.
func TestParseYAML_BasicRoundtrip(t *testing.T) {
	src := `
serverAddr: frp.example.com
serverPort: 7000
auth:
  method: token
  token: yaml-token
proxies:
  - name: y1
    type: tcp
    localPort: 22
    remotePort: 6022
`
	plan, err := Parse([]byte(src), "frpc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Format != "yaml" {
		t.Errorf("Format=%q want yaml", plan.Format)
	}
	if plan.Endpoint.Token != "yaml-token" {
		t.Errorf("token: %q", plan.Endpoint.Token)
	}
	if got := len(plan.Tunnels); got != 1 {
		t.Fatalf("tunnels: got %d want 1", got)
	}
	if plan.Tunnels[0].Name != "y1" || plan.Tunnels[0].RemotePort != 6022 {
		t.Errorf("tunnel: %+v", plan.Tunnels[0])
	}
}

// TestParseTOML_HTTPSAndSubdomain captures the HTTPS+subdomain combo
// the frps helper specifically advises about. This locks in symmetry
// between import and the existing P5-B advisor path.
func TestParseTOML_HTTPSAndSubdomain(t *testing.T) {
	src := `
serverAddr = "frp.example.com"
serverPort = 7000

[[proxies]]
name = "h"
type = "https"
localPort = 8443
subdomain = "app"
`
	plan, err := Parse([]byte(src), "frpc.toml")
	if err != nil {
		t.Fatal(err)
	}
	tn := findTunnel(t, plan, "h")
	if tn.Type != "https" || tn.Subdomain != "app" {
		t.Errorf("https/subdomain: %+v", tn)
	}
	if tn.LocalPort != 8443 {
		t.Errorf("local_port: %d", tn.LocalPort)
	}
}

// TestSuggestEndpointName covers the edge cases the API layer relies on
// to seed endpoint names: dotted filenames, missing extension, blank,
// and legacy INI extension.
func TestSuggestEndpointName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"frpc.toml", "frpc"},
		{"home.frpc.yaml", "home.frpc"},
		{"plain", "plain"},
		{"", "imported"},
		{"  ", "imported"},
		{"/tmp/foo/bar.json", "bar"},
	}
	for _, c := range cases {
		if got := suggestEndpointName(c.in); got != c.want {
			t.Errorf("suggestEndpointName(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
