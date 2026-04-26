<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, Pencil, Trash2, Play, Square } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
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
  listTunnels, createTunnel, updateTunnel, deleteTunnel,
  startTunnel, stopTunnel,
} from '@/api/tunnels'
import { listEndpoints } from '@/api/endpoints'
import type { Endpoint, Tunnel, TunnelStatus } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const tunnels = ref<Tunnel[]>([])
const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const editing = ref<Tunnel | null>(null)
const dialogOpen = ref(false)
const submitting = ref(false)

const endpointMap = computed(() => {
  const m = new Map<number, Endpoint>()
  endpoints.value.forEach(e => m.set(e.id, e))
  return m
})

interface FormState {
  endpoint_id: number | null
  name: string
  type: string
  local_ip: string
  local_port: number | string
  remote_port: number | string
  custom_domains: string
  subdomain: string
}

const form = reactive<FormState>(emptyForm())

function emptyForm(): FormState {
  return {
    endpoint_id: null,
    name: '',
    type: 'tcp',
    local_ip: '127.0.0.1',
    local_port: 0,
    remote_port: 0,
    custom_domains: '',
    subdomain: '',
  }
}

const statusVariant: Record<TunnelStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  pending: 'secondary',
  active: 'default',
  expired: 'outline',
  stopped: 'outline',
  failed: 'destructive',
}

async function reload() {
  loading.value = true
  try {
    const [tns, eps] = await Promise.all([listTunnels(), listEndpoints()])
    tunnels.value = tns
    endpoints.value = eps
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  if (endpoints.value.length === 1) {
    form.endpoint_id = endpoints.value[0].id
  }
  dialogOpen.value = true
}

function openEdit(t: Tunnel) {
  editing.value = t
  Object.assign(form, {
    endpoint_id: t.endpoint_id,
    name: t.name,
    type: t.type || 'tcp',
    local_ip: t.local_ip || '127.0.0.1',
    local_port: t.local_port,
    remote_port: t.remote_port,
    custom_domains: t.custom_domains,
    subdomain: t.subdomain,
  })
  dialogOpen.value = true
}

async function submit() {
  if (!form.endpoint_id) {
    Message.warning(t('tunnel.required_endpoint'))
    return
  }
  if (!form.name.trim()) {
    Message.warning(t('tunnel.required_name'))
    return
  }
  submitting.value = true
  try {
    const payload = {
      endpoint_id: form.endpoint_id!,
      name: form.name.trim(),
      type: form.type,
      local_ip: form.local_ip,
      local_port: Number(form.local_port) || 0,
      remote_port: Number(form.remote_port) || 0,
      custom_domains: form.custom_domains,
      subdomain: form.subdomain,
    }
    if (editing.value) {
      await updateTunnel(editing.value.id, payload)
      Message.success(t('common.updated'))
    } else {
      await createTunnel(payload)
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

async function remove(tn: Tunnel) {
  if (!confirm(t('tunnel.confirm_delete', { name: tn.name }))) return
  try {
    await deleteTunnel(tn.id)
    Message.success(t('common.deleted'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.deleteFailed'))
  }
}

async function start(tn: Tunnel) {
  try {
    await startTunnel(tn.id)
    Message.success(t('tunnel.started'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  }
}

async function stop(tn: Tunnel) {
  try {
    await stopTunnel(tn.id)
    Message.success(t('tunnel.stopped'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  }
}

onMounted(reload)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('tunnel.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('tunnel.subtitle') }}</p>
      </div>
      <div class="flex gap-2">
        <Button variant="outline" :disabled="loading" @click="reload">
          <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />
          <span>{{ t('common.refresh') }}</span>
        </Button>
        <Button v-if="auth.isAdmin" :disabled="endpoints.length === 0" @click="openCreate">
          <Plus class="size-4" />
          <span>{{ t('tunnel.add') }}</span>
        </Button>
      </div>
    </div>

    <Table v-if="tunnels.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('tunnel.field.name') }}</TableHead>
          <TableHead>{{ t('tunnel.field.endpoint') }}</TableHead>
          <TableHead>{{ t('tunnel.field.type') }}</TableHead>
          <TableHead>{{ t('tunnel.field.target') }}</TableHead>
          <TableHead>{{ t('tunnel.field.status') }}</TableHead>
          <TableHead class="text-right">{{ t('common.actions') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="tn in tunnels" :key="tn.id">
          <TableCell class="font-medium">{{ tn.name }}</TableCell>
          <TableCell>
            <span class="text-sm">{{ endpointMap.get(tn.endpoint_id)?.name ?? `#${tn.endpoint_id}` }}</span>
          </TableCell>
          <TableCell>{{ tn.type }}</TableCell>
          <TableCell class="font-mono text-xs">
            {{ tn.local_ip }}:{{ tn.local_port }} → {{ tn.remote_port || tn.subdomain || tn.custom_domains || '—' }}
          </TableCell>
          <TableCell>
            <Badge :variant="statusVariant[tn.status]">{{ t(`tunnel.status.${tn.status}`) }}</Badge>
          </TableCell>
          <TableCell class="text-right">
            <Button v-if="auth.isAdmin && tn.status !== 'active'" size="icon" variant="ghost" @click="start(tn)">
              <Play class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin && tn.status === 'active'" size="icon" variant="ghost" @click="stop(tn)">
              <Square class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" @click="openEdit(tn)">
              <Pencil class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" class="text-destructive" @click="remove(tn)">
              <Trash2 class="size-4" />
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <EmptyState
      v-else
      icon="🔌"
      :title="endpoints.length ? t('tunnel.empty') : t('tunnel.no_endpoint')"
      :description="endpoints.length ? t('tunnel.empty_hint') : t('tunnel.no_endpoint_hint')"
    />

    <Dialog v-model:open="dialogOpen">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('tunnel.edit') : t('tunnel.add') }}</DialogTitle>
        </DialogHeader>
        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5 col-span-2">
            <Label>{{ t('tunnel.field.endpoint') }}</Label>
            <Select :model-value="form.endpoint_id ? String(form.endpoint_id) : ''" @update:model-value="(v: any) => form.endpoint_id = v ? Number(v) : null">
              <SelectTrigger><SelectValue :placeholder="t('tunnel.field.endpoint')" /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="ep in endpoints" :key="ep.id" :value="String(ep.id)">
                  {{ ep.name }} ({{ ep.addr }}:{{ ep.port }})
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.name') }}</Label>
            <Input v-model="form.name" placeholder="rdp-home" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.type') }}</Label>
            <Select v-model="form.type">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="tcp">tcp</SelectItem>
                <SelectItem value="udp">udp</SelectItem>
                <SelectItem value="http">http</SelectItem>
                <SelectItem value="https">https</SelectItem>
                <SelectItem value="stcp">stcp</SelectItem>
                <SelectItem value="xtcp">xtcp</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.local_ip') }}</Label>
            <Input v-model="form.local_ip" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.local_port') }}</Label>
            <Input v-model.number="form.local_port" type="number" min="0" max="65535" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.remote_port') }}</Label>
            <Input v-model.number="form.remote_port" type="number" min="0" max="65535" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('tunnel.field.subdomain') }}</Label>
            <Input v-model="form.subdomain" placeholder="rdp" />
          </div>
          <div class="flex flex-col gap-1.5 col-span-2">
            <Label>{{ t('tunnel.field.custom_domains') }}</Label>
            <Input v-model="form.custom_domains" placeholder="rdp.example.com" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
          <Button :disabled="submitting" @click="submit">{{ t('common.confirm') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
