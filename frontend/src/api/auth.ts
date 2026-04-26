import client from './client'
import type { Me, Role } from './types'

export interface LastLoginInfo {
  at: string
  client_ip: string
  user_agent?: string
}

export interface LoginResponse {
  token: string
  username: string
  role: Role
  last_login?: LastLoginInfo
}

export interface LoginAttempt {
  id: number
  username: string
  client_ip: string
  success: boolean
  reason: string
  user_agent?: string
  created_at: string
}

export interface LoginPayload {
  username: string
  password: string
  captcha_id?: string
  captcha_answer?: string
}

export async function login(payload: LoginPayload): Promise<LoginResponse> {
  const { data } = await client.post<LoginResponse>('/auth/login', payload)
  return data
}

export interface CaptchaChallenge {
  id: string
  question: string
}

export async function fetchCaptcha(): Promise<CaptchaChallenge> {
  const { data } = await client.get<CaptchaChallenge>('/auth/captcha')
  return data
}

export async function authStatus(): Promise<{ mode: string; required: boolean }> {
  const { data } = await client.get<{ mode: string; required: boolean }>('/auth/status')
  return data
}

export async function getMe(): Promise<Me> {
  const { data } = await client.get<Me>('/auth/me')
  return data
}

export async function changeOwnPassword(oldPassword: string, newPassword: string) {
  await client.post('/auth/password', {
    old_password: oldPassword,
    new_password: newPassword
  })
}

export async function fetchMyLoginHistory(limit = 20): Promise<LoginAttempt[]> {
  const { data } = await client.get<{ attempts: LoginAttempt[] }>('/auth/my-recent-logins', {
    params: { limit }
  })
  return data.attempts ?? []
}

export async function fetchLoginHistory(params: { username?: string; limit?: number } = {}): Promise<LoginAttempt[]> {
  const { data } = await client.get<{ attempts: LoginAttempt[] }>('/auth/login-history', { params })
  return data.attempts ?? []
}
