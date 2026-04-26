// Package frpcd — TOML builder for SubprocessDriver.
//
// The embedded driver hands a typed `*v1.ClientCommonConfig` plus
// `[]ProxyConfigurer` to `client.Service.UpdateConfigSource`; the subprocess
// driver instead has to render the same configuration as a frpc.toml on
// disk so the spawned binary can read it. Building the TOML by hand
// (rather than going through frp's internal encoder, which is not exposed
// as a public API) keeps the output stable across frp upstream churn.
//
// We deliberately produce a map-based TOML tree rather than typed structs
// because frp's `[[proxies]]` array is heterogeneous (TCP / HTTP / STCP /
// ... each with different fields) and `pelletier/go-toml/v2` handles
// `[]map[string]any` flawlessly.

package frpcd

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/teacat99/FrpDeck/internal/model"
)

// SubprocessConfig is the full payload BuildSubprocessTOML serialises into
// a frpc.toml a SubprocessDriver runner spawns the user-supplied frpc
// binary against. AdminAddr/AdminPort/AdminUser/AdminPassword wire frpc's
// embedded webServer so we can hot-reload tunnels without restarting.
type SubprocessConfig struct {
	Endpoint *model.Endpoint
	Tunnels  []*model.Tunnel

	AdminAddr     string
	AdminPort     int
	AdminUser     string
	AdminPassword string

	// LogLevel maps to `log.level`. Empty defaults to "info".
	LogLevel string
}

// BuildSubprocessTOML renders the full frpc.toml content. The byte slice
// is meant to be written verbatim to `<run_dir>/frpc-<endpoint_id>.toml`
// before exec'ing the binary.
func BuildSubprocessTOML(c SubprocessConfig) ([]byte, error) {
	if c.Endpoint == nil {
		return nil, fmt.Errorf("subprocess toml: nil endpoint")
	}
	root := map[string]any{}

	root["serverAddr"] = c.Endpoint.Addr
	root["serverPort"] = c.Endpoint.Port
	if u := strings.TrimSpace(c.Endpoint.User); u != "" {
		root["user"] = u
	}
	if c.LogLevel != "" {
		root["log"] = map[string]any{"level": c.LogLevel}
	}
	if c.Endpoint.Token != "" {
		root["auth"] = map[string]any{
			"method": "token",
			"token":  c.Endpoint.Token,
		}
	}

	transport := map[string]any{}
	if p := strings.TrimSpace(c.Endpoint.Protocol); p != "" {
		transport["protocol"] = p
	}
	if c.Endpoint.PoolCount > 0 {
		transport["poolCount"] = c.Endpoint.PoolCount
	}
	if c.Endpoint.HeartbeatInterval > 0 {
		transport["heartbeatInterval"] = c.Endpoint.HeartbeatInterval
	}
	if c.Endpoint.HeartbeatTimeout > 0 {
		transport["heartbeatTimeout"] = c.Endpoint.HeartbeatTimeout
	}
	tls := map[string]any{}
	if c.Endpoint.TLSEnable {
		tls["enable"] = true
	} else {
		tls["enable"] = false
	}
	transport["tls"] = tls
	root["transport"] = transport

	if c.AdminPort > 0 {
		web := map[string]any{
			"addr": c.AdminAddr,
			"port": c.AdminPort,
		}
		if c.AdminUser != "" {
			web["user"] = c.AdminUser
		}
		if c.AdminPassword != "" {
			web["password"] = c.AdminPassword
		}
		root["webServer"] = web
	}

	proxies := make([]map[string]any, 0, len(c.Tunnels))
	visitors := make([]map[string]any, 0, len(c.Tunnels))
	for _, t := range c.Tunnels {
		if t == nil || !t.Enabled {
			continue
		}
		if IsVisitor(t) {
			v, err := buildVisitorTable(t)
			if err != nil {
				return nil, err
			}
			if v != nil {
				visitors = append(visitors, v)
			}
			continue
		}
		p, err := buildProxyTable(t)
		if err != nil {
			return nil, err
		}
		if p != nil {
			proxies = append(proxies, p)
		}
	}
	if len(proxies) > 0 {
		root["proxies"] = proxies
	}
	if len(visitors) > 0 {
		root["visitors"] = visitors
	}

	return toml.Marshal(root)
}

