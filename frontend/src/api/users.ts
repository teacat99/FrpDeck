import client from './client'
import type { Role, User } from './types'

export async function listUsers() {
  const { data } = await client.get<{ users: User[] }>('/users')
  return data.users
}

export async function createUser(payload: { username: string; password: string; role: Role }) {
  const { data } = await client.post<User>('/users', payload)
  return data
}

export async function updateUser(id: number, patch: { role?: Role; disabled?: boolean }) {
  const { data } = await client.put<User>(`/users/${id}`, patch)
  return data
}

export async function resetUserPassword(id: number, newPassword: string) {
  await client.post(`/users/${id}/password`, { new_password: newPassword })
}

export async function deleteUser(id: number) {
  await client.delete(`/users/${id}`)
}
