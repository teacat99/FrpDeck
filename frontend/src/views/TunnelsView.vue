<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Plus, RefreshCw, Pencil, Trash2, Play, Square,
  ChevronDown, ChevronRight, Clock, AlarmClockPlus, Infinity as InfinityIcon,
  Stethoscope, CircleCheck, CircleAlert, CircleX, MinusCircle, Loader2,
  ServerCog, Copy, ExternalLink, LayoutTemplate, Upload, AlertTriangle,
  // Template-icon set: each icon name in a YAML template must exist
  // in this whitelist; render falls back to LayoutTemplate otherwise.
  Globe, Lock, Monitor, TerminalSquare, Database, Network, Shield,
  ArrowRightLeft, Folder,
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
import PluginConfigEditor from '@/components/PluginConfigEditor.vue'
import { Message } from '@/lib/toast'
import {
  listTunnels, createTunnel, updateTunnel, deleteTunnel,
  startTunnel, stopTunnel, renewTunnel, diagnoseTunnel,
  frpsAdvice, listTunnelTemplates,
  importTunnelsPreview, importTunnelsCommit,
  type DiagReport, type DiagStatus,
  type FrpsAdvice, type AdviceItem, type AdviceSeverity,
  type TunnelTemplate,
  type ImportPlan, type ImportCommitItem, type ImportCommitTunnel,
  type ImportConflictStrategy,
} from '@/api/tunnels'
import { listEndpoints } from '@/api/endpoints'
import type { Endpoint, Tunnel, TunnelStatus, TunnelWrite } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import { addRelative, formatRemaining, toIsoOrNull, toLocalInput } from '@/lib/expire'
import { nativeIsAndroid } from '@/composables/useNativeBridge'

const { t } = useI18n()
const auth = useAuthStore()
const realtime = useRealtimeStore()

// Android shell detection — drives the "this tunnel takes over device
// traffic" warning badge on `visitor + socks5` rows. Browser / desktop
// builds short-circuit to false because `window.frpdeck` is absent.
const isAndroidShell = computed(() => nativeIsAndroid())

function tunnelTakesOverDevice(tn: Tunnel): boolean {
  if (tn.role !== 'visitor') return false
  if ((tn.plugin || '').toLowerCase() === 'socks5') return true
  return /socks/i.test(tn.name || '')
}

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
  appliedTemplateId.value = ''
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
    let saved: Tunnel
    if (editing.value) {
      saved = await updateTunnel(editing.value.id, payload)
      Message.success(t('common.updated'))
    } else {
      if (appliedTemplateId.value) {
        payload.template_id = appliedTemplateId.value
      }
      saved = await createTunnel(payload)
      appliedTemplateId.value = ''
      Message.success(t('common.created'))
    }
    dialogOpen.value = false
    await reload()
    // Per P5-D auto-diagnose contract: kick off the four-step probe in
    // the background and surface the panel only if anything is wrong.
    // Manual rerun is always available via the Stethoscope action.
    runDiag(saved, { open: false }).then(() => {
      if (diagReport.value && diagReport.value.overall !== 'ok' && diagReport.value.overall !== 'skipped') {
        diagOpen.value = true
      }
    })
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

// ----- Connectivity self-check (P5-D) ---------------------------------
// One panel shared by all rows. Open it on demand (Stethoscope icon)
// or automatically after a successful save so the user immediately sees
// whether the tunnel they just configured is going to work.
const diagOpen = ref(false)
const diagReport = ref<DiagReport | null>(null)
const diagRunning = ref(false)
const diagTunnel = ref<Tunnel | null>(null)
const diagError = ref('')

async function runDiag(tn: Tunnel, opts: { open?: boolean } = {}) {
  diagTunnel.value = tn
  diagRunning.value = true
  diagError.value = ''
  if (opts.open) {
    diagOpen.value = true
    diagReport.value = null
  }
  try {
    diagReport.value = await diagnoseTunnel(tn.id)
  } catch (e: any) {
    diagError.value = e?.response?.data?.error ?? t('msg.opFailed')
    diagReport.value = null
  } finally {
    diagRunning.value = false
  }
}

