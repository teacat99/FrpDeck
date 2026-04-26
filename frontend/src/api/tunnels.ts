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
