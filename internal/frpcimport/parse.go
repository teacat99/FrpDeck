// Package frpcimport parses an existing frpc configuration file
// (TOML / YAML / JSON; INI is intentionally unsupported) into a
// FrpDeck-shaped Plan that the API layer can preview and commit.
//
// Why this lives in its own package:
//   - The mapping is the inverse of `internal/frpcd/translate.go`. Keeping
//     it adjacent would tempt circular imports (translate.go pulls in
//     model, this package pulls in v1 + DTO concepts), so we keep them
//     separate and let the API layer wire them together.
//   - We deliberately do NOT call `ClientConfig.Complete()`. frp's
//     defaults (e.g. tls.enable=true) would otherwise leak in and we'd
//     surface settings the user never wrote. Importing should be
//     transparent: the operator should recognise their original toml in
//     the preview.
package frpcimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	frpconfig "github.com/fatedier/frp/pkg/config"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/pelletier/go-toml/v2"
	"github.com/samber/lo"
)

// EndpointDraft mirrors the shape of `api.endpointReq`. The API layer
// converts this into a real endpointReq before persisting; we keep the
// types decoupled so the importer compiles on its own.
type EndpointDraft struct {
	Name              string `json:"name"`
	Group             string `json:"group"`
	Addr              string `json:"addr"`
	Port              int    `json:"port"`
	Protocol          string `json:"protocol"`
	User              string `json:"user"`
	Token             string `json:"token"`
	TLSEnable         bool   `json:"tls_enable"`
	PoolCount         int    `json:"pool_count"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
	HeartbeatTimeout  int    `json:"heartbeat_timeout"`
	DriverMode        string `json:"driver_mode"`
	Enabled           bool   `json:"enabled"`
	AutoStart         bool   `json:"auto_start"`
}

// TunnelDraft mirrors `api.tunnelReq` minus the EndpointID (the API
// layer fills that in at commit time once the user picks the target
// endpoint). Warnings list per-tunnel issues so the UI can show them
// inline without a separate join.
type TunnelDraft struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Role              string `json:"role"`
	LocalIP           string `json:"local_ip"`
	LocalPort         int    `json:"local_port"`
	RemotePort        int    `json:"remote_port"`
	CustomDomains     string `json:"custom_domains"`
	Subdomain         string `json:"subdomain"`
	Locations         string `json:"locations"`
	HTTPUser          string `json:"http_user"`
	HTTPPassword      string `json:"http_password"`
	HostHeaderRewrite string `json:"host_header_rewrite"`
	SK                string `json:"sk"`
	AllowUsers        string `json:"allow_users"`
	ServerName        string `json:"server_name"`
	Encryption        bool   `json:"encryption"`
	Compression       bool   `json:"compression"`
	BandwidthLimit    string `json:"bandwidth_limit"`
	Group             string `json:"group"`
	GroupKey          string `json:"group_key"`
	HealthCheckType   string `json:"health_check_type"`
	HealthCheckURL    string `json:"health_check_url"`
	Plugin            string `json:"plugin"`
	PluginConfig      string `json:"plugin_config"`
	Enabled           bool   `json:"enabled"`
	AutoStart         bool   `json:"auto_start"`

	// Warnings are per-tunnel notices: unmapped plugin parameters,
	// fields silently dropped, etc. The UI renders these next to the
	// tunnel row in the preview panel.
	Warnings []string `json:"warnings,omitempty"`

	// Conflict is set by the API layer when a tunnel with the same
	// name already exists under the target endpoint. The UI uses it
	// to default the per-row OnConflict directive to "rename" instead
	// of "error" so a naive "import all" does not abort midway.
	Conflict bool `json:"conflict,omitempty"`
}

// Plan is the result of a successful Parse(): a suggested endpoint
// (the user can keep or replace it with an existing endpoint at commit
// time), the list of tunnel drafts to create, and free-form warnings
// at the file level. Format records the detected source format for the
// preview UI.
type Plan struct {
	Endpoint *EndpointDraft `json:"endpoint"`
	Tunnels  []TunnelDraft  `json:"tunnels"`
	Warnings []string       `json:"warnings,omitempty"`
	Format   string         `json:"format"`
}

// Parse reads a frpc configuration buffer and returns a Plan. The
// optional name (typically the original filename) seeds the suggested
// endpoint Name. Only TOML / YAML / JSON are supported — frp dropped
// INI in v0.52.0.
func Parse(data []byte, name string) (*Plan, error) {
	if len(data) == 0 {
		return nil, errors.New("empty config")
	}
	format := detectFormat(data, name)
	if format == "ini" {
		return nil, errors.New("legacy ini format is not supported; please convert to toml/yaml/json first")
	}

	var cfg v1.ClientConfig
	if err := frpconfig.LoadConfigure(data, &cfg, false, format); err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	plan := &Plan{Format: format}
	plan.Endpoint = mapEndpoint(&cfg, name, &plan.Warnings)
	plan.Tunnels = make([]TunnelDraft, 0, len(cfg.Proxies)+len(cfg.Visitors))
	for i := range cfg.Proxies {
		if d, warns := mapProxy(cfg.Proxies[i].ProxyConfigurer); d != nil {
			d.Warnings = warns
			plan.Tunnels = append(plan.Tunnels, *d)
		}
	}
	for i := range cfg.Visitors {
		if d, warns := mapVisitor(cfg.Visitors[i].VisitorConfigurer); d != nil {
			d.Warnings = warns
			plan.Tunnels = append(plan.Tunnels, *d)
		}
	}
	return plan, nil
}

// detectFormat sniffs the buffer to pick the right hint for
// LoadConfigure. The hint matters only for better error messages
// (LoadConfigure will autodetect on its own); we still want a
// deterministic value to report back to the UI in Plan.Format.
func detectFormat(data []byte, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".ini", ".conf":
		return "ini"
	}
	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	switch {
	case strings.HasPrefix(trimmed, "{"):
		return "json"
	case strings.HasPrefix(trimmed, "["), strings.HasPrefix(trimmed, "#"):
		return "toml"
	default:
		return "yaml"
	}
}

// mapEndpoint translates ClientCommonConfig into an EndpointDraft.
// We never call Complete() on the parsed config (see package doc), so
// any zero / nil here means "the user did not write that field" and we
// should leave the corresponding draft field at its zero value too.
func mapEndpoint(cfg *v1.ClientConfig, name string, warnings *[]string) *EndpointDraft {
	d := &EndpointDraft{
		Name:       suggestEndpointName(name),
		Addr:       strings.TrimSpace(cfg.ServerAddr),
		Port:       cfg.ServerPort,
		Protocol:   strings.TrimSpace(cfg.Transport.Protocol),
		User:       strings.TrimSpace(cfg.User),
		Token:      strings.TrimSpace(cfg.Auth.Token),
		PoolCount:  cfg.Transport.PoolCount,
		Enabled:    true,
		AutoStart:  true,
		DriverMode: "embedded",
	}
	if cfg.Transport.HeartbeatInterval > 0 {
		d.HeartbeatInterval = int(cfg.Transport.HeartbeatInterval)
	}
	if cfg.Transport.HeartbeatTimeout > 0 {
		d.HeartbeatTimeout = int(cfg.Transport.HeartbeatTimeout)
	}
	if cfg.Transport.TLS.Enable != nil {
		d.TLSEnable = lo.FromPtr(cfg.Transport.TLS.Enable)
	} else {
		// frp's default since v0.50.0 is true. We honour that to keep
		// imports compatible with frp's runtime behaviour.
		d.TLSEnable = true
	}

	// Surface settings we don't import as warnings so the user knows to
	// re-apply them manually if they care.
	method := strings.TrimSpace(string(cfg.Auth.Method))
	if method != "" && method != string(v1.AuthMethodToken) {
		*warnings = append(*warnings, fmt.Sprintf("auth.method=%q is not supported (only token); configure manually if needed", method))
	}
	if len(cfg.Auth.AdditionalScopes) > 0 {
		*warnings = append(*warnings, "auth.additionalScopes is not imported; FrpDeck only supports the token method")
	}
	if cfg.Transport.TLS.CertFile != "" || cfg.Transport.TLS.KeyFile != "" || cfg.Transport.TLS.TrustedCaFile != "" || cfg.Transport.TLS.ServerName != "" {
		*warnings = append(*warnings, "transport.tls.{certFile,keyFile,trustedCaFile,serverName} is not imported; configure mTLS in the endpoint advanced fields manually")
	}
	if len(cfg.IncludeConfigFiles) > 0 {
		*warnings = append(*warnings, fmt.Sprintf("includes is not imported; please add the %d included file(s) separately", len(cfg.IncludeConfigFiles)))
	}
	if cfg.WebServer.Port > 0 {
		*warnings = append(*warnings, "webServer.* is intentionally not imported; FrpDeck does not expose the frpc admin port")
	}
	if cfg.NatHoleSTUNServer != "" && cfg.NatHoleSTUNServer != "stun.easyvoip.com:3478" {
		*warnings = append(*warnings, fmt.Sprintf("natHoleStunServer=%q is not imported per-endpoint; will use the FrpDeck-wide default", cfg.NatHoleSTUNServer))
	}
	if len(cfg.Start) > 0 {
		*warnings = append(*warnings, "start[] is not imported; use per-tunnel Enabled instead")
	}
	if len(cfg.Metadatas) > 0 {
		*warnings = append(*warnings, "client metadatas is not imported")
	}
	if len(cfg.FeatureGates) > 0 {
		*warnings = append(*warnings, "featureGates is not imported")
	}
	return d
}

// suggestEndpointName picks a friendly name for the imported endpoint.
// The frpc.toml itself never contains one, so we fall back to the
// uploaded filename without extension; if even that is missing we use a
// stable placeholder the user can rename in the preview UI.
func suggestEndpointName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" || base == "." {
		return "imported"
	}
	return base
}

// mapProxy converts a single ProxyConfigurer into a TunnelDraft. The
// switch mirrors translate.go's BuildProxy in reverse. Unknown plugin
// options are dumped as a TOML fragment into PluginConfig so the user
// can edit them in the preview before committing.
func mapProxy(p v1.ProxyConfigurer) (*TunnelDraft, []string) {
	if p == nil {
		return nil, nil
	}
	base := p.GetBaseConfig()
	d := &TunnelDraft{
		Name:           strings.TrimSpace(base.Name),
		Type:           strings.ToLower(base.Type),
		Role:           "server",
		LocalIP:        strings.TrimSpace(base.LocalIP),
		LocalPort:      base.LocalPort,
		Encryption:     base.Transport.UseEncryption,
		Compression:    base.Transport.UseCompression,
		BandwidthLimit: base.Transport.BandwidthLimit.String(),
		Group:          base.LoadBalancer.Group,
		GroupKey:       base.LoadBalancer.GroupKey,
		Enabled:        true,
		AutoStart:      true,
	}
	if d.LocalIP == "" {
		d.LocalIP = "127.0.0.1"
	}
	if base.HealthCheck.Type != "" {
		d.HealthCheckType = base.HealthCheck.Type
		d.HealthCheckURL = base.HealthCheck.Path
	}
	var warns []string
	if base.Plugin.Type != "" {
		d.Plugin = base.Plugin.Type
		d.PluginConfig = pluginToTOML(base.Plugin, &warns)
		// When a plugin is in play, frpc ignores LocalIP/LocalPort.
		// Surfacing this avoids confusion in the preview UI.
		warns = append(warns, "plugin overrides local_ip/local_port; FrpDeck stores both but the driver will skip them at runtime")
	}

	switch c := p.(type) {
	case *v1.TCPProxyConfig:
		d.RemotePort = c.RemotePort
	case *v1.UDPProxyConfig:
		d.RemotePort = c.RemotePort
	case *v1.HTTPProxyConfig:
		d.CustomDomains = strings.Join(c.CustomDomains, ",")
		d.Subdomain = c.SubDomain
		d.Locations = strings.Join(c.Locations, ",")
		d.HTTPUser = c.HTTPUser
		d.HTTPPassword = c.HTTPPassword
		d.HostHeaderRewrite = c.HostHeaderRewrite
		if len(c.RequestHeaders.Set) > 0 || len(c.ResponseHeaders.Set) > 0 || len(c.RouteByHTTPUser) > 0 {
			warns = append(warns, "advanced HTTP fields (requestHeaders/responseHeaders/routeByHTTPUser) are not imported; reconfigure manually if needed")
		}
	case *v1.HTTPSProxyConfig:
		d.CustomDomains = strings.Join(c.CustomDomains, ",")
		d.Subdomain = c.SubDomain
	case *v1.TCPMuxProxyConfig:
		d.CustomDomains = strings.Join(c.CustomDomains, ",")
		d.Subdomain = c.SubDomain
		d.HTTPUser = c.HTTPUser
		d.HTTPPassword = c.HTTPPassword
		if c.Multiplexer != "" && c.Multiplexer != string(v1.TCPMultiplexerHTTPConnect) {
			warns = append(warns, fmt.Sprintf("multiplexer=%q is not supported (only httpconnect); reconfigure manually", c.Multiplexer))
		}
	case *v1.STCPProxyConfig:
		d.SK = c.Secretkey
		d.AllowUsers = strings.Join(c.AllowUsers, ",")
	case *v1.XTCPProxyConfig:
		d.SK = c.Secretkey
		d.AllowUsers = strings.Join(c.AllowUsers, ",")
	case *v1.SUDPProxyConfig:
		d.SK = c.Secretkey
		d.AllowUsers = strings.Join(c.AllowUsers, ",")
	default:
		warns = append(warns, fmt.Sprintf("unsupported proxy type %q; this entry will not be imported", base.Type))
		return nil, warns
	}
	return d, warns
}

// mapVisitor converts a VisitorConfigurer into a TunnelDraft. Visitors
// always carry the role="visitor" tag so the API layer's validate()
// picks the visitor branch (sk + server_name required).
func mapVisitor(v v1.VisitorConfigurer) (*TunnelDraft, []string) {
	if v == nil {
		return nil, nil
	}
	base := v.GetBaseConfig()
	d := &TunnelDraft{
		Name:        strings.TrimSpace(base.Name),
		Type:        strings.ToLower(base.Type),
		Role:        "visitor",
		LocalIP:     strings.TrimSpace(base.BindAddr),
		LocalPort:   base.BindPort,
		ServerName:  base.ServerName,
		SK:          base.SecretKey,
		Encryption:  base.Transport.UseEncryption,
		Compression: base.Transport.UseCompression,
		Enabled:     true,
		AutoStart:   true,
	}
	if d.LocalIP == "" {
		d.LocalIP = "127.0.0.1"
	}
	var warns []string
	switch c := v.(type) {
	case *v1.STCPVisitorConfig:
		_ = c
	case *v1.SUDPVisitorConfig:
		_ = c
	case *v1.XTCPVisitorConfig:
		if c.Protocol != "" || c.KeepTunnelOpen {
			warns = append(warns, "xtcp visitor advanced fields (protocol/keepTunnelOpen/maxRetriesAnHour/...) are not imported; reconfigure manually if needed")
		}
	default:
		warns = append(warns, fmt.Sprintf("unsupported visitor type %q; this entry will not be imported", base.Type))
		return nil, warns
	}
	return d, warns
}

// pluginToTOML serialises the typed plugin options as a TOML fragment.
// We marshal to JSON first to honour frp's `json:"..."` tag names
// (pelletier/go-toml/v2 would otherwise use Go field names because the
// frp v1 plugin structs do not declare `toml:"..."` tags), then
// round-trip the resulting map through TOML. The textarea-driven
// plugin_config in the UI is then a faithful copy the user can tweak
// before commit.
func pluginToTOML(opts v1.TypedClientPluginOptions, warns *[]string) string {
	if opts.ClientPluginOptions == nil {
		return ""
	}
	jsonBytes, err := json.Marshal(opts.ClientPluginOptions)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("failed to serialise plugin %q options: %v", opts.Type, err))
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(jsonBytes, &asMap); err != nil {
		*warns = append(*warns, fmt.Sprintf("failed to decode plugin %q options: %v", opts.Type, err))
		return ""
	}
	// Drop the redundant `type` key — the parent already stores the
	// plugin name in TunnelDraft.Plugin and the textarea is meant to
	// hold the plugin's *parameters*, not its identity.
	delete(asMap, "type")
	if len(asMap) == 0 {
		return ""
	}
	raw, err := toml.Marshal(asMap)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("failed to serialise plugin %q options: %v", opts.Type, err))
		return ""
	}
	return sortTOMLKeys(strings.TrimSpace(string(raw)))
}

// sortTOMLKeys sorts the top-level k=v lines in a TOML fragment so the
// imported plugin_config stays stable across runs (eases diffs in tests
// and snapshots). Sub-tables ([table.foo] blocks) are preserved verbatim
// after the sorted top-level pairs.
func sortTOMLKeys(s string) string {
	lines := strings.Split(s, "\n")
	var pairs, rest []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "[") {
			rest = append(rest, ln)
			continue
		}
		// A continuation of a previous block should not be re-sorted; if
		// rest is non-empty we are already past the "leading scalars"
		// section.
		if len(rest) > 0 {
			rest = append(rest, ln)
			continue
		}
		pairs = append(pairs, ln)
	}
	sort.Strings(pairs)
	combined := append(pairs, rest...)
	return strings.Join(combined, "\n")
}
