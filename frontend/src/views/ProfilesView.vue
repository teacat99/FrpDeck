<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, Pencil, Trash2, Power, PowerOff } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import EmptyState from '@/components/EmptyState.vue'
import { Message } from '@/lib/toast'
import {
  listProfiles, getProfile, createProfile, updateProfile,
  deleteProfile, activateProfile, deactivateProfiles,
} from '@/api/profiles'
import type { Profile, ProfileBinding } from '@/api/profiles'
import { listEndpoints } from '@/api/endpoints'
import { listTunnels } from '@/api/tunnels'
import type { Endpoint, Tunnel } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const profiles = ref<Profile[]>([])
const endpoints = ref<Endpoint[]>([])
const tunnels = ref<Tunnel[]>([])
const loading = ref(false)
const dialogOpen = ref(false)
const submitting = ref(false)
const editing = ref<Profile | null>(null)

interface FormState {
  name: string
  active: boolean
  // Endpoint-wildcard mode: every tunnel under this endpoint is enabled.
  endpointWildcards: Set<number>
  // Per-tunnel selections. Tunnels live under endpoints whose id is NOT
  // in endpointWildcards — wildcarded endpoints take care of all theirs.
  tunnelIDs: Set<number>
}

const form = reactive<FormState>(emptyForm())

function emptyForm(): FormState {
  return { name: '', active: false, endpointWildcards: new Set(), tunnelIDs: new Set() }
}

async function reloadAll() {
  loading.value = true
  try {
    const [ps, eps, ts] = await Promise.all([
      listProfiles(), listEndpoints(), listTunnels(),
    ])
    profiles.value = ps
    endpoints.value = eps
    tunnels.value = ts
  } finally {
    loading.value = false
  }
}

const activeProfile = computed(() => profiles.value.find((p) => p.active) ?? null)

// Group tunnels by endpoint for the binding picker so the UI can show
// "endpoint > tunnel" hierarchy without re-querying on every render.
const tunnelsByEndpoint = computed(() => {
  const map = new Map<number, Tunnel[]>()
  for (const tu of tunnels.value) {
    const list = map.get(tu.endpoint_id) ?? []
    list.push(tu)
    map.set(tu.endpoint_id, list)
  }
  return map
})

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  dialogOpen.value = true
}

async function openEdit(p: Profile) {
  editing.value = p
  Object.assign(form, emptyForm())
  form.name = p.name
  form.active = p.active
  try {
    const detail = await getProfile(p.id)
    for (const b of detail.bindings) {
      if (b.endpoint_id !== 0 && b.tunnel_id === 0) {
        form.endpointWildcards.add(b.endpoint_id)
      } else if (b.tunnel_id !== 0) {
        form.tunnelIDs.add(b.tunnel_id)
      }
    }
    dialogOpen.value = true
  } catch (err) {
    Message.error((err as Error).message)
  }
}

function buildBindings(): ProfileBinding[] {
  const out: ProfileBinding[] = []
  for (const epID of form.endpointWildcards) {
    out.push({ endpoint_id: epID, tunnel_id: 0 })
  }
  for (const tID of form.tunnelIDs) {
    const tu = tunnels.value.find((x) => x.id === tID)
    if (!tu) continue
    if (form.endpointWildcards.has(tu.endpoint_id)) continue
    out.push({ endpoint_id: tu.endpoint_id, tunnel_id: tID })
  }
  return out
}

async function submit() {
  if (!form.name.trim()) {
    Message.warning(t('profile.name'))
    return
  }
  submitting.value = true
  try {
    const payload = {
      name: form.name.trim(),
      active: form.active,
      bindings: buildBindings(),
    }
    if (editing.value) {
      await updateProfile(editing.value.id, payload)
    } else {
      await createProfile(payload)
    }
    Message.success(t('profile.saved'))
    dialogOpen.value = false
    await reloadAll()
  } finally {
    submitting.value = false
  }
}

async function activate(p: Profile) {
  try {
    await activateProfile(p.id)
    Message.success(t('profile.activate_success', { name: p.name }))
    await reloadAll()
  } catch (err) {
    Message.error((err as Error).message)
  }
}

async function deactivateAll() {
  try {
    await deactivateProfiles()
    Message.success(t('profile.deactivated'))
    await reloadAll()
  } catch (err) {
    Message.error((err as Error).message)
  }
}

async function remove(p: Profile) {
  if (p.active) {
    Message.warning(t('profile.delete_active_blocked'))
    return
  }
  if (!confirm(t('profile.confirm_delete', { name: p.name }))) return
  try {
    await deleteProfile(p.id)
    Message.success(t('common.deleted'))
    await reloadAll()
  } catch (err) {
    Message.error((err as Error).message)
  }
}