const DIAG_STATUS_VARIANT: Record<DiagStatus, 'default' | 'destructive' | 'secondary' | 'outline'> = {
  ok: 'default',
  warn: 'secondary',
  fail: 'destructive',
  skipped: 'outline',
}

// ----- frps configuration helper (P5-B) -------------------------------
// Reverse-engineers the frps.toml requirements for a given tunnel and
// surfaces them in a dedicated dialog. plan.md §7.1: lower the frp
// learning curve by enumerating exactly which knobs the current tunnel
// implies, with copy-paste TOML snippet and per-field doc deeplinks.
const adviceOpen = ref(false)
const adviceTunnel = ref<Tunnel | null>(null)
const adviceLoading = ref(false)
const adviceData = ref<FrpsAdvice | null>(null)
const adviceError = ref('')

async function openAdvice(tn: Tunnel) {
  adviceTunnel.value = tn
  adviceOpen.value = true
  adviceLoading.value = true
  adviceData.value = null
  adviceError.value = ''
  try {
    adviceData.value = await frpsAdvice(tn.id)
  } catch (e: any) {
    adviceError.value = e?.response?.data?.error ?? t('msg.opFailed')
  } finally {
    adviceLoading.value = false
  }
}

async function copyAdviceSnippet() {
  if (!adviceData.value) return
  try {
    await navigator.clipboard.writeText(adviceData.value.toml_snippet)
    Message.success(t('tunnel.advice.copied'))
  } catch {
    Message.error(t('msg.opFailed'))
  }
}

const ADVICE_SEVERITY_VARIANT: Record<AdviceSeverity, 'default' | 'destructive' | 'secondary' | 'outline'> = {
  required: 'destructive',
  recommended: 'default',
  warn: 'secondary',
  info: 'outline',
}

// adviceItems is just `adviceData?.items ?? []` but threaded through a
// computed so the template stays terse and we keep the frontend
// resilient to a future backend that omits the array on empty.
const adviceItems = computed<AdviceItem[]>(() => adviceData.value?.items ?? [])

// ----- Template wizard (P5-C) -----------------------------------------
// plan.md §7.2: 10 scenario templates. Backend returns the YAML-driven
// list; this view caches them lazily on first wizard open to avoid an
// extra round-trip on initial page load.
const templateWizardOpen = ref(false)
const templates = ref<TunnelTemplate[]>([])
const templatesLoaded = ref(false)
const templatesLoading = ref(false)

// TEMPLATE_ICONS whitelists the lucide icons we ship. Untrusted YAML
// could otherwise let us render arbitrary symbols; even though the
// YAML is bundled we keep the indirection so a typo falls back to
// the generic LayoutTemplate icon instead of a runtime crash.
const TEMPLATE_ICONS: Record<string, unknown> = {
  Globe, Lock, Monitor, TerminalSquare, Database, Network, Shield,
  ArrowRightLeft, Folder, ServerCog, LayoutTemplate,
}

function templateIcon(name?: string) {
  if (!name) return LayoutTemplate
  return (TEMPLATE_ICONS[name] as typeof LayoutTemplate) ?? LayoutTemplate
}

async function openTemplateWizard() {
  templateWizardOpen.value = true
  if (templatesLoaded.value) return
  templatesLoading.value = true
  try {
    templates.value = await listTunnelTemplates()
    templatesLoaded.value = true
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  } finally {
    templatesLoading.value = false
  }
}

