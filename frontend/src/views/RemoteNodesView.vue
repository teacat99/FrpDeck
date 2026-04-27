<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, Trash2, ExternalLink, Copy, ShieldAlert, RotateCcw, Upload, KeyRound } from 'lucide-vue-next'
import QRCode from 'qrcode'
import jsQR from 'jsqr'

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
  Tabs, TabsList, TabsTrigger, TabsContent,
} from '@/components/ui/tabs'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import EmptyState from '@/components/EmptyState.vue'

import { Message } from '@/lib/toast'
import { useAuthStore } from '@/stores/auth'
import { listEndpoints } from '@/api/endpoints'
import type { Endpoint } from '@/api/types'
import {
  listRemoteNodes,
  createRemoteInvitation,
  redeemRemoteInvitation,
  revokeRemoteNode,
  revokeRemoteMgmtToken,
  refreshRemoteInvitation,
  type RemoteNode,
  type RemoteDirection,
  type RemoteNodeStatus,
} from '@/api/remote'

const { t } = useI18n()
const auth = useAuthStore()

const nodes = ref<RemoteNode[]>([])
const endpoints = ref<Endpoint[]>([])
const loading = ref(false)
const tab = ref<RemoteDirection>('managed_by_me')

const inviteOpen = ref(false)
const inviteSubmitting = ref(false)
const inviteForm = ref({
  endpoint_id: '' as string,
  node_name: '',
  ui_scheme: 'http' as 'http' | 'https',
})
const inviteResult = ref<null | {
  invitation: string
  expire_at: string
  driver_warning?: string
  qr: string
}>(null)

const redeemOpen = ref(false)
const redeemSubmitting = ref(false)
const redeemForm = ref({ invitation: '', node_name: '' })
const redeemSuccess = ref<null | { name: string; redeem_url: string; driver_warning?: string }>(null)

const passwordModeOk = computed(() => auth.mode === 'password')

async function reloadNodes() {
  if (!passwordModeOk.value) return
  loading.value = true
  try {
    nodes.value = await listRemoteNodes()
  } finally {
    loading.value = false
  }
}

async function reloadEndpoints() {
  if (!passwordModeOk.value) return
  endpoints.value = await listEndpoints()
}

const managedByMe = computed(() => nodes.value.filter(n => n.direction === 'managed_by_me'))
const managesMe = computed(() => nodes.value.filter(n => n.direction === 'manages_me'))

function endpointName(id: number): string {
  const ep = endpoints.value.find(e => e.id === id)
  return ep ? ep.name : `#${id}`
}

function statusVariant(status: RemoteNodeStatus): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (status) {
    case 'active': return 'default'
    case 'pending': return 'secondary'
    case 'offline': return 'outline'
    case 'revoked':
    case 'expired':
      return 'destructive'
    default: return 'outline'
  }
}

function fmtTime(iso?: string | null): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '-'
  return d.toLocaleString()
}

function openInvite() {
  inviteForm.value = {
    endpoint_id: endpoints.value[0]?.id ? String(endpoints.value[0].id) : '',
    node_name: '',
    ui_scheme: 'http',
  }
  inviteResult.value = null
  inviteOpen.value = true
}

async function submitInvite() {
  const epID = Number(inviteForm.value.endpoint_id) || 0
  if (!epID) {
    Message.warning(t('remote.invite.endpoint'))
    return
  }
  inviteSubmitting.value = true
  try {
    const resp = await createRemoteInvitation({
      endpoint_id: epID,
      node_name: inviteForm.value.node_name.trim() || undefined,
      ui_scheme: inviteForm.value.ui_scheme,
    })
    // Render the invitation as a QR code so a phone-side FrpDeck can
    // scan and paste in one motion.
    const qr = await QRCode.toDataURL(resp.invitation, {
      margin: 1,
      width: 256,
      errorCorrectionLevel: 'M',
    })
    inviteResult.value = {
      invitation: resp.invitation,
      expire_at: resp.expire_at,
      driver_warning: resp.driver_warning,
      qr,
    }
    await reloadNodes()
  } finally {
    inviteSubmitting.value = false
  }
}

