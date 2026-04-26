import client from './client'
import type { Endpoint, EndpointWrite, VersionInfo } from './types'

export async function listEndpoints(): Promise<Endpoint[]> {
  const { data } = await client.get<{ endpoints: Endpoint[] }>('/endpoints')
  return data.endpoints ?? []
}

export async function getEndpoint(id: number): Promise<Endpoint> {
  const { data } = await client.get<Endpoint>(`/endpoints/${id}`)
  return data
}

export type EndpointPayload = EndpointWrite

export async function createEndpoint(payload: EndpointPayload): Promise<Endpoint> {
  const { data } = await client.post<Endpoint>('/endpoints', payload)
  return data
}

export async function updateEndpoint(id: number, payload: EndpointPayload): Promise<Endpoint> {
  const { data } = await client.put<Endpoint>(`/endpoints/${id}`, payload)
  return data
}

export async function deleteEndpoint(id: number): Promise<void> {
  await client.delete(`/endpoints/${id}`)
}

export async function fetchVersion(): Promise<VersionInfo> {
  const { data } = await client.get<VersionInfo>('/version')
  return data
}
