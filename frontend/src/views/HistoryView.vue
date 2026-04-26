<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import dayjs from 'dayjs'
import { RefreshCw } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import EmptyState from '@/components/EmptyState.vue'
import client from '@/api/client'
import type { AuditLog } from '@/api/types'

const { t } = useI18n()

const rows = ref<AuditLog[]>([])
const loading = ref(false)

async function reload() {
  loading.value = true
  try {
    // POrtPass exposes a richer /api/history; FrpDeck currently only logs
    // /api/settings audit lines. Use /api/settings as the lightweight
    // surface until the audit endpoint lands in P1.
    const { data } = await client.get<{ logs: AuditLog[]; rows: AuditLog[] }>('/audit', {
      params: { limit: 200 },
    }).catch(() => ({ data: { logs: [] as AuditLog[], rows: [] as AuditLog[] } }))
    rows.value = data.logs ?? data.rows ?? []
  } finally {
    loading.value = false
  }
}

onMounted(reload)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('history.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('history.subtitle') }}</p>
      </div>
      <Button variant="outline" :disabled="loading" @click="reload">
        <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />
        <span>{{ t('common.refresh') }}</span>
      </Button>
    </div>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('history.field.time') }}</TableHead>
          <TableHead>{{ t('history.field.action') }}</TableHead>
          <TableHead>{{ t('history.field.actor') }}</TableHead>
          <TableHead>{{ t('history.field.detail') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="r in rows" :key="r.id">
          <TableCell class="font-mono text-xs whitespace-nowrap">
            {{ dayjs(r.created_at).format('YYYY-MM-DD HH:mm:ss') }}
          </TableCell>
          <TableCell><Badge variant="outline">{{ r.action }}</Badge></TableCell>
          <TableCell>
            <div class="flex flex-col">
              <span>{{ r.actor || '—' }}</span>
              <span class="text-xs text-muted-foreground font-mono">{{ r.actor_ip }}</span>
            </div>
          </TableCell>
          <TableCell class="text-sm">{{ r.detail || '' }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <EmptyState
      v-else
      icon="📜"
      :title="t('history.empty')"
      :description="t('history.empty_hint')"
    />
  </div>
</template>