function toggleEndpointWildcard(epID: number) {
  if (form.endpointWildcards.has(epID)) {
    form.endpointWildcards.delete(epID)
  } else {
    form.endpointWildcards.add(epID)
    // Wildcarding a whole endpoint subsumes any per-tunnel selections
    // beneath it, so we drop them to keep the binding list minimal.
    for (const tu of tunnelsByEndpoint.value.get(epID) ?? []) {
      form.tunnelIDs.delete(tu.id)
    }
  }
}

function toggleTunnel(tID: number) {
  if (form.tunnelIDs.has(tID)) {
    form.tunnelIDs.delete(tID)
  } else {
    form.tunnelIDs.add(tID)
  }
}

onMounted(reloadAll)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('profile.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('profile.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="outline" :disabled="loading" @click="reloadAll">
          <RefreshCw class="size-4" :class="{ 'animate-spin': loading }" />
          <span>{{ t('common.refresh') }}</span>
        </Button>
        <Button v-if="auth.isAdmin && activeProfile" variant="outline" @click="deactivateAll">
          <PowerOff class="size-4" />
          <span>{{ t('profile.deactivate_all') }}</span>
        </Button>
        <Button v-if="auth.isAdmin" @click="openCreate">
          <Plus class="size-4" />
          <span>{{ t('profile.new') }}</span>
        </Button>
      </div>
    </div>

    <div v-if="!activeProfile" class="rounded-md border border-dashed border-border p-3 text-sm text-muted-foreground">
      {{ t('profile.no_active') }}
    </div>

    <Table v-if="profiles.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('profile.name') }}</TableHead>
          <TableHead>{{ t('profile.active_label') }}</TableHead>
          <TableHead class="text-right">{{ t('common.actions') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="p in profiles" :key="p.id">
          <TableCell class="font-medium">{{ p.name }}</TableCell>
          <TableCell>
            <Badge v-if="p.active">{{ t('profile.active_label') }}</Badge>
            <span v-else class="text-muted-foreground">—</span>
          </TableCell>
          <TableCell class="text-right">
            <Button v-if="auth.isAdmin && !p.active" size="icon" variant="ghost" @click="activate(p)">
              <Power class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" @click="openEdit(p)">
              <Pencil class="size-4" />
            </Button>
            <Button v-if="auth.isAdmin" size="icon" variant="ghost" class="text-destructive" @click="remove(p)">
              <Trash2 class="size-4" />
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <EmptyState v-else icon="📦" :title="t('profile.empty')" />

    <Dialog v-model:open="dialogOpen">
      <DialogContent class="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('profile.edit') : t('profile.new') }}</DialogTitle>
        </DialogHeader>

        <div class="flex flex-col gap-5">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('profile.name') }}</Label>
            <Input v-model="form.name" placeholder="home / office / demo" />
          </div>
          <div class="flex items-center gap-3">
            <Switch v-model:checked="form.active" />
            <span class="text-sm">{{ t('profile.activate') }}</span>
            <span v-if="editing && form.active" class="text-xs text-amber-600">
              {{ t('profile.edit_active_warn') }}
            </span>
          </div>

          <section class="flex flex-col gap-3">
            <div class="flex flex-col gap-1">
              <Label>{{ t('profile.bindings') }}</Label>
              <span class="text-xs text-muted-foreground">{{ t('profile.bindings_hint') }}</span>
            </div>

            <div v-for="ep in endpoints" :key="ep.id" class="rounded-md border border-border p-3 flex flex-col gap-2">
              <label class="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  class="size-4"
                  :checked="form.endpointWildcards.has(ep.id)"
                  @change="toggleEndpointWildcard(ep.id)"
                />
                <span class="font-medium">{{ ep.name }}</span>
                <span class="text-xs text-muted-foreground font-mono">{{ ep.addr }}:{{ ep.port }}</span>
                <Badge variant="outline" class="ml-auto">{{ t('profile.binding_endpoint') }}</Badge>
              </label>
              <div
                v-if="!form.endpointWildcards.has(ep.id) && (tunnelsByEndpoint.get(ep.id)?.length ?? 0) > 0"
                class="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1 pl-7"
              >
                <label v-for="tu in tunnelsByEndpoint.get(ep.id)" :key="tu.id" class="flex items-center gap-2 cursor-pointer text-sm">
                  <input
                    type="checkbox"
                    class="size-4"
                    :checked="form.tunnelIDs.has(tu.id)"
                    @change="toggleTunnel(tu.id)"
                  />
                  <span>{{ tu.name }}</span>
                  <span class="text-xs text-muted-foreground">[{{ tu.type }}]</span>
                </label>
              </div>
            </div>
          </section>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false">
            {{ t('common.cancel') }}
          </Button>
          <Button :disabled="submitting" @click="submit">
            {{ t('common.save') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
