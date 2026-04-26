<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Plus, RefreshCw, Pencil, Trash2, Play, Square,
  ChevronDown, ChevronRight, Clock, AlarmClockPlus, Infinity as InfinityIcon,
} from 'lucide-vue-next'
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
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent,
  DropdownMenuItem, DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import EmptyState from '@/components/EmptyState.vue'
import { Message } from '@/lib/toast'
import {
  listTunnels, createTunnel, updateTunnel, deleteTunnel,
  startTunnel, stopTunnel, renewTunnel,
} from '@/api/tunnels'
import { listEndpoints } from '@/api/endpoints'
import type { Endpoint, Tunnel, TunnelStatus, TunnelWrite } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import { addRelative, formatRemaining, toIsoOrNull, toLocalInput } from '@/lib/expire'

const { t } = useI18n()
const auth = useAuthStore()
const realtime = useRealtimeStore()

const tunnels = ref<Tunnel[]>([])
const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const editing = ref<Tunnel | null>(null)
const dialogOpen = ref(false)
const submitting = ref(false)
const showAdvanced = ref(false)

// Tick once a second to keep the per-row countdown fresh. Cheap (just a
// reactive integer) and avoids needing a per-row interval.
const nowTick = ref(Date.now())
let tickHandle: number | undefined

const endpointMap = computed(() => {
  const m = new Map<number, Endpoint>()
  endpoints.value.forEach(e => m.set(e.id, e))
  return m
})

// FormState mirrors TunnelWrite but uses string ports so the user can
// freely type in <input type="number"> without immediately collapsing
// blank to 0. We coerce on submit.
interface FormState {
  endpoint_id: number | null
  name: string
  type: string
  role: '' | 'server' | 'visitor'
  local_ip: string
  local_port: number | string
  remote_port: number | string
  custom_domains: string
  subdomain: string
  locations: string
  http_user: string
  http_password: string
  host_header_rewrite: string
  sk: string
  allow_users: string
  server_name: string
  encryption: boolean
  compression: boolean
  bandwidth_limit: string
  group: string
  group_key: string
  health_check_type: string
  health_check_url: string
  plugin: string
  plugin_config: string
  enabled: boolean
  auto_start: boolean
  expire_local: string // value of <input type="datetime-local">
}

const form = reactive<FormState>(emptyForm())

function emptyForm(): FormState {
  return {
    endpoint_id: null,
    name: '',
    type: 'tcp',
    role: '',
    local_ip: '127.0.0.1',
    local_port: '',
    remote_port: '',
    custom_domains: '',
    subdomain: '',
    locations: '',
    http_user: '',
    http_password: '',
    host_header_rewrite: '',
    sk: '',
    allow_users: '',
    server_name: '',
    encryption: false,
    compression: false,
    bandwidth_limit: '',
    group: '',
    group_key: '',
    health_check_type: '',
    health_check_url: '',
    plugin: '',
    plugin_config: '',
    enabled: true,
    auto_start: true,
    expire_local: '',
  }
}

const statusVariant: Record<TunnelStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  pending: 'secondary',
  active: 'default',
  expired: 'outline',
  stopped: 'outline',
  failed: 'destructive',
}

// canHaveVisitor flags the secret-suite proxy types that have a paired
// visitor side. These are the only types where the role switch is
// meaningful; everything else is implicitly "server".
const SECRET_TYPES = new Set(['stcp', 'xtcp', 'sudp'])
const HTTP_TYPES = new Set(['http', 'https', 'tcpmux'])

const isSecret = computed(() => SECRET_TYPES.has(form.type))
const isHttp = computed(() => HTTP_TYPES.has(form.type))
const isVisitor = computed(() => form.role === 'visitor')

// previewRemaining reflects the user's current expire_local pick so the
// helper text below the datetime input shows what the lifecycle
// manager will actually do.
const previewRemaining = computed(() => {
  const iso = toIsoOrNull(form.expire_local)
  return iso ? formatRemaining(iso) : null
})

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
  showAdvanced.value = false
  dialogOpen.value = true
}

