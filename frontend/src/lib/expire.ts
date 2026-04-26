// Tiny helpers for the temporary-tunnel expiry UX. Centralised here so
// the table column and the dialog preview stay in lock step — when the
// list ticks "1m left → expired" the form should agree.

import dayjs from 'dayjs'

// formatRemaining produces a short, locale-friendly countdown such as
// "2d 4h" or "5m 12s". Returns null when the input is empty / null and
// "expired" when the timestamp has already passed.
export function formatRemaining(expireAt?: string | null): string | null {
  if (!expireAt) return null
  const target = dayjs(expireAt)
  if (!target.isValid()) return null
  const diff = target.diff(dayjs(), 'second')
  if (diff <= 0) return 'expired'
  const days = Math.floor(diff / 86400)
  const hours = Math.floor((diff % 86400) / 3600)
  const minutes = Math.floor((diff % 3600) / 60)
  const seconds = diff % 60
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${seconds}s`
  return `${seconds}s`
}

// addRelative returns ISO 8601 (RFC3339-ish, with offset) for "now plus
// N units". The backend parses with time.Parse(time.RFC3339), so we use
// the locale-aware ISO format that includes a timezone offset.
export function addRelative(amount: number, unit: 'hour' | 'day'): string {
  return dayjs().add(amount, unit).format('YYYY-MM-DDTHH:mm:ssZ')
}

// toIsoOrNull turns the value of an `<input type="datetime-local">` into
// an RFC3339 string the backend accepts. Empty input becomes null,
// signalling "no expiry / forever".
export function toIsoOrNull(local: string | null | undefined): string | null {
  if (!local) return null
  const v = dayjs(local)
  if (!v.isValid()) return null
  return v.format('YYYY-MM-DDTHH:mm:ssZ')
}

// toLocalInput converts an RFC3339 timestamp back into the format that
// `<input type="datetime-local">` understands ("YYYY-MM-DDTHH:mm").
export function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return ''
  const v = dayjs(iso)
  if (!v.isValid()) return ''
  return v.format('YYYY-MM-DDTHH:mm')
}