func buildProxyTable(t *model.Tunnel) (map[string]any, error) {
	if t == nil {
		return nil, nil
	}
	typ := strings.ToLower(strings.TrimSpace(t.Type))
	if typ == "" {
		return nil, fmt.Errorf("tunnel %q: missing type", t.Name)
	}
	tbl := map[string]any{
		"name": tunnelName(t),
		"type": typ,
	}
	if t.LocalIP != "" {
		tbl["localIP"] = t.LocalIP
	}
	if t.LocalPort > 0 {
		tbl["localPort"] = t.LocalPort
	}
	transport := map[string]any{}
	if t.Encryption {
		transport["useEncryption"] = true
	}
	if t.Compression {
		transport["useCompression"] = true
	}
	if bw := strings.TrimSpace(t.BandwidthLimit); bw != "" {
		transport["bandwidthLimit"] = bw
	}
	if len(transport) > 0 {
		tbl["transport"] = transport
	}
	if g := strings.TrimSpace(t.Group); g != "" {
		tbl["loadBalancer"] = map[string]any{
			"group":    g,
			"groupKey": t.GroupKey,
		}
	}
	if hc := strings.TrimSpace(t.HealthCheckType); hc != "" {
		hcCfg := map[string]any{"type": hc}
		if hc == "http" && t.HealthCheckURL != "" {
			hcCfg["path"] = t.HealthCheckURL
		}
		tbl["healthCheck"] = hcCfg
	}

	switch typ {
	case "tcp", "udp":
		if t.RemotePort > 0 {
			tbl["remotePort"] = t.RemotePort
		}
	case "http":
		if doms := splitCSV(t.CustomDomains); len(doms) > 0 {
			tbl["customDomains"] = doms
		}
		if sd := strings.TrimSpace(t.Subdomain); sd != "" {
			tbl["subdomain"] = sd
		}
		if locs := splitCSV(t.Locations); len(locs) > 0 {
			tbl["locations"] = locs
		}
		if t.HTTPUser != "" {
			tbl["httpUser"] = t.HTTPUser
		}
		if t.HTTPPassword != "" {
			tbl["httpPassword"] = t.HTTPPassword
		}
		if t.HostHeaderRewrite != "" {
			tbl["hostHeaderRewrite"] = t.HostHeaderRewrite
		}
	case "https":
		if doms := splitCSV(t.CustomDomains); len(doms) > 0 {
			tbl["customDomains"] = doms
		}
		if sd := strings.TrimSpace(t.Subdomain); sd != "" {
			tbl["subdomain"] = sd
		}
	case "tcpmux":
		if doms := splitCSV(t.CustomDomains); len(doms) > 0 {
			tbl["customDomains"] = doms
		}
		if sd := strings.TrimSpace(t.Subdomain); sd != "" {
			tbl["subdomain"] = sd
		}
		if t.HTTPUser != "" {
			tbl["httpUser"] = t.HTTPUser
		}
		if t.HTTPPassword != "" {
			tbl["httpPassword"] = t.HTTPPassword
		}
		tbl["multiplexer"] = "httpconnect"
	case "stcp", "xtcp", "sudp":
		if t.SK != "" {
			tbl["secretKey"] = t.SK
		}
		if au := splitCSV(t.AllowUsers); len(au) > 0 {
			tbl["allowUsers"] = au
		}
	default:
		return nil, fmt.Errorf("tunnel %q: unsupported type %q", t.Name, typ)
	}

	if pluginName := strings.TrimSpace(t.Plugin); pluginName != "" {
		plugin, err := buildPluginTable(pluginName, t.PluginConfig)
		if err != nil {
			return nil, fmt.Errorf("tunnel %q plugin: %w", t.Name, err)
		}
		tbl["plugin"] = plugin
	}

	return tbl, nil
}

func buildVisitorTable(t *model.Tunnel) (map[string]any, error) {
	if t == nil {
		return nil, nil
	}
	typ := strings.ToLower(strings.TrimSpace(t.Type))
	switch typ {
	case "stcp", "xtcp", "sudp":
	default:
		return nil, fmt.Errorf("visitor %q: unsupported type %q", t.Name, typ)
	}
	tbl := map[string]any{
		"name":       tunnelName(t),
		"type":       typ,
		"role":       "visitor",
		"serverName": t.ServerName,
		"serverUser": t.ServerUser,
		"secretKey":  t.SK,
		"bindAddr":   t.LocalIP,
		"bindPort":   t.LocalPort,
	}
	if t.Encryption {
		tbl["transport"] = map[string]any{"useEncryption": true}
	}
	if t.Compression {
		if existing, ok := tbl["transport"].(map[string]any); ok {
			existing["useCompression"] = true
		} else {
			tbl["transport"] = map[string]any{"useCompression": true}
		}
	}
	return tbl, nil
}

// buildPluginTable parses the user-supplied `key=value` plugin config text
// (the same shape PluginConfigEditor.vue produces) and merges it into a
// `[proxies.plugin]` table, with `type = <pluginName>` injected so the
// frpc binary picks the right handler. Empty / blank input is allowed —
// some plugins (e.g. socks5 with no auth) take no parameters.
func buildPluginTable(pluginName, pluginConfig string) (map[string]any, error) {
	tbl := map[string]any{"type": pluginName}
	cfg := strings.TrimSpace(pluginConfig)
	if cfg == "" {
		return tbl, nil
	}
	parsed := map[string]any{}
	if err := toml.Unmarshal([]byte(cfg), &parsed); err != nil {
		return nil, fmt.Errorf("invalid plugin_config: %w", err)
	}
	for k, v := range parsed {
		if k == "type" {
			// Disallow user-supplied `type` overriding pluginName so the
			// driver-level invariant (one plugin name per tunnel) holds.
			continue
		}
		tbl[k] = v
	}
	return tbl, nil
}
