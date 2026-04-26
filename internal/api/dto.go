// Request DTOs for endpoint / tunnel CRUD.
//
// Why this file exists: model.Endpoint and model.Tunnel hide their
// sensitive fields with `json:"-"` so password-like values never leak
// through GET responses. That same tag, however, makes them invisible
// to ShouldBindJSON, so the only way to *write* them is through DTOs
// whose tags are read/write-friendly. The mapping helpers also fold in
// PUT-friendly semantics: a missing or empty secret keeps the previous
// value, and an explicit null on expire_at clears the expiry.

package api

import (
	"errors"
	"strings"
	"time"

	"github.com/teacat99/FrpDeck/internal/model"
)

// endpointReq mirrors model.Endpoint but exposes the secrets so the
// JSON binder can fill them. Any field omitted from the wire payload
// behaves like its zero value; PUT-side helpers (applyToEndpoint) carry
// existing secrets through when the request leaves them empty.
type endpointReq struct {
	Name              string `json:"name"`
	Group             string `json:"group"`
	Addr              string `json:"addr"`
	Port              int    `json:"port"`
	Protocol          string `json:"protocol"`
	User              string `json:"user"`
	Token             string `json:"token"`
	MetaToken         string `json:"meta_token"`
	TLSEnable         bool   `json:"tls_enable"`
	TLSConfig         string `json:"tls_config"`
	PoolCount         int    `json:"pool_count"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
	HeartbeatTimeout  int    `json:"heartbeat_timeout"`
	DriverMode        string `json:"driver_mode"`
	SubprocessPath    string `json:"subprocess_path"`
	Enabled           bool   `json:"enabled"`
	AutoStart         bool   `json:"auto_start"`
}

// validate performs cheap sanity checks before we touch the database.
// We surface stable English error strings; the frontend translates them
// in i18n. Empty / missing values are allowed for everything except
// the truly mandatory triple (name + addr + port).
func (r *endpointReq) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("name required")
	}
	if strings.TrimSpace(r.Addr) == "" {
		return errors.New("addr required")
	}
	if r.Port <= 0 || r.Port > 65535 {
		return errors.New("port out of range")
	}
	if r.DriverMode != "" && r.DriverMode != model.DriverModeEmbedded && r.DriverMode != model.DriverModeSubprocess {
		return errors.New("invalid driver_mode")
	}
	return nil
}

// applyToEndpoint copies the request into a target Endpoint. When `keep`
// is non-nil the sensitive fields (token / meta_token) carry the
// previous values forward whenever the request leaves them blank — the
// usual "leave password empty to keep current" UX. New rows pass
// keep=nil so blanks remain blanks.
func (r *endpointReq) applyToEndpoint(t *model.Endpoint, keep *model.Endpoint) {
	t.Name = strings.TrimSpace(r.Name)
	t.Group = strings.TrimSpace(r.Group)
	t.Addr = strings.TrimSpace(r.Addr)
	t.Port = r.Port
	t.Protocol = strings.TrimSpace(r.Protocol)
	t.User = strings.TrimSpace(r.User)
	t.TLSEnable = r.TLSEnable
	t.TLSConfig = r.TLSConfig
	t.PoolCount = r.PoolCount
	t.HeartbeatInterval = r.HeartbeatInterval
	t.HeartbeatTimeout = r.HeartbeatTimeout
	if r.DriverMode == "" {
		t.DriverMode = model.DriverModeEmbedded
	} else {
		t.DriverMode = r.DriverMode
	}
	t.SubprocessPath = strings.TrimSpace(r.SubprocessPath)
	t.Enabled = r.Enabled
	t.AutoStart = r.AutoStart

	if r.Token != "" {
		t.Token = r.Token
	} else if keep != nil {
		t.Token = keep.Token
	}
	if r.MetaToken != "" {
		t.MetaToken = r.MetaToken
	} else if keep != nil {
		t.MetaToken = keep.MetaToken
	}
}

// tunnelReq is the analogous request shape for tunnels. ExpireAt is a
// pointer so we can distinguish "absent" (don't touch / new = no expiry)
// from "explicit null" (PUT clearing the expiry); the JSON binder treats
// missing keys and explicit null both as nil pointer, so we additionally
// look at ExpireAtClear to disambiguate when needed.
type tunnelReq struct {
	EndpointID uint   `json:"endpoint_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Role       string `json:"role"`

	LocalIP   string `json:"local_ip"`
	LocalPort int    `json:"local_port"`

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
	ServerUser        string `json:"server_user"`

	Encryption     bool   `json:"encryption"`
	Compression    bool   `json:"compression"`
	BandwidthLimit string `json:"bandwidth_limit"`
	Group          string `json:"group"`
	GroupKey       string `json:"group_key"`

	HealthCheckType string `json:"health_check_type"`
	HealthCheckURL  string `json:"health_check_url"`

	Plugin       string `json:"plugin"`
	PluginConfig string `json:"plugin_config"`

	Enabled   bool `json:"enabled"`
	AutoStart bool `json:"auto_start"`

	ExpireAt *string `json:"expire_at"`

	// TemplateID records which scenario template seeded this tunnel.
	// Optional; the field is read on create and ignored on update so
	// editing an existing tunnel never clears its provenance.
	TemplateID string `json:"template_id"`
}

