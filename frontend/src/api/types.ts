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
