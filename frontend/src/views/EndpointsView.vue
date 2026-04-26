<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, Pencil, Trash2, ChevronDown, ChevronRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import {
  Select, SelectTrigger, SelectValue, SelectContent, SelectItem,
} from '@/components/ui/select'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import EmptyState from '@/components/EmptyState.vue'
import { Message } from '@/lib/toast'
import {
  listEndpoints, createEndpoint, updateEndpoint, deleteEndpoint,
} from '@/api/endpoints'
import type { Endpoint, EndpointWrite } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'

const { t } = useI18n()
const auth = useAuthStore()
const realtime = useRealtimeStore()

const rows = ref<Endpoint[]>([])
const loading = ref(false)
const editing = ref<Endpoint | null>(null)
const dialogOpen = ref(false)
const submitting = ref(false)
const showAdvanced = ref(false)

interface FormState {
  name: string
  group: string
  addr: string
  port: number | string
  protocol: string
  token: string
  meta_token: string
  user: string
  driver_mode: 'embedded' | 'subprocess'
  tls_enable: boolean
  tls_config: string
  pool_count: number | string
  heartbeat_interval: number | string
  heartbeat_timeout: number | string
  enabled: boolean
  auto_start: boolean
}

const form = reactive<FormState>(emptyForm())

function emptyForm(): FormState {
  return {
    name: '',
    group: '',
    addr: '',
    port: 7000,
    protocol: 'tcp',
    token: '',
    meta_token: '',
    user: '',
    driver_mode: 'embedded',
    tls_enable: true,
    tls_config: '',
    pool_count: 0,
    heartbeat_interval: 0,
    heartbeat_timeout: 0,
    enabled: true,
    auto_start: true,
  }
}

