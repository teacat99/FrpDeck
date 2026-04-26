<script setup lang="ts">
import { ref } from 'vue'
import { Copy, Check } from 'lucide-vue-next'
import { toast } from 'vue-sonner'

const props = defineProps<{
  value: string | number
  /** Override what gets put on the clipboard (vs displayed). */
  copyValue?: string
  /** Render value in monospace font. */
  mono?: boolean
  /** Hide the displayed value, show only the copy icon. */
  iconOnly?: boolean
  /** When true (default) truncates overflow; set false to allow wrapping. */
  truncate?: boolean
}>()

const copied = ref(false)

async function copy() {
  const v = String(props.copyValue ?? props.value ?? '')
  if (!v) return
  try {
    await navigator.clipboard.writeText(v)
    copied.value = true
    toast.success('已复制', { duration: 1200 })
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    toast.error('复制失败，请手动选择文本')
  }
}
</script>

<template>
  <span
    class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-muted transition-colors cursor-pointer max-w-full group align-middle"
    role="button"
    tabindex="0"
    :title="String(copyValue ?? value ?? '')"
    @click="copy"
    @keydown.enter.prevent="copy"
  >
    <span
      v-if="!iconOnly"
      class="select-text leading-none"
      :class="[
        mono ? 'font-mono tabular-nums' : '',
        truncate === false ? '' : 'truncate'
      ]"
    >{{ value }}</span>
    <component
      :is="copied ? Check : Copy"
      class="size-3.5 shrink-0 opacity-40 group-hover:opacity-100 group-hover:text-primary transition-opacity"
    />
  </span>
</template>