// applyTemplate seeds the create-tunnel form from the chosen
// template's defaults map and opens the existing edit dialog. We
// reuse openCreate's "single endpoint → auto-select" convenience so
// the wizard collapses to a one-click flow when there is exactly
// one frps.
//
// The `expire_in_seconds` knob (used by the temporary-DB-share
// template) is converted to an absolute datetime-local string here,
// because the form input only understands the local-tz form.
function applyTemplate(tpl: TunnelTemplate) {
  templateWizardOpen.value = false
  editing.value = null
  const fresh = emptyForm()
  // Copy whitelisted keys; ignore unknown ones so future template
  // additions never break older clients.
  const knownKeys = Object.keys(fresh) as Array<keyof FormState>
  const d = tpl.defaults || {}
  for (const k of knownKeys) {
    if (k in d) {
      // String/number coercion is intentional: <input type=number>
      // bound to a string is fine, but switching to number breaks
      // the empty-state contract used elsewhere in this view.
      ;(fresh as any)[k] = d[k as string]
    }
  }
  // `template_id` is not a FormState field but the create payload
  // accepts it; we merge it back in the submit() code path.
  fresh.name = fresh.name || tpl.id
  Object.assign(form, fresh)
  if (endpoints.value.length === 1) {
    form.endpoint_id = endpoints.value[0].id
  }
  // Translate expire_in_seconds → expire_local. Storing seconds
  // (rather than an absolute timestamp) in the YAML keeps templates
  // deterministic across builds.
  const exp = (d as any).expire_in_seconds
  if (typeof exp === 'number' && exp > 0) {
    const at = new Date(Date.now() + exp * 1000)
    form.expire_local = toLocalInput(at.toISOString()) || ''
  }
  appliedTemplateId.value = tpl.id
  showAdvanced.value = false
  dialogOpen.value = true
}

// appliedTemplateId is sent on the create payload so the backend
// records which template seeded the tunnel; useful for analytics
// later ("how many users actually use the templates we ship?").
const appliedTemplateId = ref<string>('')

// ----- Import wizard (P5-E) -------------------------------------------
// plan.md §15 requires the import flow to be dry-runnable so the
// operator can review the parsed plan before we touch the database.
// We keep two modes: `paste` (textarea) and `upload` (input[type=file])
// — the latter is just a thin wrapper around the former.
const importOpen = ref(false)
const importContent = ref('')
const importFilename = ref('')
const importLoading = ref(false)
const importPlan = ref<ImportPlan | null>(null)
const importEndpointId = ref<number | null>(null)
const importSelected = ref<Record<number, boolean>>({})
const importOnConflict = ref<Record<number, ImportConflictStrategy>>({})
const importDefaultOnConflict = ref<ImportConflictStrategy>('rename')
const importCommitting = ref(false)
const importResults = ref<ImportCommitItem[] | null>(null)

function openImport() {
  importOpen.value = true
  importContent.value = ''
  importFilename.value = ''
  importPlan.value = null
  importEndpointId.value = endpoints.value.length === 1 ? endpoints.value[0].id : null
  importSelected.value = {}
  importOnConflict.value = {}
  importDefaultOnConflict.value = 'rename'
  importResults.value = null
}

async function handleImportFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importFilename.value = file.name
  importContent.value = await file.text()
  // Auto-trigger preview after upload — operators expect "drop the file
  // and immediately see what would happen".
  await runImportPreview()
  input.value = ''
}

async function runImportPreview() {
  if (!importContent.value.trim()) {
    Message.error(t('tunnel.import.errors.empty'))
    return
  }
  importLoading.value = true
  importResults.value = null
  try {
    const plan = await importTunnelsPreview(
      importContent.value,
      importFilename.value,
      importEndpointId.value ?? undefined,
    )
    importPlan.value = plan
    // Default to selecting every tunnel; the operator can deselect any
    // they don't want.
    const selected: Record<number, boolean> = {}
    const onConflict: Record<number, ImportConflictStrategy> = {}
    plan.tunnels.forEach((tn, idx) => {
      selected[idx] = true
      if (tn.conflict) {
        onConflict[idx] = importDefaultOnConflict.value
      }
    })
    importSelected.value = selected
    importOnConflict.value = onConflict
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
    importPlan.value = null
  } finally {
    importLoading.value = false
  }
}

// Re-runs the preview when the operator picks a target endpoint so the
// backend can stamp `conflict` flags against the actual destination.
async function onImportEndpointChange(value: any) {
  importEndpointId.value = value ? Number(value) : null
  if (importPlan.value) {
    await runImportPreview()
  }
}

