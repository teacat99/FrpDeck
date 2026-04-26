<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import dayjs from 'dayjs'

// CountdownChip renders a compact pill that shows the remaining time of a
// rule until expire_at. Colour transitions through four tones based on the
// fraction of time remaining, so even without reading the label the user
// gets a visual "how hot is this" cue.

const props = defineProps<{
  expireAt: string | Date
  createdAt?: string | Date
  size?: 'default' | 'small'
}>()

const now = ref<number>(Date.now())
let timer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  timer = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})

const expireMs = computed(() => dayjs(props.expireAt).valueOf())
const createdMs = computed(() => props.createdAt ? dayjs(props.createdAt).valueOf() : now.value - 60_000)
const remainingMs = computed(() => Math.max(0, expireMs.value - now.value))
const totalMs = computed(() => Math.max(1, expireMs.value - createdMs.value))
const ratio = computed(() => remainingMs.value / totalMs.value)

const tone = computed<'ok' | 'warn' | 'danger' | 'expired'>(() => {
  if (remainingMs.value <= 0) return 'expired'
  if (ratio.value > 0.3) return 'ok'
  if (ratio.value > 0.1) return 'warn'
  return 'danger'
})

const toneClass = computed(() => {
  switch (tone.value) {
    case 'ok':
      return 'text-emerald-600 border-emerald-500/40 dark:text-emerald-400'
    case 'warn':
      return 'text-amber-600 border-amber-500/40 dark:text-amber-400'
    case 'danger':
      return 'text-destructive border-destructive/40 animate-pulse'
    case 'expired':
    default:
      return 'text-muted-foreground border-border'
  }
})

const barTone = computed(() => {
  switch (tone.value) {
    case 'ok':      return 'bg-emerald-500/15'
    case 'warn':    return 'bg-amber-500/15'
    case 'danger':  return 'bg-destructive/15'
    default:        return 'bg-muted'
  }
})

const label = computed(() => {
  const ms = remainingMs.value
  if (ms <= 0) return '已过期'
  const s = Math.floor(ms / 1000)
  if (s < 60) return s + ' 秒'
  const m = Math.floor(s / 60)
  if (m < 60) return m + ' 分 ' + (s % 60).toString().padStart(2, '0')
  const h = Math.floor(m / 60)
  if (h < 24) return h + ' 时 ' + (m % 60).toString().padStart(2, '0') + ' 分'
  const d = Math.floor(h / 24)
  return d + ' 天 ' + (h % 24) + ' 时'
})
</script>

<template>
  <span
    class="relative inline-flex items-center justify-center rounded-full border text-xs font-medium tabular-nums overflow-hidden"
    :class="[
      toneClass,
      size === 'small' ? 'min-w-[72px] px-2 py-[1px] text-[11px]' : 'min-w-[92px] px-2.5 py-0.5'
    ]"
  >
    <span
      class="absolute inset-y-0 left-0 transition-[width] duration-1000 linear"
      :class="barTone"
      :style="{ width: (Math.min(100, ratio * 100)) + '%' }"
    />
    <span class="relative z-10 whitespace-nowrap">{{ label }}</span>
  </span>
</template>