// validate cross-checks tunnel fields against the proxy / visitor
// type matrix. The intent is to refuse obviously wrong combinations
// before they reach the driver — we let the driver / frps refuse
// genuine edge cases.
func (r *tunnelReq) validate() error {
	if r.EndpointID == 0 {
		return errors.New("endpoint_id required")
	}
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("name required")
	}
	typ := strings.ToLower(strings.TrimSpace(r.Type))
	switch typ {
	case "tcp", "udp", "http", "https", "tcpmux", "stcp", "xtcp", "sudp":
	default:
		return errors.New("invalid type")
	}

	role := strings.ToLower(strings.TrimSpace(r.Role))
	if role != "" && role != "server" && role != "visitor" {
		return errors.New("invalid role")
	}
	if role == "visitor" && typ != "stcp" && typ != "xtcp" && typ != "sudp" {
		return errors.New("visitor only valid for stcp/xtcp/sudp")
	}

	if role != "visitor" {
		// proxy side validations
		switch typ {
		case "tcp", "udp":
			if r.RemotePort < 0 || r.RemotePort > 65535 {
				return errors.New("remote_port out of range")
			}
		case "http", "https", "tcpmux":
			if strings.TrimSpace(r.CustomDomains) == "" && strings.TrimSpace(r.Subdomain) == "" {
				return errors.New("custom_domains or subdomain required")
			}
		case "stcp", "xtcp", "sudp":
			if strings.TrimSpace(r.SK) == "" {
				return errors.New("sk required for stcp/xtcp/sudp")
			}
		}
		if r.LocalPort < 0 || r.LocalPort > 65535 {
			return errors.New("local_port out of range")
		}
	} else {
		if strings.TrimSpace(r.SK) == "" {
			return errors.New("sk required")
		}
		if strings.TrimSpace(r.ServerName) == "" {
			return errors.New("server_name required")
		}
		if r.LocalPort < 0 || r.LocalPort > 65535 {
			return errors.New("local_port out of range")
		}
	}

	if r.ExpireAt != nil && strings.TrimSpace(*r.ExpireAt) != "" {
		ts, err := time.Parse(time.RFC3339, *r.ExpireAt)
		if err != nil {
			return errors.New("expire_at must be RFC3339")
		}
		if !ts.After(time.Now()) {
			return errors.New("expire_at must be in the future")
		}
	}
	return nil
}

// applyToTunnel writes the request into a target Tunnel. As with
// endpointReq, blank secrets carry forward when keep is non-nil so
// the operator can tweak non-sensitive fields without re-typing the
// SK or HTTP password every time.
func (r *tunnelReq) applyToTunnel(t *model.Tunnel, keep *model.Tunnel) {
	t.EndpointID = r.EndpointID
	t.Name = strings.TrimSpace(r.Name)
	t.Type = strings.ToLower(strings.TrimSpace(r.Type))
	t.Role = strings.ToLower(strings.TrimSpace(r.Role))
	t.LocalIP = strings.TrimSpace(r.LocalIP)
	if t.LocalIP == "" {
		t.LocalIP = "127.0.0.1"
	}
	t.LocalPort = r.LocalPort
	t.RemotePort = r.RemotePort
	t.CustomDomains = r.CustomDomains
	t.Subdomain = r.Subdomain
	t.Locations = r.Locations
	t.HTTPUser = r.HTTPUser
	t.HostHeaderRewrite = r.HostHeaderRewrite
	t.AllowUsers = r.AllowUsers
	t.ServerName = r.ServerName
	t.ServerUser = r.ServerUser
	t.Encryption = r.Encryption
	t.Compression = r.Compression
	t.BandwidthLimit = r.BandwidthLimit
	t.Group = r.Group
	t.GroupKey = r.GroupKey
	t.HealthCheckType = r.HealthCheckType
	t.HealthCheckURL = r.HealthCheckURL
	t.Plugin = r.Plugin
	t.PluginConfig = r.PluginConfig
	t.Enabled = r.Enabled
	t.AutoStart = r.AutoStart

	if r.HTTPPassword != "" {
		t.HTTPPassword = r.HTTPPassword
	} else if keep != nil {
		t.HTTPPassword = keep.HTTPPassword
	}
	if r.SK != "" {
		t.SK = r.SK
	} else if keep != nil {
		t.SK = keep.SK
	}

	if r.ExpireAt == nil {
		t.ExpireAt = nil
	} else if v := strings.TrimSpace(*r.ExpireAt); v == "" {
		t.ExpireAt = nil
	} else if ts, err := time.Parse(time.RFC3339, v); err == nil {
		t.ExpireAt = &ts
	}

	// template_id is sticky on create and preserved on update: we never
	// blank it from the wire because the field is metadata, not user
	// content. Updates therefore inherit the existing value when keep
	// is non-nil and the request leaves it empty.
	if id := strings.TrimSpace(r.TemplateID); id != "" {
		t.TemplateID = id
	} else if keep != nil {
		t.TemplateID = keep.TemplateID
	}
}

// tunnelRenewReq is the payload for `POST /api/tunnels/:id/renew`.
//
// `extend_seconds` is a *int so callers can intentionally pass 0 to
// signal "make permanent" — distinct from omitting the field, which
// returns a 400 (we want explicit intent). Negative values are rejected
// so a typo cannot accidentally shorten an expiry; the canonical way to
// shorten is the regular PUT /tunnels/:id with a smaller expire_at.
type tunnelRenewReq struct {
	ExtendSeconds *int `json:"extend_seconds"`
}

// validate enforces the explicit-intent contract: callers must always
// pass `extend_seconds`, and only non-negative integers are allowed.
func (r *tunnelRenewReq) validate() error {
	if r.ExtendSeconds == nil {
		return errors.New("extend_seconds required")
	}
	if *r.ExtendSeconds < 0 {
		return errors.New("extend_seconds must be >= 0")
	}
	// 30 days upper bound matches the existing max_duration_hours
	// guardrail (720h ≈ 30d). Larger values are almost certainly a
	// client bug; the user can always renew again.
	if *r.ExtendSeconds > 30*24*3600 {
		return errors.New("extend_seconds too large")
	}
	return nil
}
