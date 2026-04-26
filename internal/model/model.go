// Package model defines the GORM-backed entities persisted by FrpDeck.
//
// Layering note: anything in this package describes "shape on disk".
// Business behaviour lives in `internal/store` (CRUD), `internal/api`
// (HTTP), `internal/lifecycle` (timers + reconcile) and `internal/frpcd`
// (the frp client driver abstraction).
package model

import "time"

// Tunnel lifecycle states. Mirrors the PortPass-DNA state machine but
// with FrpDeck-flavoured terminology:
//
//	pending  → tunnel persisted, not yet up on frps
//	active   → live on frps, traffic accepted
//	expired  → ExpireAt elapsed; auto-stopped by lifecycle
//	stopped  → manually stopped by operator
//	failed   → driver reported a hard error
const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusExpired = "expired"
	StatusStopped = "stopped"
	StatusFailed  = "failed"
)

// User role enumeration. Cloned verbatim from PortPass: admin owns the
// instance (endpoints / users / settings); user is a constrained role
// reserved for future use cases such as scoped tunnel ownership.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Endpoint driver mode. Selects which FrpDriver implementation
// in `internal/frpcd` services this Endpoint at runtime.
const (
	DriverModeEmbedded   = "embedded"
	DriverModeSubprocess = "subprocess"
)

// Tunnel source markers. Used by the UI to badge tunnels imported from
// a frpc.toml, instantiated from a scenario template, or pushed via the
// remote-management channel — distinct from manually authored ones.
const (
	TunnelSourceManual     = "manual"
	TunnelSourceImported   = "imported"
	TunnelSourceTemplate   = "template"
	TunnelSourceRemoteMgmt = "remote_mgmt"
)

// RemoteNode pairing direction. "managed_by_me" means this row controls
// the listed peer; "manages_me" means the peer is allowed to dial in
// and manipulate our state.
const (
	RemoteDirectionManagedByMe = "managed_by_me"
	RemoteDirectionManagesMe   = "manages_me"
)

// Endpoint represents one frps server FrpDeck connects to. Each Endpoint
// runs its own FrpDriver instance; multiple Endpoints may operate in
// parallel against different frps servers (the differentiator vs.
// frpc-desktop, see plan.md §1.3).
type Endpoint struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Name              string    `gorm:"size:64;not null" json:"name"`
	Group             string    `gorm:"size:64" json:"group"`
	Addr              string    `gorm:"size:255;not null" json:"addr"`
	Port              int       `gorm:"not null" json:"port"`
	Protocol          string    `gorm:"size:16" json:"protocol"`
	Token             string    `gorm:"size:255" json:"-"`
	User              string    `gorm:"size:64" json:"user"`
	MetaToken         string    `gorm:"size:255" json:"-"`
	TLSEnable         bool      `gorm:"default:false" json:"tls_enable"`
	TLSConfig         string    `gorm:"type:text" json:"tls_config"`
	PoolCount         int       `gorm:"default:0" json:"pool_count"`
	HeartbeatInterval int       `gorm:"default:0" json:"heartbeat_interval"`
	HeartbeatTimeout  int       `gorm:"default:0" json:"heartbeat_timeout"`
	DriverMode        string    `gorm:"size:16;default:embedded" json:"driver_mode"`
	SubprocessPath    string    `gorm:"size:512" json:"subprocess_path"`
	Enabled           bool      `gorm:"default:true" json:"enabled"`
	AutoStart         bool      `gorm:"default:true" json:"auto_start"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Tunnel is a single frp proxy / visitor configuration. The state
// machine field (Status) and the temporary-tunnel field (ExpireAt) are
// the heart of the FrpDeck differentiator vs. frpc-desktop.
type Tunnel struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	EndpointID uint   `gorm:"index;not null" json:"endpoint_id"`
	Name       string `gorm:"size:128;not null" json:"name"`
	Type       string `gorm:"size:16" json:"type"`

	LocalIP   string `gorm:"size:64;default:127.0.0.1" json:"local_ip"`
	LocalPort int    `json:"local_port"`

	RemotePort        int    `json:"remote_port"`
	CustomDomains     string `gorm:"size:512" json:"custom_domains"`
	Subdomain         string `gorm:"size:128" json:"subdomain"`
	Locations         string `gorm:"size:512" json:"locations"`
	HTTPUser          string `gorm:"size:64" json:"http_user"`
	HTTPPassword      string `gorm:"size:255" json:"-"`
	HostHeaderRewrite string `gorm:"size:255" json:"host_header_rewrite"`
	SK                string `gorm:"size:255" json:"-"`
	AllowUsers        string `gorm:"size:255" json:"allow_users"`
	Role              string `gorm:"size:16" json:"role"`
	ServerName        string `gorm:"size:128" json:"server_name"`

	Encryption     bool   `gorm:"default:false" json:"encryption"`
	Compression    bool   `gorm:"default:false" json:"compression"`
	BandwidthLimit string `gorm:"size:32" json:"bandwidth_limit"`
	Group          string `gorm:"size:64" json:"group"`
	GroupKey       string `gorm:"size:128" json:"group_key"`

	HealthCheckType string `gorm:"size:16" json:"health_check_type"`
	HealthCheckURL  string `gorm:"size:255" json:"health_check_url"`

	Plugin       string `gorm:"size:64" json:"plugin"`
	PluginConfig string `gorm:"type:text" json:"plugin_config"`

	Enabled   bool `gorm:"default:true" json:"enabled"`
	AutoStart bool `gorm:"default:true" json:"auto_start"`

	// Temporary-tunnel lifecycle (cf. plan.md §6).
	ExpireAt    *time.Time `gorm:"index" json:"expire_at,omitempty"`
	Status      string     `gorm:"index;size:16" json:"status"`
	LastStartAt *time.Time `json:"last_start_at,omitempty"`
	LastStopAt  *time.Time `json:"last_stop_at,omitempty"`
	LastError   string     `gorm:"size:512" json:"last_error"`

	Source     string `gorm:"size:16;default:manual" json:"source"`
	TemplateID string `gorm:"size:64" json:"template_id"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Profile is a named bundle of (Endpoint, Tunnel) bindings. Switching
