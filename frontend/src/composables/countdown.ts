import { onBeforeUnmount, onMounted, ref } from 'vue'

// useNow returns a reactive `Date.now()` that updates every `intervalMs`.
// Views that display countdown timers import this single ticker rather than
// each row installing its own setInterval.
export function useNow(intervalMs = 1000) {
  const now = ref(Date.now())
  let id: ReturnType<typeof setInterval> | null = null
  onMounted(() => {
    id = setInterval(() => {
      now.value = Date.now()
    }, intervalMs)
  })
  onBeforeUnmount(() => {
    if (id) clearInterval(id)
  })
  return now
}

export function formatRemaining(expireAt: string, nowMs: number): string {
  const end = new Date(expireAt).getTime()
  let s = Math.floor((end - nowMs) / 1000)
  if (s <= 0) return '—'
  const d = Math.floor(s / 86400); s -= d * 86400
  const h = Math.floor(s / 3600); s -= h * 3600
  const m = Math.floor(s / 60); s -= m * 60
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}
