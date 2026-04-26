import client from './client'

// RuntimeSettings is a typed view of the snapshot returned by
// GET /api/runtime-settings. Keep the keys in sync with
// internal/runtime.AllKeys on the backend; PUT accepts the same names
// (with string values, server-side parsed and validated).
export interface RuntimeSettings {
  max_duration_hours: number
  history_retention_days: number
  max_rules_per_ip: number
  rate_limit_per_minute_per_ip: number

  login_fail_max_per_ip: number
  login_fail_window_ip_min: number
  login_fail_max_per_user: number
  login_fail_window_user_min: number
  login_lockout_ip_min: number
  login_lockout_user_min: number
  login_min_password_len: number

  login_fail_subnet_bits: number
  captcha_threshold: number

  // Lead time (in minutes) for the `tunnel_expiring` early-warning
  // event. 0 disables the warning entirely; the backend caps it at 120.
  tunnel_expiring_notify_minutes: number

  ntfy_url: string
  ntfy_topic: string
  // Token is redacted to a masked form for display; the real value is
  // write-only.
  ntfy_token: string
}

export interface RuntimeSystemInfo {
  listen: string
  data_dir: string
  driver: string
  frp_version: string
  auth_mode: string
  jwt_secret_set: boolean
  trusted_proxies: string[]
}

export interface RuntimeBundle {
  settings: RuntimeSettings
  system: RuntimeSystemInfo
}

export async function fetchRuntimeSettings(): Promise<RuntimeBundle> {
  const { data } = await client.get<RuntimeBundle>('/runtime-settings')
  return data
}

// updateRuntimeSettings sends an arbitrary subset of fields - only
// changed knobs need to be transmitted; the backend re-validates the
// whole payload as a unit.
export async function updateRuntimeSettings(
  patch: Record<string, string>,
): Promise<RuntimeBundle> {
  const { data } = await client.put<RuntimeBundle>('/runtime-settings', patch)
  return data
}

export async function testNotify(): Promise<void> {
  await client.post('/notify/test')
}
