import client from './client'
import type { Tunnel, TunnelWrite } from './types'

export async function listTunnels(endpointId?: number): Promise<Tunnel[]> {
  const params = endpointId ? { endpoint_id: endpointId } : undefined
  const { data } = await client.get<{ tunnels: Tunnel[] }>('/tunnels', { params })
  return data.tunnels ?? []
}

export async function getTunnel(id: number): Promise<Tunnel> {
  const { data } = await client.get<Tunnel>(`/tunnels/${id}`)
  return data
}

export type TunnelPayload = TunnelWrite

export async function createTunnel(payload: TunnelPayload): Promise<Tunnel> {
  const { data } = await client.post<Tunnel>('/tunnels', payload)
  return data
}

export async function updateTunnel(id: number, payload: TunnelPayload): Promise<Tunnel> {
  const { data } = await client.put<Tunnel>(`/tunnels/${id}`, payload)
  return data
}

export async function deleteTunnel(id: number): Promise<void> {
  await client.delete(`/tunnels/${id}`)
}

export async function startTunnel(id: number): Promise<Tunnel> {
  const { data } = await client.post<Tunnel>(`/tunnels/${id}/start`)
  return data
}

export async function stopTunnel(id: number): Promise<Tunnel> {
  const { data } = await client.post<Tunnel>(`/tunnels/${id}/stop`)
  return data
}

// renewTunnel extends the tunnel's expiry by `extendSeconds`. Pass 0
// to make the tunnel permanent (clears `expire_at`). The lifecycle
// manager reactivates the row if it had auto-expired, so this is also
// the API behind the "uh, give me another hour" recovery button.
export async function renewTunnel(id: number, extendSeconds: number): Promise<Tunnel> {
  const { data } = await client.post<Tunnel>(`/tunnels/${id}/renew`, {
    extend_seconds: extendSeconds,
  })
  return data
}

// DiagStatus mirrors internal/diag.Status. Stable string union so the
// frontend can map status → i18n suffix and badge variant.
export type DiagStatus = 'ok' | 'warn' | 'fail' | 'skipped'

export interface DiagCheck {
  id: 'dns' | 'tcp_probe' | 'frps_register' | 'local_reach'
  status: DiagStatus
  message: string
  hint?: string
  duration_ms: number
}

export interface DiagReport {
  tunnel_id: number
  endpoint_id: number
  overall: DiagStatus
  generated_at: string
  checks: DiagCheck[]
}

// diagnoseTunnel runs the four-step connectivity self-check on the
// backend and returns the structured report. The detail panel calls
// this on save and exposes a manual "Re-run" button.
export async function diagnoseTunnel(id: number): Promise<DiagReport> {
  const { data } = await client.post<DiagReport>(`/tunnels/${id}/diagnose`)
  return data
}

// AdviceSeverity mirrors internal/frpshelper.Severity. plan.md §7.1
// renders required > recommended > info > warn as four distinct badge
// styles, so we keep the union exhaustive even though current rules
// don't emit `warn` yet.
export type AdviceSeverity = 'required' | 'recommended' | 'info' | 'warn'

export interface AdviceItem {
  severity: AdviceSeverity
  field?: string
  value?: string
  title: string
  detail?: string
  doc_url?: string
}

export interface FrpsAdvice {
  tunnel_id: number
  endpoint_id: number
  items: AdviceItem[]
  toml_snippet: string
  caveats?: string[]
}

// frpsAdvice fetches the structured "what does my frps.toml need"
// report. Pure read on the backend; safe to call repeatedly.
export async function frpsAdvice(id: number): Promise<FrpsAdvice> {
  const { data } = await client.get<FrpsAdvice>(`/tunnels/${id}/frps-advice`)
  return data
}

// TunnelTemplate is the frontend mirror of internal/templates.Template.
// The defaults map is intentionally untyped (Record<string, unknown>)
// because the backend can ship new keys without a frontend release —
// the wizard only consumes the keys it knows about and silently
// drops the rest.
export interface TunnelTemplate {
  id: string
  icon?: string
  name_key: string
  description_key: string
  audience_key: string
  prereq_keys?: string[]
  defaults: Record<string, unknown>
}

// listTunnelTemplates returns the 10 embedded scenario templates the
// "create from template" wizard renders. Auth-required but not
// admin-restricted; payload is a few KB.
export async function listTunnelTemplates(): Promise<TunnelTemplate[]> {
  const { data } = await client.get<{ templates: TunnelTemplate[] }>('/tunnels/templates')
  return data.templates ?? []
}

// ImportEndpointDraft mirrors internal/frpcimport.EndpointDraft. The
// import preview UI displays it as a read-only summary so the operator
// can confirm which existing endpoint to bind the imported tunnels to.
export interface ImportEndpointDraft {
  name: string
  group: string
  addr: string
  port: number
  protocol: string
  user: string
  token: string
  tls_enable: boolean
  pool_count: number
  heartbeat_interval: number
  heartbeat_timeout: number
  driver_mode: string
  enabled: boolean
  auto_start: boolean
}

// ImportTunnelDraft mirrors internal/frpcimport.TunnelDraft. The wire
// shape is intentionally close to TunnelWrite so the commit step can
// pass the user-edited drafts straight back without remapping.
export interface ImportTunnelDraft {
  name: string
  type: string
  role: string
  local_ip: string
  local_port: number
  remote_port: number
  custom_domains: string
  subdomain: string
  locations: string
  http_user: string
  http_password: string
  host_header_rewrite: string
  sk: string
  allow_users: string
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
  warnings?: string[]
  // Set by the backend when an endpoint_id was supplied to the preview
  // and a tunnel of the same name already exists. The UI uses this to
  // default the per-row OnConflict to "rename" instead of "error".
  conflict?: boolean
}

export type ImportConflictStrategy = 'error' | 'skip' | 'rename'

export interface ImportPlan {
  endpoint: ImportEndpointDraft | null
  tunnels: ImportTunnelDraft[]
  warnings?: string[]
  format: string
}

// importTunnelsPreview parses the supplied frpc.toml/yaml/json content
// and returns a Plan describing what would be created. No state is
// mutated — the user reviews the plan and then calls the commit API.
//
// When endpointId is provided we ask the backend to cross-check tunnel
// names against the picked endpoint and stamp `conflict: true` on each
// colliding draft.
export async function importTunnelsPreview(
  content: string,
  filename?: string,
  endpointId?: number,
): Promise<ImportPlan> {
  const { data } = await client.post<ImportPlan>('/tunnels/import/preview', {
    content,
    filename,
    endpoint_id: endpointId,
  })
  return data
}

export interface ImportCommitTunnel extends ImportTunnelDraft {
  on_conflict?: ImportConflictStrategy
}

export interface ImportCommitItem {
  name: string
  id?: number
  error?: string
  skipped?: boolean
  renamed?: string
}

// importTunnelsCommit creates the selected tunnel drafts under the
// chosen endpoint. Errors are returned per-tunnel so a single bad row
// does not abort the rest of the batch. defaultOnConflict applies to
// drafts whose `on_conflict` field is unset.
export async function importTunnelsCommit(
  endpointId: number,
  tunnels: ImportCommitTunnel[],
  defaultOnConflict: ImportConflictStrategy = 'error',
): Promise<ImportCommitItem[]> {
  const { data } = await client.post<{ items: ImportCommitItem[] }>(
    '/tunnels/import/commit',
    {
      endpoint_id: endpointId,
      tunnels,
      default_on_conflict: defaultOnConflict,
    },
  )
  return data.items ?? []
}
