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