async function copyInvite() {
  const inv = inviteResult.value?.invitation
  if (!inv) return
  try {
    await navigator.clipboard.writeText(inv)
    Message.success(t('remote.invite.copied'))
  } catch {
    Message.warning(t('remote.invite.copied'))
  }
}

function openRedeem() {
  redeemForm.value = { invitation: '', node_name: '' }
  redeemSuccess.value = null
  redeemOpen.value = true
}

const redeemUploadRef = ref<HTMLInputElement | null>(null)
const redeemDecoding = ref(false)

function triggerRedeemUpload() {
  redeemUploadRef.value?.click()
}

async function decodeQrFromImage(file: File): Promise<string | null> {
  // We render the file into a hidden canvas, sample its pixels, and pass
  // the ImageData to jsQR. Resizing the canvas to the natural image
  // dimensions keeps the decoder happy on high-DPI screenshots; jsQR
  // refuses oversized buffers (>2k x 2k worth of pixels).
  const url = URL.createObjectURL(file)
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const el = new Image()
      el.onload = () => resolve(el)
      el.onerror = () => reject(new Error('cannot load image'))
      el.src = url
    })
    const max = 1280
    const ratio = Math.min(1, max / Math.max(img.naturalWidth, img.naturalHeight))
    const w = Math.max(1, Math.round(img.naturalWidth * ratio))
    const h = Math.max(1, Math.round(img.naturalHeight * ratio))
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    const ctx = canvas.getContext('2d')
    if (!ctx) return null
    ctx.drawImage(img, 0, 0, w, h)
    const data = ctx.getImageData(0, 0, w, h)
    const result = jsQR(data.data, data.width, data.height, { inversionAttempts: 'attemptBoth' })
    return result?.data ?? null
  } finally {
    URL.revokeObjectURL(url)
  }
}

async function handleRedeemUpload(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  redeemDecoding.value = true
  try {
    const text = await decodeQrFromImage(file)
    if (!text) {
      Message.error(t('remote.redeem.qr_failed'))
      return
    }
    redeemForm.value.invitation = text
    Message.success(t('remote.redeem.qr_decoded'))
  } catch (err) {
    Message.error((err as Error)?.message ?? t('remote.redeem.qr_failed'))
  } finally {
    redeemDecoding.value = false
  }
}

async function submitRedeem() {
  if (!redeemForm.value.invitation.trim()) return
  redeemSubmitting.value = true
  try {
    const resp = await redeemRemoteInvitation({
      invitation: redeemForm.value.invitation.trim(),
      node_name: redeemForm.value.node_name.trim() || undefined,
    })
    redeemSuccess.value = {
      name: resp.node.name,
      redeem_url: resp.redeem_url,
      driver_warning: resp.driver_warning,
    }
    await reloadNodes()
  } finally {
    redeemSubmitting.value = false
  }
}

async function openRemote(node: RemoteNode) {
  if (node.direction !== 'managed_by_me' || !node.local_bind_port) {
    Message.warning(t('remote.open_unavailable'))
    return
  }
  // The redeem URL we cache server-side embeds the mgmt token. We
  // don't persist it on the client — but we can ask the server to
  // re-issue an invitation for this node? That would change semantics
  // (every "open" call rotates the token). For now: simply hop to the
  // bind port; the operator will reuse the token only if it is still
  // valid (24h TTL). If expired, they need to revoke + re-invite.
  // The redeem URL is therefore stored on the server for `managed_by_me`
  // but not leaked through the listing endpoint. We instead use the
  // local visitor bind port directly: if the operator hadn't logged
  // in there yet they'll see the regular login page.
  const url = `http://127.0.0.1:${node.local_bind_port}/`
  window.open(url, '_blank', 'noopener')
}