async function commitImport() {
  if (!importPlan.value) return
  if (!importEndpointId.value) {
    Message.error(t('tunnel.import.errors.endpoint_required'))
    return
  }
  const selectedIdx = importPlan.value.tunnels
    .map((_, idx) => idx)
    .filter(idx => importSelected.value[idx] !== false)
  if (!selectedIdx.length) {
    Message.error(t('tunnel.import.errors.no_tunnels_selected'))
    return
  }
  const tunnels: ImportCommitTunnel[] = selectedIdx.map(idx => {
    const draft = importPlan.value!.tunnels[idx]
    const out: ImportCommitTunnel = { ...draft }
    if (draft.conflict) {
      out.on_conflict = importOnConflict.value[idx] ?? importDefaultOnConflict.value
    }
    return out
  })
  importCommitting.value = true
  try {
    const items = await importTunnelsCommit(
      importEndpointId.value,
      tunnels,
      importDefaultOnConflict.value,
    )
    // Map results back to the index space used by the preview list so
    // the per-row badge keeps lining up after rename/skip resolution.
    const aligned: ImportCommitItem[] = []
    selectedIdx.forEach((origIdx, i) => {
      aligned[origIdx] = items[i]
    })
    importResults.value = aligned
    const ok = items.filter(i => i.id).length
    const skipped = items.filter(i => i.skipped).length
    const fail = items.filter(i => i.error).length
    if (fail === 0 && skipped === 0) {
      Message.success(t('tunnel.import.success', { ok }))
    } else if (fail === 0 && skipped > 0) {
      Message.warning(t('tunnel.import.partial_with_skip', { ok, skipped }))
    } else if (ok === 0) {
      Message.error(t('tunnel.import.partial_fail', { ok, fail }))
    } else {
      Message.warning(t('tunnel.import.partial', { ok, fail }))
    }
    if (ok > 0) {
      await reload()
    }
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.opFailed'))
  } finally {
    importCommitting.value = false
  }
}

