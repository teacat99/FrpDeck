<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, RefreshCw, Pencil, Trash2 } from 'lucide-vue-next'
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
import type { Endpoint } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const rows = ref<Endpoint[]>([])
const loading = ref(false)
const editing = ref<Endpoint | null>(null)
const dialogOpen = ref(false)
const submitting = ref(false)

interface FormState {
  name: string
  group: string
  addr: string
  port: number | string
  protocol: string
  token: string
  user: string
  driver_mode: 'embedded' | 'subprocess'
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
    user: '',
    driver_mode: 'embedded',
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
    user: ep.user,
    driver_mode: ep.driver_mode || 'embedded',
    enabled: ep.enabled,
    auto_start: ep.auto_start,
  })
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
    const payload = {
      name: form.name.trim(),
      group: form.group.trim(),
      addr: form.addr.trim(),
      port,
      protocol: form.protocol,
      token: form.token,
      user: form.user.trim(),
      driver_mode: form.driver_mode,
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

onMounted(reload)
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
          <TableHead>{{ t('endpoint.field.enabled') }}</TableHead>
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
            <Badge :variant="ep.enabled ? 'default' : 'outline'">
              {{ ep.enabled ? t('common.on') : t('common.off') }}
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
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ editing ? t('endpoint.edit') : t('endpoint.add') }}</DialogTitle>
        </DialogHeader>
        <div class="grid grid-cols-2 gap-3">
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
            <Input v-model="form.user" placeholder="" />
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
          <div class="flex items-center gap-2 col-span-2">
            <Switch v-model:checked="form.enabled" />
            <Label class="cursor-pointer" @click="form.enabled = !form.enabled">{{ t('endpoint.field.enabled') }}</Label>
            <Switch class="ml-4" v-model:checked="form.auto_start" />
            <Label class="cursor-pointer" @click="form.auto_start = !form.auto_start">{{ t('endpoint.field.auto_start') }}</Label>
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