async function refreshInvite(node: RemoteNode) {
  if (!window.confirm(t('remote.refresh_confirm', { name: node.name }))) return
  try {
    const resp = await refreshRemoteInvitation(node.id, 'http')
    const qr = await QRCode.toDataURL(resp.invitation, {
      margin: 1,
      width: 256,
      errorCorrectionLevel: 'M',
    })
    inviteResult.value = {
      invitation: resp.invitation,
      expire_at: resp.expire_at,
      driver_warning: resp.driver_warning,
      qr,
    }
    inviteOpen.value = true
    Message.success(t('remote.refresh_success', { name: node.name }))
    await reloadNodes()
  } catch (err) {
    Message.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error
      ?? (err as Error)?.message
      ?? t('msg.opFailed'))
  }
}

async function revoke(node: RemoteNode) {
  if (!window.confirm(t('remote.revoke_confirm'))) return
  try {
    await revokeRemoteNode(node.id)
    Message.success(t('remote.revoked'))
    await reloadNodes()
  } catch (err) {
    Message.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error
      ?? (err as Error)?.message
      ?? t('msg.opFailed'))
  }
}

// revokeToken voids the currently outstanding mgmt_token for a
// `manages_me` row. The pairing itself stays alive; only the QR /
// token that has been (or is suspected to have been) leaked is
// invalidated. Operator typically follows with `refreshInvite` to
// hand a fresh QR to the legitimate redeemer.
async function revokeToken(node: RemoteNode) {
  if (!window.confirm(t('remote.revoke_token_confirm'))) return
  try {
    await revokeRemoteMgmtToken(node.id)
    Message.success(t('remote.token_revoked'))
    await reloadNodes()
  } catch (err) {
    Message.error((err as { response?: { data?: { error?: string } } })?.response?.data?.error
      ?? (err as Error)?.message
      ?? t('msg.opFailed'))
  }
}

