import client from './client'

export interface Profile {
  id: number
  name: string
  active: boolean
  created_at: string
  updated_at: string
}

export interface ProfileBinding {
  id?: number
  profile_id?: number
  endpoint_id: number
  tunnel_id: number
}

export interface ProfileDetail {
  profile: Profile
  bindings: ProfileBinding[]
}

export interface ProfilePayload {
  name: string
  active?: boolean
  bindings: ProfileBinding[]
}

export async function listProfiles(): Promise<Profile[]> {
  const { data } = await client.get<{ profiles: Profile[] }>('/profiles')
  return data.profiles ?? []
}

export async function getProfile(id: number): Promise<ProfileDetail> {
  const { data } = await client.get<ProfileDetail>(`/profiles/${id}`)
  return data
}

export async function getActiveProfile(): Promise<ProfileDetail | null> {
  const { data } = await client.get<{ profile: null } | ProfileDetail>('/profiles/active')
  if (!data || (data as { profile: null }).profile === null) return null
  return data as ProfileDetail
}

export async function createProfile(payload: ProfilePayload): Promise<ProfileDetail> {
  const { data } = await client.post<ProfileDetail>('/profiles', payload)
  return data
}

export async function updateProfile(id: number, payload: ProfilePayload): Promise<ProfileDetail> {
  const { data } = await client.put<ProfileDetail>(`/profiles/${id}`, payload)
  return data
}

export async function deleteProfile(id: number): Promise<void> {
  await client.delete(`/profiles/${id}`)
}

export async function activateProfile(id: number): Promise<ProfileDetail> {
  const { data } = await client.post<ProfileDetail>(`/profiles/${id}/activate`)
  return data
}

export async function deactivateProfiles(): Promise<void> {
  await client.post('/profiles/deactivate')
}

export interface ProbeResult {
  path: string
  version: string
  compatible: boolean
  min_required: string
}

export async function probeFrpc(path: string): Promise<ProbeResult> {
  const { data } = await client.post<ProbeResult>('/frpc/probe', { path })
  return data
}

export interface DownloadResult {
  path: string
  version: string
}

export interface DownloadPayload {
  version?: string
  os?: string
  arch?: string
  sha256?: string
}

export async function downloadFrpc(payload: DownloadPayload = {}): Promise<DownloadResult> {
  const { data } = await client.post<DownloadResult>('/frpc/download', payload)
  return data
}
