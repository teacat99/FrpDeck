package frpcd

import (
	"fmt"
	"strings"

	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/samber/lo"

	"github.com/teacat99/FrpDeck/internal/model"
)

// tunnelName returns the frp-side proxy/visitor name to use for a Tunnel.
// We prefer the user-given Name (so it shows up identically in frps' admin
// view) and fall back to a synthesized "tnl-<id>" only when Name is empty
// — that fallback should never trigger after API validation, but keeping
// it defensive means an edit-while-running never crashes the driver.
func tunnelName(t *model.Tunnel) string {
	if name := strings.TrimSpace(t.Name); name != "" {
		return name
	}
	return fmt.Sprintf("tnl-%d", t.ID)
}

// splitCSV parses a comma-separated list field (custom_domains, allow_users,
// locations) into a trimmed slice. Empty input yields a nil slice so the
// emitted JSON config does not include the key at all.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseBandwidth converts the user-facing string ("1MB", "200KB") into the
// types.BandwidthQuantity frp uses. An invalid value silently drops the
// limit — wrong-but-running beats startup-failed for a UX detail.
func parseBandwidth(s string) types.BandwidthQuantity {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.BandwidthQuantity{}
	}
	q, err := types.NewBandwidthQuantity(s)
	if err != nil {
		return types.BandwidthQuantity{}
	}
	return q
}

// applyProxyBase fills the fields shared by every ProxyConfigurer derived
// from a Tunnel. The proxy-type-specific code in BuildProxy then layers on
// the remaining fields.
func applyProxyBase(t *model.Tunnel, base *v1.ProxyBaseConfig) {
	base.Name = tunnelName(t)
	base.Type = strings.ToLower(t.Type)
	base.Transport.UseEncryption = t.Encryption
	base.Transport.UseCompression = t.Compression
	base.Transport.BandwidthLimit = parseBandwidth(t.BandwidthLimit)
	base.LoadBalancer.Group = t.Group
	base.LoadBalancer.GroupKey = t.GroupKey
	base.LocalIP = t.LocalIP
	base.LocalPort = t.LocalPort
	if t.HealthCheckType != "" {
		base.HealthCheck.Type = t.HealthCheckType
		if t.HealthCheckType == "http" {
			base.HealthCheck.Path = t.HealthCheckURL
		}
	}
}

// IsVisitor reports whether a tunnel describes the *visitor* (client) side
// of an stcp/xtcp/sudp pair. Visitors are dialled by the local user; only
// stcp/xtcp/sudp tunnel types may be visitors.
func IsVisitor(t *model.Tunnel) bool {
	return strings.EqualFold(t.Role, "visitor")
}

// BuildProxy converts a Tunnel into a frp ProxyConfigurer. It returns
// (nil, nil) when the tunnel is a visitor; callers in that case should
// use BuildVisitor.
func BuildProxy(t *model.Tunnel) (v1.ProxyConfigurer, error) {
	if IsVisitor(t) {
		return nil, nil
	}
	switch strings.ToLower(t.Type) {
	case "tcp":
		c := &v1.TCPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.RemotePort = t.RemotePort
		return c, nil
	case "udp":
		c := &v1.UDPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.RemotePort = t.RemotePort
		return c, nil
	case "http":
		c := &v1.HTTPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.CustomDomains = splitCSV(t.CustomDomains)
		c.SubDomain = strings.TrimSpace(t.Subdomain)
		c.Locations = splitCSV(t.Locations)
		c.HTTPUser = t.HTTPUser
		c.HTTPPassword = t.HTTPPassword
		c.HostHeaderRewrite = t.HostHeaderRewrite
		return c, nil
	case "https":
		c := &v1.HTTPSProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.CustomDomains = splitCSV(t.CustomDomains)
		c.SubDomain = strings.TrimSpace(t.Subdomain)
		return c, nil
	case "tcpmux":
		c := &v1.TCPMuxProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.CustomDomains = splitCSV(t.CustomDomains)
		c.SubDomain = strings.TrimSpace(t.Subdomain)
		c.HTTPUser = t.HTTPUser
		c.HTTPPassword = t.HTTPPassword
		c.Multiplexer = string(v1.TCPMultiplexerHTTPConnect)
		return c, nil
	case "stcp":
		c := &v1.STCPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.Secretkey = t.SK
		c.AllowUsers = splitCSV(t.AllowUsers)
		return c, nil
	case "xtcp":
		c := &v1.XTCPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.Secretkey = t.SK
		c.AllowUsers = splitCSV(t.AllowUsers)
		return c, nil
	case "sudp":
		c := &v1.SUDPProxyConfig{}
		applyProxyBase(t, &c.ProxyBaseConfig)
		c.Secretkey = t.SK
		c.AllowUsers = splitCSV(t.AllowUsers)
		return c, nil
	}
	return nil, fmt.Errorf("unsupported tunnel type %q", t.Type)
}

// BuildVisitor converts a Tunnel into a frp VisitorConfigurer. Returns
// (nil, nil) when the tunnel is not a visitor.
func BuildVisitor(t *model.Tunnel) (v1.VisitorConfigurer, error) {
	if !IsVisitor(t) {
		return nil, nil
	}
	typ := strings.ToLower(t.Type)
	base := v1.VisitorBaseConfig{
		Name:       tunnelName(t),
		Type:       typ,
		ServerName: t.ServerName,
		ServerUser: t.ServerUser,
		SecretKey:  t.SK,
		BindAddr:   t.LocalIP,
		BindPort:   t.LocalPort,
	}
	base.Transport.UseEncryption = t.Encryption
	base.Transport.UseCompression = t.Compression
	switch typ {
	case "stcp":
		return &v1.STCPVisitorConfig{VisitorBaseConfig: base}, nil
	case "xtcp":
		return &v1.XTCPVisitorConfig{VisitorBaseConfig: base}, nil
	case "sudp":
		return &v1.SUDPVisitorConfig{VisitorBaseConfig: base}, nil
	}
	return nil, fmt.Errorf("unsupported visitor type %q", t.Type)
}

// EndpointCommon translates an Endpoint row into frp's ClientCommonConfig.
// We deliberately skip the embedded admin web server (Port=0) and force
// LoginFailExit=false so a misconfigured frps does not crash the entire
// FrpDeck process — frpc will just retry until the operator fixes things.
func EndpointCommon(ep *model.Endpoint) *v1.ClientCommonConfig {
	c := &v1.ClientCommonConfig{
		ServerAddr: ep.Addr,
		ServerPort: ep.Port,
		User:       ep.User,
	}
	c.Auth.Method = v1.AuthMethodToken
	c.Auth.Token = ep.Token
	if ep.Protocol != "" {
		c.Transport.Protocol = ep.Protocol
	}
	if ep.PoolCount > 0 {
		c.Transport.PoolCount = ep.PoolCount
	}
	if ep.HeartbeatInterval != 0 {
		c.Transport.HeartbeatInterval = int64(ep.HeartbeatInterval)
	}
	if ep.HeartbeatTimeout != 0 {
		c.Transport.HeartbeatTimeout = int64(ep.HeartbeatTimeout)
	}
	if !ep.TLSEnable {
		c.Transport.TLS.Enable = lo.ToPtr(false)
	}
	c.WebServer.Port = 0
	c.LoginFailExit = lo.ToPtr(false)
	return c
}
