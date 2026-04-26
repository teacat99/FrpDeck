// Shared shapes for the FrpDeck REST API. Mirrors `internal/model/*.go`
// on the backend; keep them in sync when fields change.

export interface Endpoint {
  id: number
  name: string
  group: string
  addr: string
  port: number
  protocol: string
  user: string
  tls_enable: boolean
  tls_config: string
  pool_count: number
  heartbeat_interval: number
  heartbeat_timeout: number
  driver_mode: 'embedded' | 'subprocess'
  subprocess_path: string
  enabled: boolean
  auto_start: boolean
  created_at: string
  updated_at: string
}

export type TunnelStatus = 'pending' | 'active' | 'expired' | 'stopped' | 'failed'
export type TunnelSource = 'manual' | 'imported' | 'template' | 'remote_mgmt'

export interface Tunnel {
  id: number
  endpoint_id: number
  name: string
  type: string
  local_ip: string
  local_port: number
  remote_port: number
  custom_domains: string
  subdomain: string
  locations: string
  http_user: string
  host_header_rewrite: string
  allow_users: string
  role: string
  server_name: string
  encryption: boolean
  compression: boolean
  bandwidth_limit: string
  group: string
  group_key: string
  health_check_type: string
  health_check_url: string
  plugin: string
  plugin_config: string
  enabled: boolean
  auto_start: boolean
  expire_at?: string | null
  status: TunnelStatus
  last_start_at?: string | null
  last_stop_at?: string | null
  last_error: string
  source: TunnelSource
  template_id: string
  created_by: number
  created_at: string
  updated_at: string
}

// Write-side shapes. Distinct from the read shapes because the backend
// hides sensitive fields (token / sk / http_password) from GET responses
// but still accepts them on PUT/POST. A blank secret on PUT is the
// "leave it alone" signal — the backend keeps the previous value.
export interface EndpointWrite {
  name: string
  group?: string
  addr: string
  port: number
  protocol?: string
  user?: string
  token?: string
  meta_token?: string
  tls_enable?: boolean
  tls_config?: string
  pool_count?: number
  heartbeat_interval?: number
  heartbeat_timeout?: number
  driver_mode?: 'embedded' | 'subprocess'
  subprocess_path?: string
  enabled?: boolean
  auto_start?: boolean
}

export interface TunnelWrite {
  endpoint_id: number
  name: string
  type: string
  role?: '' | 'server' | 'visitor'
  local_ip?: string
  local_port?: number
  remote_port?: number
  custom_domains?: string
  subdomain?: string
  locations?: string
  http_user?: string
  http_password?: string
  host_header_rewrite?: string
  sk?: string
  allow_users?: string
  server_name?: string
  encryption?: boolean
  compression?: boolean
  bandwidth_limit?: string
  group?: string
  group_key?: string
  health_check_type?: string
  health_check_url?: string
  plugin?: string
  plugin_config?: string
  enabled?: boolean
  auto_start?: boolean
  // ISO 8601 (RFC3339) string. Pass null to clear an existing expiry.
  expire_at?: string | null
  // Stable template id from internal/templates (e.g. "ssh", "rdp").
  // Surfaced on create only; the backend stores it in tunnels.template_id
  // so we can later answer "which scenarios actually get used".
  template_id?: string
}

export type Role = 'admin' | 'user'

export interface User {
  id: number
  username: string
  role: Role
  disabled: boolean
  created_at: string
  updated_at: string
}

export interface Me {
  id: number
  username: string
  role: Role
  auth_mode: 'password' | 'ipwhitelist' | 'none'
}

export interface Setting {
  key: string
  value: string
  updated_at: string
}

export interface SettingsBundle {
  auth_mode: 'password' | 'ipwhitelist' | 'none'
  max_duration_hours: number
  history_retention_days: number
  trusted_proxies: string[]
  kv: Setting[]
}

export interface AuditLog {
  id: number
  action: string
  tunnel_id: number
  actor: string
  actor_ip: string
  detail: string
  created_at: string
}

export interface VersionInfo {
  frp_version: string
  driver: string
}

// Realtime event envelope. The backend emits these on /api/ws after the
// client subscribes to topics like "tunnels" / "endpoints" /
// "logs:endpoint:<id>" / "logs:tunnel:<id>". Mirrors `frpcd.Event`.
export type RealtimeEventType = 'endpoint_state' | 'tunnel_state' | 'log' | 'tunnel_expiring'

export interface RealtimeEvent {
  type: RealtimeEventType
  endpoint_id?: number
  tunnel_id?: number
  state?: string
  err?: string
  level?: string
  msg?: string
  at: string
}

// Endpoint runtime states emitted by the driver. We stick to the same
// vocabulary the backend uses so a literal switch maps directly to UI
// badges without translation.
export type EndpointState = 'disconnected' | 'connecting' | 'connected' | 'failed'

// Tunnel runtime states emitted by the driver. Distinct from the
// persisted `Tunnel.status` field; the live state is a richer view of
// what the running frpc engine reports.
export type TunnelRuntimeState =
  | 'pending'
  | 'starting'
  | 'running'
  | 'check_failed'
  | 'stopped'
  | 'error'