const importSelectedCount = computed(() => {
  if (!importPlan.value) return 0
  return importPlan.value.tunnels.reduce(
    (acc, _, idx) => acc + (importSelected.value[idx] !== false ? 1 : 0),
    0,
  )
})

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
        <Button
          v-if="auth.isAdmin"
          variant="outline"
          :disabled="endpoints.length === 0"
          @click="openTemplateWizard"
        >
          <LayoutTemplate class="size-4" />
          <span>{{ t('template.wizard.action') }}</span>
        </Button>
        <Button
          v-if="auth.isAdmin"
          variant="outline"
          :disabled="endpoints.length === 0"
          @click="openImport"
        >
          <Upload class="size-4" />
          <span>{{ t('tunnel.import.action') }}</span>
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
            <div class="flex items-center gap-1 flex-wrap">
              <Badge variant="outline">{{ tn.type }}<span v-if="tn.role">·{{ tn.role }}</span></Badge>
              <Badge
                v-if="isAndroidShell && tunnelTakesOverDevice(tn)"
                variant="destructive"
                class="text-[10px]"
                :title="t('tunnel.android_vpn_takeover_hint')"
              >
                {{ t('tunnel.android_vpn_takeover') }}
              </Badge>
            </div>
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
            <Button
              v-if="auth.isAdmin"
              size="icon"
              variant="ghost"
              :title="t('tunnel.diag.action')"
              @click="runDiag(tn, { open: true })"
            >
              <Stethoscope class="size-4" />
            </Button>
            <Button
              v-if="auth.isAdmin"
              size="icon"
              variant="ghost"
              :title="t('tunnel.advice.action')"
              @click="openAdvice(tn)"
            >
              <ServerCog class="size-4" />
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
              <Select v-model="form.plugin">
                <SelectTrigger><SelectValue placeholder="—" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="">—</SelectItem>
                  <SelectItem value="static_file">static_file</SelectItem>
                  <SelectItem value="unix_domain_socket">unix_domain_socket</SelectItem>
                  <SelectItem value="http_proxy">http_proxy</SelectItem>
                  <SelectItem value="socks5">socks5</SelectItem>
                  <SelectItem value="https2http">https2http</SelectItem>
                  <SelectItem value="https2https">https2https</SelectItem>
                  <SelectItem value="http2https">http2https</SelectItem>
                  <SelectItem value="tls2raw">tls2raw</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="flex flex-col gap-1.5 col-span-2">
              <Label>{{ t('tunnel.field.plugin_config') }}</Label>
              <PluginConfigEditor v-model="form.plugin_config" :plugin="form.plugin" />
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

    <!-- Connectivity self-check panel (P5-D) ------------------------- -->
    <Dialog v-model:open="diagOpen">
      <DialogContent class="max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {{ t('tunnel.diag.title') }}
            <span v-if="diagTunnel" class="text-sm text-muted-foreground font-normal">— {{ diagTunnel.name }}</span>
          </DialogTitle>
        </DialogHeader>

        <div class="flex flex-col gap-3">
          <p class="text-sm text-muted-foreground">{{ t('tunnel.diag.subtitle') }}</p>

          <div v-if="diagError" class="text-sm text-destructive">{{ diagError }}</div>

          <div v-if="diagRunning && !diagReport" class="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 class="size-4 animate-spin" />
            <span>{{ t('tunnel.diag.running') }}</span>
          </div>

          <div v-if="diagReport" class="flex items-center gap-2">
            <span class="text-xs uppercase tracking-wider text-muted-foreground">{{ t('tunnel.diag.overall') }}</span>
            <Badge :variant="DIAG_STATUS_VARIANT[diagReport.overall]">
              {{ t('tunnel.diag.status.' + diagReport.overall) }}
            </Badge>
          </div>

          <ul v-if="diagReport" class="flex flex-col gap-2">
            <li v-for="c in diagReport.checks" :key="c.id" class="rounded-md border p-3 flex flex-col gap-1.5">
              <div class="flex items-center gap-2">
                <CircleCheck v-if="c.status === 'ok'" class="size-4 text-emerald-500" />
                <CircleAlert v-else-if="c.status === 'warn'" class="size-4 text-amber-500" />
                <CircleX v-else-if="c.status === 'fail'" class="size-4 text-destructive" />
                <MinusCircle v-else class="size-4 text-muted-foreground" />
                <span class="font-medium text-sm">{{ t('tunnel.diag.check.' + c.id) }}</span>
                <Badge :variant="DIAG_STATUS_VARIANT[c.status]" class="ml-auto text-[10px]">
                  {{ t('tunnel.diag.status.' + c.status) }}
                </Badge>
                <span class="text-[10px] text-muted-foreground font-mono">{{ c.duration_ms }}ms</span>
              </div>
              <p class="text-xs text-muted-foreground font-mono break-all">{{ c.message }}</p>
              <p v-if="c.hint" class="text-xs text-amber-600 dark:text-amber-400">→ {{ c.hint }}</p>
            </li>
          </ul>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="diagOpen = false">{{ t('common.close') }}</Button>
          <Button
            :disabled="diagRunning || !diagTunnel"
            @click="diagTunnel && runDiag(diagTunnel)"
          >
            <RefreshCw class="size-4" :class="{ 'animate-spin': diagRunning }" />
            <span>{{ t('tunnel.diag.rerun') }}</span>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Template wizard (P5-C) -->
    <Dialog v-model:open="templateWizardOpen">
      <DialogContent class="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{{ t('template.wizard.title') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-3 max-h-[65vh] overflow-y-auto">
          <p class="text-sm text-muted-foreground">{{ t('template.wizard.subtitle') }}</p>

          <div v-if="templatesLoading" class="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 class="size-4 animate-spin" />
            <span>{{ t('template.wizard.loading') }}</span>
          </div>

          <div v-if="templatesLoaded && !templates.length" class="text-sm text-muted-foreground">
            {{ t('template.wizard.empty') }}
          </div>

          <div v-if="templates.length" class="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <button
              v-for="tpl in templates"
              :key="tpl.id"
              class="text-left rounded-md border p-3 hover:border-primary hover:bg-accent/30 transition-colors flex flex-col gap-1.5"
              type="button"
              @click="applyTemplate(tpl)"
            >
              <div class="flex items-center gap-2">
                <component :is="templateIcon(tpl.icon)" class="size-4 shrink-0" />
                <span class="font-medium text-sm">{{ t('template.' + tpl.id + '.name') }}</span>
              </div>
              <div class="text-xs text-muted-foreground leading-relaxed">
                {{ t('template.' + tpl.id + '.desc') }}
              </div>
              <div class="text-[10px] text-muted-foreground/80 italic">
                {{ t('template.audience') }}: {{ t('template.' + tpl.id + '.audience') }}
              </div>
              <ul v-if="tpl.prereq_keys && tpl.prereq_keys.length" class="text-[11px] text-muted-foreground list-disc list-inside mt-1">
                <li v-for="k in tpl.prereq_keys" :key="k">{{ t('template.' + k) }}</li>
              </ul>
            </button>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="templateWizardOpen = false">{{ t('common.cancel') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- frps configuration helper (P5-B) -->
    <Dialog v-model:open="adviceOpen">
      <DialogContent class="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{{ t('tunnel.advice.title') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-4 max-h-[60vh] overflow-y-auto">
          <p v-if="adviceTunnel" class="text-sm text-muted-foreground">
            {{ t('tunnel.advice.subtitle', { name: adviceTunnel.name }) }}
          </p>

          <div v-if="adviceLoading" class="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 class="size-4 animate-spin" />
            <span>{{ t('tunnel.advice.loading') }}</span>
          </div>

          <div v-if="adviceError" class="text-sm text-destructive">{{ adviceError }}</div>

          <div v-if="adviceData">
            <ul v-if="adviceItems.length" class="flex flex-col gap-2">
              <li
                v-for="(it, idx) in adviceItems"
                :key="idx"
                class="rounded-md border p-3 flex flex-col gap-1.5"
              >
                <div class="flex items-center gap-2 flex-wrap">
                  <Badge :variant="ADVICE_SEVERITY_VARIANT[it.severity]">
                    {{ t('tunnel.advice.severity.' + it.severity) }}
                  </Badge>
                  <span class="font-medium text-sm">{{ it.title }}</span>
                </div>
                <div v-if="it.field" class="font-mono text-xs text-muted-foreground">
                  {{ it.field }}<span v-if="it.value"> = {{ it.value }}</span>
                </div>
                <div v-if="it.detail" class="text-xs text-muted-foreground leading-relaxed">{{ it.detail }}</div>
                <a
                  v-if="it.doc_url"
                  :href="it.doc_url"
                  target="_blank"
                  rel="noreferrer noopener"
                  class="text-xs text-primary inline-flex items-center gap-1 hover:underline"
                >
                  <ExternalLink class="size-3" />
                  <span>{{ t('tunnel.advice.docs') }}</span>
                </a>
              </li>
            </ul>
            <div v-else class="text-sm text-muted-foreground">
              {{ t('tunnel.advice.empty') }}
            </div>

            <div v-if="adviceData.caveats && adviceData.caveats.length" class="mt-3 rounded-md border border-amber-300/50 bg-amber-50/40 dark:bg-amber-950/20 p-3">
              <div class="text-xs font-medium mb-1.5">{{ t('tunnel.advice.caveats') }}</div>
              <ul class="list-disc list-inside text-xs text-muted-foreground flex flex-col gap-1">
                <li v-for="(c, i) in adviceData.caveats" :key="i">{{ c }}</li>
              </ul>
            </div>

            <div v-if="adviceData.toml_snippet" class="mt-3">
              <div class="flex items-center justify-between mb-1.5">
                <div class="text-xs font-medium">{{ t('tunnel.advice.snippet') }}</div>
                <Button size="sm" variant="ghost" @click="copyAdviceSnippet">
                  <Copy class="size-3.5" />
                  <span>{{ t('tunnel.advice.copy') }}</span>
                </Button>
              </div>
              <pre class="rounded-md bg-muted p-3 text-xs font-mono overflow-x-auto whitespace-pre">{{ adviceData.toml_snippet }}</pre>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="adviceOpen = false">{{ t('common.close') }}</Button>
          <Button
            :disabled="adviceLoading || !adviceTunnel"
            @click="adviceTunnel && openAdvice(adviceTunnel)"
          >
            <RefreshCw class="size-4" :class="{ 'animate-spin': adviceLoading }" />
            <span>{{ t('common.refresh') }}</span>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Import frpc.toml (P5-E) -->
    <Dialog v-model:open="importOpen">
      <DialogContent class="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{{ t('tunnel.import.title') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-4 max-h-[70vh] overflow-y-auto">
          <p class="text-sm text-muted-foreground">{{ t('tunnel.import.subtitle') }}</p>

          <!-- Step 1: paste/upload -->
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2 flex-wrap">
              <Label class="text-xs">{{ t('tunnel.import.upload_label') }}</Label>
              <input
                type="file"
                accept=".toml,.yaml,.yml,.json"
                class="text-xs"
                @change="handleImportFile"
              />
              <span v-if="importFilename" class="text-xs text-muted-foreground">{{ importFilename }}</span>
            </div>
            <Label class="text-xs">{{ t('tunnel.import.paste_label') }}</Label>
            <textarea
              v-model="importContent"
              :placeholder="t('tunnel.import.placeholder')"
              class="min-h-[140px] w-full rounded-md border bg-background p-2 text-xs font-mono"
              spellcheck="false"
            />
            <div class="flex items-center gap-2">
              <Button
                size="sm"
                :disabled="importLoading || !importContent.trim()"
                @click="runImportPreview"
              >
                <Loader2 v-if="importLoading" class="size-4 animate-spin" />
                <span>{{ t('tunnel.import.preview') }}</span>
              </Button>
              <span v-if="importPlan" class="text-xs text-muted-foreground">
                {{ t('tunnel.import.parsed_format', { format: importPlan.format }) }}
              </span>
            </div>
          </div>

          <!-- Step 2: review plan -->
          <div v-if="importPlan" class="flex flex-col gap-3">
            <!-- File-level warnings -->
            <div
              v-if="importPlan.warnings && importPlan.warnings.length"
              class="rounded-md border border-amber-300/50 bg-amber-50/40 dark:bg-amber-950/20 p-3"
            >
              <div class="flex items-center gap-1.5 text-xs font-medium mb-1.5">
                <AlertTriangle class="size-3.5" />
                <span>{{ t('tunnel.import.file_warnings') }}</span>
              </div>
              <ul class="list-disc list-inside text-xs text-muted-foreground flex flex-col gap-1">
                <li v-for="(w, i) in importPlan.warnings" :key="i">{{ w }}</li>
              </ul>
            </div>

            <!-- Endpoint summary + target picker -->
            <div v-if="importPlan.endpoint" class="rounded-md border p-3 flex flex-col gap-2">
              <div class="text-xs font-medium">{{ t('tunnel.import.endpoint_section') }}</div>
              <div class="text-xs text-muted-foreground font-mono">
                {{ importPlan.endpoint.addr }}:{{ importPlan.endpoint.port }}
                <span v-if="importPlan.endpoint.protocol"> ({{ importPlan.endpoint.protocol }})</span>
                <span v-if="importPlan.endpoint.user"> · user={{ importPlan.endpoint.user }}</span>
                <span v-if="importPlan.endpoint.tls_enable"> · tls</span>
                <span v-if="importPlan.endpoint.token"> · token=***</span>
              </div>
              <div class="flex items-center gap-2 flex-wrap">
                <Label class="text-xs">{{ t('tunnel.import.target_endpoint') }}</Label>
                <Select
                  :model-value="importEndpointId ? String(importEndpointId) : ''"
                  @update:model-value="onImportEndpointChange"
                >
                  <SelectTrigger class="h-8 text-xs w-[260px]">
                    <SelectValue :placeholder="t('tunnel.import.target_endpoint_placeholder')" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="ep in endpoints"
                      :key="ep.id"
                      :value="String(ep.id)"
                    >
                      {{ ep.name }} — {{ ep.addr }}:{{ ep.port }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="text-[11px] text-muted-foreground">
                {{ t('tunnel.import.target_endpoint_hint') }}
              </div>
              <div class="flex items-center gap-2 flex-wrap">
                <Label class="text-xs">{{ t('tunnel.import.default_conflict') }}</Label>
                <Select
                  :model-value="importDefaultOnConflict"
                  @update:model-value="(v: any) => importDefaultOnConflict = v as ImportConflictStrategy"
                >
                  <SelectTrigger class="h-8 text-xs w-[200px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="error">{{ t('tunnel.import.conflict.error') }}</SelectItem>
                    <SelectItem value="rename">{{ t('tunnel.import.conflict.rename') }}</SelectItem>
                    <SelectItem value="skip">{{ t('tunnel.import.conflict.skip') }}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <!-- Tunnels list -->
            <div class="rounded-md border p-3 flex flex-col gap-2">
              <div class="text-xs font-medium">
                {{ t('tunnel.import.tunnels_section', { count: importPlan.tunnels.length, selected: importSelectedCount }) }}
              </div>
              <div v-if="!importPlan.tunnels.length" class="text-xs text-muted-foreground">
                {{ t('tunnel.import.tunnels_empty') }}
              </div>
              <ul v-else class="flex flex-col gap-1.5">
                <li
                  v-for="(tn, idx) in importPlan.tunnels"
                  :key="idx"
                  class="rounded-md border p-2 flex items-start gap-2"
                >
                  <input
                    type="checkbox"
                    class="mt-1"
                    :checked="importSelected[idx] !== false"
                    @change="(e: Event) => importSelected[idx] = (e.target as HTMLInputElement).checked"
                  />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="font-medium text-sm truncate">{{ tn.name }}</span>
                      <Badge variant="outline">{{ tn.type }}</Badge>
                      <Badge v-if="tn.role === 'visitor'" variant="secondary">visitor</Badge>
                      <Badge v-if="tn.conflict" variant="destructive">
                        {{ t('tunnel.import.conflict.badge') }}
                      </Badge>
                      <Select
                        v-if="tn.conflict && !importResults"
                        :model-value="importOnConflict[idx] ?? importDefaultOnConflict"
                        @update:model-value="(v: any) => importOnConflict[idx] = v as ImportConflictStrategy"
                      >
                        <SelectTrigger class="h-6 text-[10px] w-[120px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="error">{{ t('tunnel.import.conflict.error') }}</SelectItem>
                          <SelectItem value="rename">{{ t('tunnel.import.conflict.rename') }}</SelectItem>
                          <SelectItem value="skip">{{ t('tunnel.import.conflict.skip') }}</SelectItem>
                        </SelectContent>
                      </Select>
                      <span
                        v-if="importResults && importResults[idx]"
                        class="text-[10px]"
                        :class="importResults[idx].error
                          ? 'text-destructive'
                          : importResults[idx].skipped
                            ? 'text-muted-foreground'
                            : 'text-green-600'"
                      >
                        <template v-if="importResults[idx].error">
                          {{ t('tunnel.import.row_failed') }}: {{ importResults[idx].error }}
                        </template>
                        <template v-else-if="importResults[idx].skipped">
                          {{ t('tunnel.import.row_skipped') }}
                        </template>
                        <template v-else-if="importResults[idx].renamed">
                          {{ t('tunnel.import.row_renamed', { name: importResults[idx].renamed }) }} (#{{ importResults[idx].id }})
                        </template>
                        <template v-else>
                          {{ t('tunnel.import.row_ok') }} (#{{ importResults[idx].id }})
                        </template>
                      </span>
                    </div>
                    <div class="text-[11px] text-muted-foreground font-mono mt-0.5">
                      <template v-if="tn.role !== 'visitor'">
                        {{ tn.local_ip }}:{{ tn.local_port }}
                        <template v-if="tn.remote_port"> → :{{ tn.remote_port }}</template>
                        <template v-if="tn.custom_domains"> @ {{ tn.custom_domains }}</template>
                        <template v-if="tn.subdomain"> @ {{ tn.subdomain }}.*</template>
                      </template>
                      <template v-else>
                        bind {{ tn.local_ip }}:{{ tn.local_port }} → {{ tn.server_name }}
                      </template>
                      <template v-if="tn.plugin"> · plugin={{ tn.plugin }}</template>
                    </div>
                    <ul
                      v-if="tn.warnings && tn.warnings.length"
                      class="list-disc list-inside text-[11px] text-amber-600 dark:text-amber-400 mt-1"
                    >
                      <li v-for="(w, i) in tn.warnings" :key="i">{{ w }}</li>
                    </ul>
                  </div>
                </li>
              </ul>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="importOpen = false">{{ t('common.close') }}</Button>
          <Button
            :disabled="!importPlan || !importPlan.tunnels.length || importCommitting || importSelectedCount === 0"
            @click="commitImport"
          >
            <Loader2 v-if="importCommitting" class="size-4 animate-spin" />
            <span>{{ t('tunnel.import.commit', { count: importSelectedCount }) }}</span>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
