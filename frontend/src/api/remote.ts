import client from './client'

// RemoteNode mirrors `internal/model.RemoteNode`. Direction tells the
// frontend whether this row represents a peer that manages us
// ("manages_me" — A side after issuing an invitation) or a peer we
// manage ("managed_by_me" — B side after redeeming).
export type RemoteDirection = 'manages_me' | 'managed_by_me'

export type RemoteNodeStatus = 'pending' | 'active' | 'offline' | 'revoked' | 'expired'

export interface RemoteNode {
  id: number
  name: string
  direction: RemoteDirection
  endpoint_id: number
  tunnel_id: number
  remote_user: string
  local_bind_port: number
  // Auth token / sk / mgmt_token_jti are intentionally hidden by the
  // backend on GET; they only show up once on POST /invitations.
  status: RemoteNodeStatus
  last_seen?: string | null
  invite_expiry?: string | null
  created_at: string
  updated_at: string
}

export interface CreateInvitationPayload {
  endpoint_id: number
  node_name?: string
  ui_scheme?: 'http' | 'https'
}

export interface CreateInvitationResp {
  node: RemoteNode
  invitation: string
  expire_at: string
  mgmt_token: string
  tunnel_id: number
  driver_warning?: string
}

export interface RedeemInvitationPayload {
  invitation: string
  node_name?: string
}

export interface RedeemInvitationResp {
  node: RemoteNode
  redeem_url: string
  endpoint: { id: number; name: string; addr: string; port: number }
  driver_warning?: string
}

export interface RemoteRedeemTokenResp {
  token: string
  username: string
  role: 'admin' | 'user'
  node: RemoteNode
}

export async function listRemoteNodes(): Promise<RemoteNode[]> {
  const { data } = await client.get<{ nodes: RemoteNode[] }>('/remote/nodes')
  return data.nodes ?? []
}

export async function createRemoteInvitation(
  payload: CreateInvitationPayload
): Promise<CreateInvitationResp> {
  const { data } = await client.post<CreateInvitationResp>('/remote/invitations', payload)
  return data
}

export async function redeemRemoteInvitation(
  payload: RedeemInvitationPayload
): Promise<RedeemInvitationResp> {
  const { data } = await client.post<RedeemInvitationResp>('/remote/redeem', payload)
  return data
}

export async function revokeRemoteNode(id: number): Promise<RemoteNode> {
  const { data } = await client.delete<RemoteNode>(`/remote/nodes/${id}`)
  return data
}

// revokeRemoteMgmtToken voids the currently issued mgmt_token without
// tearing down the underlying stcp pairing. The pairing remains usable
// — the operator must call refreshRemoteInvitation() afterwards to mint
// a fresh QR for a new redeemer. Use this when a mgmt_token is suspected
// of being shared with the wrong contact and must be locked out before
// its 24h TTL elapses.
export async function revokeRemoteMgmtToken(id: number): Promise<RemoteNode> {
  const { data } = await client.post<RemoteNode>(`/remote/nodes/${id}/revoke-token`, {})
  return data
}

// refreshRemoteInvitation regenerates the invitation for an existing
// `manages_me` node. The backend rotates SK + mgmt_token JTI and replays
// the underlying tunnel; the response shape mirrors createRemoteInvitation
// so the frontend can reuse the invite dialog presentation logic.
export async function refreshRemoteInvitation(
  id: number,
  uiScheme?: 'http' | 'https'
): Promise<CreateInvitationResp> {
  const params = uiScheme ? { ui_scheme: uiScheme } : undefined
  const { data } = await client.post<CreateInvitationResp>(
    `/remote/nodes/${id}/refresh`,
    {},
    { params }
  )
  return data
}

// Token redemption is mounted on the public route group because the
// caller is using the mgmt_token *as* the credential. The frontend
// relies on this when bouncing the operator into a managed instance via
// `?_redeem=...`.
export async function exchangeMgmtToken(mgmtToken: string): Promise<RemoteRedeemTokenResp> {
  const { data } = await client.post<RemoteRedeemTokenResp>('/auth/remote-redeem', {
    mgmt_token: mgmtToken
  })
  return data
}