function openEdit(tn: Tunnel) {
  editing.value = tn
  Object.assign(form, {
    endpoint_id: tn.endpoint_id,
    name: tn.name,
    type: tn.type || 'tcp',
    role: (tn.role === 'server' || tn.role === 'visitor' ? tn.role : '') as FormState['role'],
    local_ip: tn.local_ip || '127.0.0.1',
    local_port: tn.local_port || '',
    remote_port: tn.remote_port || '',
    custom_domains: tn.custom_domains,
    subdomain: tn.subdomain,
    locations: tn.locations,
    http_user: tn.http_user,
    http_password: '',
    host_header_rewrite: tn.host_header_rewrite,
    sk: '',
    allow_users: tn.allow_users,
    server_name: tn.server_name,
    encryption: tn.encryption,
    compression: tn.compression,
    bandwidth_limit: tn.bandwidth_limit,
    group: tn.group,
    group_key: tn.group_key,
    health_check_type: tn.health_check_type,
    health_check_url: tn.health_check_url,
    plugin: tn.plugin,
    plugin_config: tn.plugin_config,
    enabled: tn.enabled,
    auto_start: tn.auto_start,
    expire_local: toLocalInput(tn.expire_at ?? null),
  })
  // Open the advanced section automatically if any of its fields carry
  // values — avoids hiding existing config behind a fold.
  showAdvanced.value = Boolean(
    tn.encryption || tn.compression || tn.bandwidth_limit || tn.group ||
    tn.group_key || tn.health_check_type || tn.plugin
  )
  dialogOpen.value = true
}

function setExpirePreset(unit: 'hour' | 'day', amount: number) {
  form.expire_local = toLocalInput(addRelative(amount, unit))
}

function clearExpire() {
  form.expire_local = ''
}

function validate(): string | null {
  if (!form.endpoint_id) return t('tunnel.required_endpoint')
  if (!form.name.trim()) return t('tunnel.required_name')
  if (!form.type) return t('tunnel.validation.type_required')

  if (form.role === 'visitor' && !SECRET_TYPES.has(form.type)) {
    return t('tunnel.validation.visitor_only_for_secret')
  }

  const localPort = Number(form.local_port) || 0
  const remotePort = Number(form.remote_port) || 0
  if (localPort < 0 || localPort > 65535 || remotePort < 0 || remotePort > 65535) {
    return t('tunnel.validation.port_range')
  }

  if (isVisitor.value) {
    if (!form.sk && !editing.value) return t('tunnel.validation.sk_required')
    if (!form.server_name.trim()) return t('tunnel.validation.server_name_required')
  } else if (isSecret.value) {
    if (!form.sk && !editing.value) return t('tunnel.validation.sk_required')
  } else if (isHttp.value) {
    if (!form.subdomain.trim() && !form.custom_domains.trim()) {
      return t('tunnel.validation.domains_required')
    }
  }

  if (form.expire_local) {
    const iso = toIsoOrNull(form.expire_local)
    if (!iso || formatRemaining(iso) === 'expired') {
      return t('tunnel.validation.expire_in_past')
    }
  }
  return null
}

