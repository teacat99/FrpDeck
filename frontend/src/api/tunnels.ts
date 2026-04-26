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