// the active Profile flips the entire active set in one click — useful
// for "home" vs. "office" vs. "demo" scenarios.
type Profile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Active    bool      `gorm:"default:false" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProfileBinding links a Profile to the (Endpoint, Tunnel) entries it
// activates. EndpointID alone (TunnelID=0) means "include all tunnels
// under that endpoint" — handy for grouping by environment.
type ProfileBinding struct {
	ID         uint `gorm:"primaryKey" json:"id"`
	ProfileID  uint `gorm:"index;not null" json:"profile_id"`
	EndpointID uint `gorm:"index" json:"endpoint_id"`
	TunnelID   uint `gorm:"index" json:"tunnel_id"`
}

// RemoteNode represents a paired peer FrpDeck instance reachable via a
// self-hosted stcp tunnel. Phase P5 fills in the actual pairing flow;
// the entity is staked here so the schema migrates cleanly later.
type RemoteNode struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Name          string     `gorm:"size:64;not null" json:"name"`
	Direction     string     `gorm:"size:16" json:"direction"`
	EndpointID    uint       `gorm:"index" json:"endpoint_id"`
	SK            string     `gorm:"size:255" json:"-"`
	LocalBindPort int        `json:"local_bind_port"`
	AuthToken     string     `gorm:"size:255" json:"-"`
	InviteExpiry  *time.Time `json:"invite_expiry,omitempty"`
	Status        string     `gorm:"size:16" json:"status"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// User is an account stored in the local SQLite database. The password is
// always persisted as a bcrypt hash; the plaintext is never stored nor
// returned via JSON. Role governs what the user may see and do.
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	Role         string    `gorm:"size:16;not null;default:user" json:"role"`
	Disabled     bool      `gorm:"default:false" json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Setting is a free-form key/value configuration row. Used for runtime-mutable
// preferences that are more convenient to edit in the UI than via env vars.
type Setting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AuditLog records every mutating action for compliance and forensics.
// TunnelID stays optional (0 means a non-tunnel-scoped action such as
// user CRUD or settings change).
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Action    string    `gorm:"index;size:32" json:"action"`
	TunnelID  uint      `gorm:"index" json:"tunnel_id"`
	Actor     string    `gorm:"size:64" json:"actor"`
	ActorIP   string    `gorm:"size:64" json:"actor_ip"`
	Detail    string    `gorm:"type:text" json:"detail"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// LoginAttempt records every authentication attempt (success or failure)
// so we can rate-limit attackers, detect brute force, and give real users
// visibility into activity on their account. Separate from AuditLog because
// we keep a tighter retention window (matching LoginFailWindow*) and the
// hot-path cardinality is very different.
type LoginAttempt struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"index;size:64" json:"username"`
	ClientIP  string    `gorm:"index;size:64" json:"client_ip"`
	Success   bool      `gorm:"index" json:"success"`
	Reason    string    `gorm:"size:64" json:"reason"`
	UserAgent string    `gorm:"size:255" json:"user_agent"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
