<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'

const props = defineProps<{ status: string }>()
const { t } = useI18n()

type Tone = 'active' | 'pending' | 'expired' | 'revoked' | 'failed'

interface Meta {
  variant: 'success' | 'warning' | 'muted' | 'destructive'
  label: string
  dot: string
}

// Single source of truth for the status colour palette so every page that
// shows a rule status uses the exact same vocabulary.
const palette: Record<Tone, Meta> = {
  active:  { variant: 'success',     label: 'status.active',  dot: 'bg-emerald-500' },
  pending: { variant: 'warning',     label: 'status.pending', dot: 'bg-amber-500' },
  expired: { variant: 'muted',       label: 'status.expired', dot: 'bg-muted-foreground' },
  revoked: { variant: 'destructive', label: 'status.revoked', dot: 'bg-destructive' },
  failed:  { variant: 'destructive', label: 'status.failed',  dot: 'bg-destructive' }
}

const meta = computed<Meta>(() => palette[props.status as Tone] ?? {
  variant: 'muted',
  label: props.status,
  dot: 'bg-muted-foreground'
})
</script>

<template>
  <Badge :variant="meta.variant" class="gap-1.5">
    <span class="inline-block size-1.5 rounded-full" :class="meta.dot"></span>
    {{ meta.label.startsWith('status.') ? t(meta.label) : meta.label }}
  </Badge>
</template>
