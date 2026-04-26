import axios, { type AxiosInstance } from 'axios'
import { Message } from '@/lib/toast'
import i18n from '@/i18n'

const errorMap: Record<string, string> = {
  'rate limit exceeded': 'msg.rateLimitExceeded',
  'concurrent rule quota exceeded': 'msg.concurrentQuotaExceeded',
}

function localiseError(raw: string): string {
  const t = i18n.global.t as (key: string) => string
  const lower = raw.toLowerCase()
  for (const [pattern, key] of Object.entries(errorMap)) {
    if (lower.includes(pattern)) return t(key)
  }
  if (lower.includes('duration exceeds allowed')) return t('msg.durationExceeded')
  if (lower.includes('expiry exceeds max')) return t('msg.expiryExceeded')
  return raw
}

const client: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 15000
})

client.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('frpdeck.token')
  if (token) {
    cfg.headers = cfg.headers ?? {}
    cfg.headers.Authorization = `Bearer ${token}`
  }
  return cfg
})

client.interceptors.response.use(
  (resp) => resp,
  (err) => {
    const status = err?.response?.status
    const raw: string = err?.response?.data?.error ?? err.message ?? 'Network error'
    const message = localiseError(raw)
    if (status === 401) {
      localStorage.removeItem('frpdeck.token')
      if (!location.pathname.endsWith('/login')) {
        location.assign('/login')
      }
    } else if (status === 429) {
      Message.warning(message)
    } else if (status && status >= 500) {
      Message.error(message)
    } else if (status && status >= 400) {
      Message.warning(message)
    } else {
      Message.error(message)
    }
    return Promise.reject(err)
  }
)

export default client