async function submit() {
  const err = validate()
  if (err) {
    Message.warning(err)
    return
  }
  submitting.value = true
  try {
    const payload: TunnelWrite = {
      endpoint_id: form.endpoint_id!,
      name: form.name.trim(),
      type: form.type,
      role: form.role,
      local_ip: form.local_ip || '127.0.0.1',
      local_port: Number(form.local_port) || 0,
      remote_port: Number(form.remote_port) || 0,
      custom_domains: form.custom_domains,
      subdomain: form.subdomain,
      locations: form.locations,
      http_user: form.http_user,
      http_password: form.http_password,
      host_header_rewrite: form.host_header_rewrite,
      sk: form.sk,
      allow_users: form.allow_users,
      server_name: form.server_name,
      encryption: form.encryption,
      compression: form.compression,
      bandwidth_limit: form.bandwidth_limit,
      group: form.group,
      group_key: form.group_key,
      health_check_type: form.health_check_type,
      health_check_url: form.health_check_url,
      plugin: form.plugin,
      plugin_config: form.plugin_config,
      enabled: form.enabled,
      auto_start: form.auto_start,
      expire_at: form.expire_local ? toIsoOrNull(form.expire_local) : null,
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
    realtime.dismissExpiring(tn.id)
    Message.success(t('tunnel.stopped'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  }
}

// renew issues a one-shot extension to the tunnel's expiry. The
// backend reactivates expired rows in-place so the operator can
// rescue a stale tunnel without re-creating it. extendSeconds=0 is
// the explicit "make permanent" path.
async function renew(tn: Tunnel, extendSeconds: number) {
  try {
    await renewTunnel(tn.id, extendSeconds)
    realtime.dismissExpiring(tn.id)
    Message.success(extendSeconds === 0 ? t('tunnel.renewed_permanent') : t('tunnel.renewed'))
    await reload()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  }
}

// remainingForRow re-reads nowTick so the computed re-evaluates each
// second. We don't actually need the value, just the dependency.
function remainingForRow(tn: Tunnel): string | null {
  void nowTick.value
  return formatRemaining(tn.expire_at ?? null)
}

// rowIsExpiringSoon highlights a row that has either received a live
// `tunnel_expiring` warning OR whose persisted expire_at is within the
// next 60s of the latest tick (covers the case where the user lands on
// the page after the warning fired).
function rowIsExpiringSoon(tn: Tunnel): boolean {
  void nowTick.value
  if (tn.status !== 'active') return false
  if (realtime.tunnelExpiringInfo(tn.id)) return true
  if (!tn.expire_at) return false
  const ms = Date.parse(tn.expire_at)
  if (Number.isNaN(ms)) return false
  return ms - Date.now() <= 5 * 60 * 1000 && ms > Date.now()
}

// Quick-action presets surfaced inside the renew dropdown. Mapped to
// seconds so the API contract stays a single integer field.
const RENEW_PRESETS: { labelKey: string, seconds: number }[] = [
  { labelKey: 'tunnel.renew.plus_1h', seconds: 3600 },
  { labelKey: 'tunnel.renew.plus_1d', seconds: 86400 },
  { labelKey: 'tunnel.renew.plus_7d', seconds: 7 * 86400 },
]

let unsubTunnels: (() => void) | null = null

onMounted(() => {
  reload()
  tickHandle = window.setInterval(() => { nowTick.value = Date.now() }, 1000)
  realtime.ensureConnected()
  unsubTunnels = realtime.subscribeTunnels()
})

onUnmounted(() => {
  if (tickHandle) clearInterval(tickHandle)
  unsubTunnels?.()
  unsubTunnels = null
})

// liveStateLabel surfaces the driver's runtime tunnel state when it
// disagrees with the persisted status (e.g. status=active but the
// driver actually reports check_failed). Returns null when the live
// state is in sync with the DB so we don't render redundant chrome.
function liveStateLabel(tn: Tunnel): string | null {
  const live = realtime.tunnelState(tn.id)
  if (!live) return null
  // Active vs running mean the same thing to the user; suppress.
  if (live === 'running' && tn.status === 'active') return null
  if (live === 'stopped' && tn.status === 'stopped') return null
  return live
}
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
          <TableHead>{{ t('tunnel.field.expire') }}</TableHead>
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
          <TableCell>
            <Badge variant="outline">{{ tn.type }}<span v-if="tn.role">·{{ tn.role }}</span></Badge>
          </TableCell>
          <TableCell class="font-mono text-xs">
            {{ tn.local_ip }}:{{ tn.local_port }} → {{ tn.remote_port || tn.subdomain || tn.custom_domains || '—' }}
          </TableCell>
          <TableCell>
            <div class="flex items-center gap-2 flex-wrap">
              <span v-if="!tn.expire_at" class="text-xs text-muted-foreground">—</span>
              <Badge v-else-if="remainingForRow(tn) === 'expired'" variant="outline">
                {{ t('tunnel.expire.expired') }}
              </Badge>
              <Badge
                v-else
                :variant="rowIsExpiringSoon(tn) ? 'destructive' : 'secondary'"
                class="font-mono"
              >
                <Clock class="size-3 mr-1" />
                {{ t('tunnel.expire.remaining', { value: remainingForRow(tn) }) }}
              </Badge>
              <DropdownMenu v-if="auth.isAdmin && tn.expire_at">
                <DropdownMenuTrigger as-child>
                  <Button size="icon" variant="ghost" class="size-7">
                    <AlarmClockPlus class="size-3.5" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" class="min-w-[8rem]">
                  <DropdownMenuItem
                    v-for="p in RENEW_PRESETS"
                    :key="p.seconds"
                    @select="renew(tn, p.seconds)"
                  >
                    <Clock class="size-3.5" />
                    <span>{{ t(p.labelKey) }}</span>
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem @select="renew(tn, 0)">
                    <InfinityIcon class="size-3.5" />
                    <span>{{ t('tunnel.renew.permanent') }}</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </TableCell>
          <TableCell>
            <div class="flex items-center gap-2">
              <Badge :variant="statusVariant[tn.status]">{{ t(`tunnel.status.${tn.status}`) }}</Badge>
              <Badge v-if="liveStateLabel(tn)" variant="outline" class="font-mono text-[10px]">
                {{ t('tunnel.live.' + liveStateLabel(tn)!) }}
              </Badge>
            </div>
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
      <DialogContent class="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('tunnel.edit') : t('tunnel.add') }}</DialogTitle>
        </DialogHeader>

        <div class="flex flex-col gap-5">
          <!-- Basic identity ---------------------------------------- -->
          <section class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.endpoint') }}</Label>
              <Select
                :model-value="form.endpoint_id ? String(form.endpoint_id) : ''"
                @update:model-value="(v: any) => form.endpoint_id = v ? Number(v) : null"
              >
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
                  <SelectItem value="tcpmux">tcpmux</SelectItem>
                  <SelectItem value="stcp">stcp</SelectItem>
                  <SelectItem value="xtcp">xtcp</SelectItem>
                  <SelectItem value="sudp">sudp</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div v-if="isSecret" class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.role.label') }}</Label>
              <Select :model-value="form.role || 'server'" @update:model-value="(v: any) => form.role = v as FormState['role']">
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="server">{{ t('tunnel.role.server') }}</SelectItem>
                  <SelectItem value="visitor">{{ t('tunnel.role.visitor') }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </section>

          <!-- Local target (always shown, both server and visitor) - -->
          <section class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.local_ip') }}</Label>
              <Input v-model="form.local_ip" placeholder="127.0.0.1" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.local_port') }}</Label>
              <Input v-model="form.local_port" type="number" min="0" max="65535" />
            </div>
          </section>

          <!-- Proxy-side fields: tcp/udp expose remote_port -->
          <section v-if="!isVisitor && (form.type === 'tcp' || form.type === 'udp')" class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.remote_port') }}</Label>
              <Input v-model="form.remote_port" type="number" min="0" max="65535" />
            </div>
          </section>

          <!-- HTTP / HTTPS / tcpmux fields -->
          <section v-if="!isVisitor && isHttp" class="flex flex-col gap-3">
            <div class="grid grid-cols-2 gap-3">
              <div class="flex flex-col gap-1.5">
                <Label>{{ t('tunnel.field.subdomain') }}</Label>
                <Input v-model="form.subdomain" placeholder="rdp" />
              </div>
              <div class="flex flex-col gap-1.5">
                <Label>{{ t('tunnel.field.host_header_rewrite') }}</Label>
                <Input v-model="form.host_header_rewrite" placeholder="" />
              </div>
              <div class="flex flex-col gap-1.5 col-span-2">
                <Label>{{ t('tunnel.field.custom_domains') }}</Label>
                <Input v-model="form.custom_domains" placeholder="rdp.example.com" />
              </div>
              <div v-if="form.type === 'http'" class="flex flex-col gap-1.5 col-span-2">
                <Label>{{ t('tunnel.field.locations') }}</Label>
                <Input v-model="form.locations" placeholder="/api,/healthz" />
              </div>
              <div v-if="form.type === 'http'" class="flex flex-col gap-1.5">
                <Label>{{ t('tunnel.field.http_user') }}</Label>
                <Input v-model="form.http_user" />
              </div>
              <div v-if="form.type === 'http'" class="flex flex-col gap-1.5">
                <Label>{{ t('tunnel.field.http_password') }}</Label>
                <Input
                  v-model="form.http_password"
                  type="password"
                  :placeholder="editing ? t('tunnel.field.http_password_keep') : ''"
                />
              </div>
            </div>
          </section>

          <!-- STCP / XTCP / SUDP — server side -->
          <section v-if="!isVisitor && isSecret" class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.sk') }}</Label>
              <Input
                v-model="form.sk"
                type="password"
                :placeholder="editing ? t('tunnel.field.sk_keep') : ''"
              />
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.allow_users') }}</Label>
              <Input v-model="form.allow_users" placeholder="alice,bob | *" />
            </div>
          </section>

          <!-- Visitor side -->
          <section v-if="isVisitor && isSecret" class="grid grid-cols-2 gap-3">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.server_name') }}</Label>
              <Input v-model="form.server_name" placeholder="exposed-tunnel-name" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.sk') }}</Label>
              <Input
                v-model="form.sk"
                type="password"
                :placeholder="editing ? t('tunnel.field.sk_keep') : ''"
              />
            </div>
          </section>

          <!-- Lifecycle -->
          <section class="flex flex-col gap-2 rounded-md border p-3">
            <div class="flex items-center gap-2">
              <Clock class="size-4 text-muted-foreground" />
              <Label class="m-0">{{ t('tunnel.expire.label') }}</Label>
            </div>
            <div class="flex items-center gap-2 flex-wrap">
              <Input v-model="form.expire_local" type="datetime-local" class="w-auto" />
              <Button type="button" variant="outline" size="sm" @click="setExpirePreset('hour', 2)">
                {{ t('tunnel.expire.preset_2h') }}
              </Button>
              <Button type="button" variant="outline" size="sm" @click="setExpirePreset('day', 1)">
                {{ t('tunnel.expire.preset_1d') }}
              </Button>
              <Button type="button" variant="outline" size="sm" @click="setExpirePreset('day', 7)">
                {{ t('tunnel.expire.preset_7d') }}
              </Button>
              <Button type="button" variant="ghost" size="sm" @click="clearExpire">
                {{ t('tunnel.expire.forever') }}
              </Button>
            </div>
            <p class="text-xs text-muted-foreground">
              <template v-if="!form.expire_local">{{ t('tunnel.expire.hint') }}</template>
              <template v-else-if="previewRemaining === 'expired'">{{ t('tunnel.validation.expire_in_past') }}</template>
              <template v-else>{{ t('tunnel.expire.remaining', { value: previewRemaining }) }}</template>
            </p>
          </section>

          <!-- Advanced toggle -->
          <button
            type="button"
            class="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground self-start"
            @click="showAdvanced = !showAdvanced"
          >
            <component :is="showAdvanced ? ChevronDown : ChevronRight" class="size-4" />
            <span>{{ showAdvanced ? t('tunnel.advanced_hide') : t('tunnel.advanced') }}</span>
          </button>

          <section v-if="showAdvanced" class="grid grid-cols-2 gap-3 rounded-md border bg-muted/30 p-3">
            <div class="flex items-center gap-2">
              <Switch v-model:checked="form.encryption" />
              <Label class="cursor-pointer m-0" @click="form.encryption = !form.encryption">{{ t('tunnel.field.encryption') }}</Label>
            </div>
            <div class="flex items-center gap-2">
              <Switch v-model:checked="form.compression" />
              <Label class="cursor-pointer m-0" @click="form.compression = !form.compression">{{ t('tunnel.field.compression') }}</Label>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.bandwidth_limit') }}</Label>
              <Input v-model="form.bandwidth_limit" :placeholder="t('tunnel.field.bandwidth_limit_hint')" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.group') }}</Label>
              <Input v-model="form.group" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.group_key') }}</Label>
              <Input v-model="form.group_key" type="password" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.health_check_type') }}</Label>
              <Select v-model="form.health_check_type">
                <SelectTrigger><SelectValue placeholder="—" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="">—</SelectItem>
                  <SelectItem value="tcp">tcp</SelectItem>
                  <SelectItem value="http">http</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div v-if="form.health_check_type === 'http'" class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.health_check_url') }}</Label>
              <Input v-model="form.health_check_url" placeholder="/healthz" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('tunnel.field.plugin') }}</Label>
              <Input v-model="form.plugin" placeholder="static_file / unix_domain_socket / …" />
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.plugin_config') }}</Label>
              <Input v-model="form.plugin_config" placeholder="local_path=/srv,strip_prefix=static" />
            </div>
            <div class="flex items-center gap-4 col-span-2 mt-1">
              <div class="flex items-center gap-2">
                <Switch v-model:checked="form.enabled" />
                <Label class="cursor-pointer m-0" @click="form.enabled = !form.enabled">{{ t('tunnel.field.enabled') }}</Label>
              </div>
              <div class="flex items-center gap-2">
                <Switch v-model:checked="form.auto_start" />
                <Label class="cursor-pointer m-0" @click="form.auto_start = !form.auto_start">{{ t('tunnel.field.auto_start') }}</Label>
              </div>
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