onMounted(async () => {
  if (!passwordModeOk.value) return
  await Promise.all([reloadEndpoints(), reloadNodes()])
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('remote.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('remote.subtitle') }}</p>
      </div>
      <div v-if="passwordModeOk" class="flex gap-2">
        <Button variant="outline" :disabled="loading" @click="reloadNodes">
          <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />
          <span>{{ t('remote.refresh') }}</span>
        </Button>
        <Button variant="outline" @click="openRedeem">
          <Plus class="size-4" />
          <span>{{ t('remote.redeem_action') }}</span>
        </Button>
        <Button @click="openInvite">
          <Plus class="size-4" />
          <span>{{ t('remote.invite_action') }}</span>
        </Button>
      </div>
    </div>

    <div
      v-if="!passwordModeOk"
      class="rounded-md border bg-amber-500/10 text-amber-700 dark:text-amber-300 px-4 py-3 flex items-start gap-3"
    >
      <ShieldAlert class="size-5 shrink-0 mt-0.5" />
      <div class="flex flex-col gap-1">
        <div class="text-sm font-semibold">{{ t('remote.auth_mode_required_title') }}</div>
        <div class="text-xs">{{ t('remote.auth_mode_required_hint') }}</div>
      </div>
    </div>

    <Tabs v-else v-model="tab" default-value="managed_by_me">
      <TabsList>
        <TabsTrigger value="managed_by_me">{{ t('remote.tabs.managed_by_me') }}</TabsTrigger>
        <TabsTrigger value="manages_me">{{ t('remote.tabs.manages_me') }}</TabsTrigger>
      </TabsList>

      <TabsContent value="managed_by_me" class="pt-4">
        <Table v-if="managedByMe.length">
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('remote.table.name') }}</TableHead>
              <TableHead>{{ t('remote.table.endpoint') }}</TableHead>
              <TableHead>{{ t('remote.table.bind_port') }}</TableHead>
              <TableHead>{{ t('remote.table.status') }}</TableHead>
              <TableHead>{{ t('remote.table.last_seen') }}</TableHead>
              <TableHead class="text-right">{{ t('remote.table.actions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="n in managedByMe" :key="n.id">
              <TableCell class="font-medium">{{ n.name }}</TableCell>
              <TableCell class="text-sm">{{ endpointName(n.endpoint_id) }}</TableCell>
              <TableCell class="font-mono text-sm">{{ n.local_bind_port || '-' }}</TableCell>
              <TableCell>
                <Badge :variant="statusVariant(n.status)">
                  {{ t('remote.status.' + n.status) }}
                </Badge>
              </TableCell>
              <TableCell class="text-sm text-muted-foreground">{{ fmtTime(n.last_seen) }}</TableCell>
              <TableCell class="text-right">
                <Button size="sm" variant="ghost" @click="openRemote(n)">
                  <ExternalLink class="size-4" />
                  <span class="hidden md:inline">{{ t('remote.open') }}</span>
                </Button>
                <Button size="sm" variant="ghost" class="text-destructive" @click="revoke(n)">
                  <Trash2 class="size-4" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <EmptyState
          v-else
          icon="🔗"
          :title="t('remote.empty_managed')"
          :description="t('remote.empty_managed_hint')"
        />
      </TabsContent>

      <TabsContent value="manages_me" class="pt-4">
        <Table v-if="managesMe.length">
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('remote.table.name') }}</TableHead>
              <TableHead>{{ t('remote.table.endpoint') }}</TableHead>
              <TableHead>{{ t('remote.table.status') }}</TableHead>
              <TableHead>{{ t('remote.table.last_seen') }}</TableHead>
              <TableHead>{{ t('remote.table.created_at') }}</TableHead>
              <TableHead class="text-right">{{ t('remote.table.actions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="n in managesMe" :key="n.id">
              <TableCell class="font-medium">{{ n.name }}</TableCell>
              <TableCell class="text-sm">{{ endpointName(n.endpoint_id) }}</TableCell>
              <TableCell>
                <Badge :variant="statusVariant(n.status)">
                  {{ t('remote.status.' + n.status) }}
                </Badge>
              </TableCell>
              <TableCell class="text-sm text-muted-foreground">{{ fmtTime(n.last_seen) }}</TableCell>
              <TableCell class="text-sm text-muted-foreground">{{ fmtTime(n.created_at) }}</TableCell>
              <TableCell class="text-right">
                <Button
                  size="sm"
                  variant="ghost"
                  :disabled="n.status === 'revoked'"
                  :title="t('remote.refresh_invite')"
                  @click="refreshInvite(n)"
                >
                  <RotateCcw class="size-4" />
                  <span class="hidden md:inline">{{ t('remote.refresh_invite') }}</span>
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  :disabled="n.status === 'revoked'"
                  :title="t('remote.revoke_token')"
                  @click="revokeToken(n)"
                >
                  <KeyRound class="size-4" />
                  <span class="hidden md:inline">{{ t('remote.revoke_token') }}</span>
                </Button>
                <Button size="sm" variant="ghost" class="text-destructive" @click="revoke(n)">
                  <Trash2 class="size-4" />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <EmptyState
          v-else
          icon="📡"
          :title="t('remote.empty_manages')"
          :description="t('remote.empty_manages_hint')"
        />
      </TabsContent>
    </Tabs>

    <Dialog v-model:open="inviteOpen">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t('remote.invite.title') }}</DialogTitle>
        </DialogHeader>
        <p class="text-sm text-muted-foreground">{{ t('remote.invite.subtitle') }}</p>

        <div v-if="!inviteResult" class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('remote.invite.endpoint') }}</Label>
            <Select v-model="inviteForm.endpoint_id">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="ep in endpoints"
                  :key="ep.id"
                  :value="String(ep.id)"
                >
                  {{ ep.name }} ({{ ep.addr }}:{{ ep.port }})
                </SelectItem>
              </SelectContent>
            </Select>
            <span class="text-xs text-muted-foreground">{{ t('remote.invite.endpoint_hint') }}</span>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>{{ t('remote.invite.node_name') }}</Label>
            <Input v-model="inviteForm.node_name" placeholder="hq-frpdeck" />
            <span class="text-xs text-muted-foreground">{{ t('remote.invite.node_name_hint') }}</span>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>{{ t('remote.invite.ui_scheme') }}</Label>
            <Select v-model="inviteForm.ui_scheme">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="http">{{ t('remote.invite.ui_scheme_http') }}</SelectItem>
                <SelectItem value="https">{{ t('remote.invite.ui_scheme_https') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <div v-else class="flex flex-col gap-4">
          <div class="flex flex-col items-center gap-2">
            <img :src="inviteResult.qr" alt="invitation qr" class="rounded-md border" />
            <p class="text-xs text-muted-foreground">{{ t('remote.invite.result_qr_hint') }}</p>
          </div>
          <textarea
            class="w-full font-mono text-xs leading-relaxed rounded-md border bg-muted/40 p-2 break-all"
            rows="5"
            readonly
            :value="inviteResult.invitation"
          />
          <p class="text-xs text-muted-foreground">
            {{ t('remote.invite.result_hint') }}
            <span class="font-medium text-foreground"> · {{ fmtTime(inviteResult.expire_at) }}</span>
          </p>
          <p v-if="inviteResult.driver_warning" class="text-xs text-amber-500">
            {{ t('remote.invite.driver_warning', { msg: inviteResult.driver_warning }) }}
          </p>
        </div>

        <DialogFooter>
          <Button v-if="inviteResult" variant="outline" @click="copyInvite">
            <Copy class="size-4" />
            <span>{{ t('remote.invite.copy') }}</span>
          </Button>
          <Button variant="outline" @click="inviteOpen = false">{{ t('remote.invite.close') }}</Button>
          <Button v-if="!inviteResult" :disabled="inviteSubmitting" @click="submitInvite">
            {{ inviteSubmitting ? t('remote.invite.submitting') : t('remote.invite.submit') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="redeemOpen">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t('remote.redeem.title') }}</DialogTitle>
        </DialogHeader>
        <p class="text-sm text-muted-foreground">{{ t('remote.redeem.subtitle') }}</p>

        <div v-if="!redeemSuccess" class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <div class="flex items-center justify-between">
              <Label>{{ t('remote.redeem.input_label') }}</Label>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                :disabled="redeemDecoding"
                @click="triggerRedeemUpload"
              >
                <Upload class="size-4" />
                <span>{{ redeemDecoding ? t('remote.redeem.qr_decoding') : t('remote.redeem.qr_upload') }}</span>
              </Button>
              <input
                ref="redeemUploadRef"
                type="file"
                accept="image/*"
                class="hidden"
                @change="handleRedeemUpload"
              />
            </div>
            <textarea
              class="w-full font-mono text-xs leading-relaxed rounded-md border bg-background p-2 break-all"
              rows="5"
              :placeholder="t('remote.redeem.input_placeholder')"
              v-model="redeemForm.invitation"
            />
            <span class="text-xs text-muted-foreground">{{ t('remote.redeem.qr_upload_hint') }}</span>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('remote.redeem.node_name') }}</Label>
            <Input v-model="redeemForm.node_name" placeholder="hq-frpdeck" />
            <span class="text-xs text-muted-foreground">{{ t('remote.redeem.node_name_hint') }}</span>
          </div>
        </div>

        <div v-else class="flex flex-col gap-3">
          <p class="text-sm">
            {{ t('remote.redeem.success', { name: redeemSuccess.name }) }}
          </p>
          <p v-if="redeemSuccess.driver_warning" class="text-xs text-amber-500">
            {{ t('remote.redeem.driver_warning', { msg: redeemSuccess.driver_warning }) }}
          </p>
          <a
            :href="redeemSuccess.redeem_url"
            target="_blank"
            rel="noopener"
            class="inline-flex items-center gap-1 text-primary text-sm hover:underline"
          >
            <ExternalLink class="size-4" />
            <span>{{ t('remote.redeem.open_remote') }}</span>
          </a>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="redeemOpen = false">{{ t('remote.redeem.close') }}</Button>
          <Button v-if="!redeemSuccess" :disabled="redeemSubmitting" @click="submitRedeem">
            {{ redeemSubmitting ? t('remote.redeem.submitting') : t('remote.redeem.submit') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