async function reload() {
  loading.value = true
  try {
    rows.value = await listEndpoints()
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  showAdvanced.value = false
  dialogOpen.value = true
}

function openEdit(ep: Endpoint) {
  editing.value = ep
  Object.assign(form, {
    name: ep.name,
    group: ep.group,
    addr: ep.addr,
    port: ep.port,
    protocol: ep.protocol || 'tcp',
    token: '',
    meta_token: '',
    user: ep.user,
    driver_mode: ep.driver_mode || 'embedded',
    tls_enable: ep.tls_enable,
    tls_config: ep.tls_config,
    pool_count: ep.pool_count,
    heartbeat_interval: ep.heartbeat_interval,
    heartbeat_timeout: ep.heartbeat_timeout,
    enabled: ep.enabled,
    auto_start: ep.auto_start,
  })
  // Auto-expand advanced section if any non-default value exists, so we
  // never hide pre-existing config behind the fold.
  showAdvanced.value = Boolean(
    !ep.tls_enable || ep.tls_config || ep.pool_count > 0 ||
    ep.heartbeat_interval > 0 || ep.heartbeat_timeout > 0
  )
  dialogOpen.value = true
}

async function submit() {
  if (!form.name.trim() || !form.addr.trim()) {
    Message.warning(t('endpoint.required'))
    return
  }
  const port = Number(form.port)
  if (!port || port <= 0 || port > 65535) {
    Message.warning(t('endpoint.invalid_port'))
    return
  }
  submitting.value = true
  try {
    const payload: EndpointWrite = {
      name: form.name.trim(),
      group: form.group.trim(),
      addr: form.addr.trim(),
      port,
      protocol: form.protocol,
      token: form.token,
      meta_token: form.meta_token,
      user: form.user.trim(),
      driver_mode: form.driver_mode,
      tls_enable: form.tls_enable,
      tls_config: form.tls_config,
      pool_count: Number(form.pool_count) || 0,
      heartbeat_interval: Number(form.heartbeat_interval) || 0,
      heartbeat_timeout: Number(form.heartbeat_timeout) || 0,
      enabled: form.enabled,
      auto_start: form.auto_start,
    }
    if (editing.value) {
      await updateEndpoint(editing.value.id, payload)
      Message.success(t('common.updated'))
    } else {
      await createEndpoint(payload)
      Message.success(t('common.created'))
    }
    dialogOpen.value = false
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.saveFailed'))
  } finally {
    submitting.value = false
  }
}

async function remove(ep: Endpoint) {
  if (!confirm(t('endpoint.confirm_delete', { name: ep.name }))) return
  try {
    await deleteEndpoint(ep.id)
    Message.success(t('common.deleted'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.deleteFailed'))
  }
}

// Subscribe to live endpoint state on mount; unsubscribe on unmount
// so the WS server stops sending us events nobody is rendering.
let unsubEndpoints: (() => void) | null = null

onMounted(async () => {
  await reload()
  realtime.ensureConnected()
  unsubEndpoints = realtime.subscribeEndpoints()
})

onBeforeUnmount(() => {
  unsubEndpoints?.()
  unsubEndpoints = null
})

// Tone-mapping for the live endpoint badge. Defaulting to "secondary"
// keeps unfamiliar future states from rendering as visual noise.
function endpointBadgeVariant(state: string): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (state) {
    case 'connected': return 'default'
    case 'connecting': return 'secondary'
    case 'failed': return 'destructive'
    case 'disconnected':
    default:
      return 'outline'
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('endpoint.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('endpoint.subtitle') }}</p>
      </div>
      <div class="flex gap-2">
        <Button variant="outline" :disabled="loading" @click="reload">
          <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />
          <span>{{ t('common.refresh') }}</span>
        </Button>
        <Button v-if="auth.isAdmin" @click="openCreate">
          <Plus class="size-4" />
          <span>{{ t('endpoint.add') }}</span>
        </Button>
      </div>
    </div>

    <Table v-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('endpoint.field.name') }}</TableHead>
          <TableHead>{{ t('endpoint.field.addr') }}</TableHead>
          <TableHead>{{ t('endpoint.field.protocol') }}</TableHead>
          <TableHead>{{ t('endpoint.field.driver') }}</TableHead>
          <TableHead>{{ t('endpoint.field.tls_enable') }}</TableHead>
          <TableHead>{{ t('endpoint.field.enabled') }}</TableHead>
          <TableHead>{{ t('endpoint.field.live_state') }}</TableHead>
          <TableHead class="text-right">{{ t('common.actions') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="ep in rows" :key="ep.id">
          <TableCell class="font-medium">
            <div class="flex flex-col">
              <span>{{ ep.name }}</span>
              <span v-if="ep.group" class="text-xs text-muted-foreground">{{ ep.group }}</span>
            </div>
          </TableCell>
          <TableCell class="font-mono text-sm">{{ ep.addr }}:{{ ep.port }}</TableCell>
          <TableCell>{{ ep.protocol || 'tcp' }}</TableCell>
          <TableCell>
            <Badge variant="secondary">{{ ep.driver_mode }}</Badge>
          </TableCell>
          <TableCell>
            <Badge :variant="ep.tls_enable ? 'default' : 'outline'">
              {{ ep.tls_enable ? t('common.on') : t('common.off') }}
            </Badge>
          </TableCell>
          <TableCell>
            <Badge :variant="ep.enabled ? 'default' : 'outline'">
              {{ ep.enabled ? t('common.on') : t('common.off') }}
            </Badge>
          </TableCell>
          <TableCell>
            <Badge :variant="endpointBadgeVariant(realtime.endpointState(ep.id))">
              {{ t('endpoint.state.' + realtime.endpointState(ep.id)) }}
            </Badge>
          </TableCell>
          <TableCell class="text-right">
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" @click="openEdit(ep)">
              <Pencil class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" class="text-destructive" @click="remove(ep)">
              <Trash2 class="size-4" />
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <EmptyState v-else icon="🛰️" :title="t('endpoint.empty')" :description="t('endpoint.empty_hint')" />

    <Dialog v-model:open="dialogOpen">
      <DialogContent class="max-w-xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('endpoint.edit') : t('endpoint.add') }}</DialogTitle>
        </DialogHeader>

        <div class="flex flex-col gap-5">
          <section class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.name') }}</Label>
              <Input v-model="form.name" placeholder="aliyun-bj" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.group') }}</Label>
              <Input v-model="form.group" placeholder="prod" />
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('endpoint.field.addr') }}</Label>
              <Input v-model="form.addr" placeholder="frps.example.com" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.port') }}</Label>
              <Input v-model.number="form.port" type="number" min="1" max="65535" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.protocol') }}</Label>
              <Select v-model="form.protocol">
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="tcp">tcp</SelectItem>
                  <SelectItem value="kcp">kcp</SelectItem>
                  <SelectItem value="quic">quic</SelectItem>
                  <SelectItem value="websocket">websocket</SelectItem>
                  <SelectItem value="wss">wss</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('endpoint.field.token') }}</Label>
              <Input v-model="form.token" type="password" :placeholder="editing ? t('endpoint.field.token_keep') : ''" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.user') }}</Label>
              <Input v-model="form.user" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.driver') }}</Label>
              <Select v-model="form.driver_mode">
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="embedded">embedded</SelectItem>
                  <SelectItem value="subprocess">subprocess</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="flex items-center gap-4 col-span-2">
              <div class="flex items-center gap-2">
                <Switch v-model:checked="form.enabled" />
                <Label class="cursor-pointer m-0" @click="form.enabled = !form.enabled">{{ t('endpoint.field.enabled') }}</Label>
              </div>
              <div class="flex items-center gap-2">
                <Switch v-model:checked="form.auto_start" />
                <Label class="cursor-pointer m-0" @click="form.auto_start = !form.auto_start">{{ t('endpoint.field.auto_start') }}</Label>
              </div>
            </div>
          </section>

          <button
            type="button"
            class="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground self-start"
            @click="showAdvanced = !showAdvanced"
          >
            <component :is="showAdvanced ? ChevronDown : ChevronRight" class="size-4" />
            <span>{{ showAdvanced ? t('endpoint.advanced_hide') : t('endpoint.advanced') }}</span>
          </button>

          <section v-if="showAdvanced" class="grid grid-cols-2 gap-3 rounded-md border bg-muted/30 p-3">
            <div class="flex items-center gap-2 col-span-2">
              <Switch v-model:checked="form.tls_enable" />
              <div class="flex flex-col">
                <Label class="cursor-pointer m-0" @click="form.tls_enable = !form.tls_enable">
                  {{ t('endpoint.field.tls_enable') }}
                </Label>
                <span class="text-xs text-muted-foreground">{{ t('endpoint.field.tls_enable_hint') }}</span>
              </div>
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('endpoint.field.tls_config') }}</Label>
              <Input v-model="form.tls_config" placeholder="/etc/frp/tls/cert.pem" />
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('endpoint.field.meta_token') }}</Label>
              <Input
                v-model="form.meta_token"
                type="password"
                :placeholder="editing ? t('endpoint.field.token_keep') : t('endpoint.field.meta_token_hint')"
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.pool_count') }}</Label>
              <Input v-model.number="form.pool_count" type="number" min="0" />
              <span class="text-xs text-muted-foreground">{{ t('endpoint.field.pool_count_hint') }}</span>
            </div>
            <div />
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.heartbeat_interval') }}</Label>
              <Input v-model.number="form.heartbeat_interval" type="number" min="0" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('endpoint.field.heartbeat_timeout') }}</Label>
              <Input v-model.number="form.heartbeat_timeout" type="number" min="0" />
            </div>
          </section>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
          <Button :disabled="submitting" @click="submit">{{ t('common.confirm') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
