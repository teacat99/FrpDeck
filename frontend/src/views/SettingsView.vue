<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import dayjs from 'dayjs'
import {
  Plus, RefreshCw, Pencil, Trash2,
  Users as UsersIcon, Settings as SettingsIcon, History as HistoryIcon,
  Lock, Check, X as XIcon,
} from 'lucide-vue-next'
import {
  createUser, deleteUser, listUsers, resetUserPassword, updateUser,
} from '@/api/users'
import { fetchLoginHistory, type LoginAttempt } from '@/api/auth'
import type { Role, User } from '@/api/types'

import { useAuthStore } from '@/stores/auth'
import { Message } from '@/lib/toast'

import RuntimeSettingsForm from '@/components/RuntimeSettingsForm.vue'
import EmptyState from '@/components/EmptyState.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import {
  Tabs, TabsList, TabsTrigger, TabsContent,
} from '@/components/ui/tabs'
import {
  Select, SelectTrigger, SelectValue, SelectContent, SelectItem,
} from '@/components/ui/select'
import {
  Table, TableHeader, TableBody, TableRow, TableHead, TableCell,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogFooter, DialogTitle,
} from '@/components/ui/dialog'

const { t } = useI18n()
const auth = useAuthStore()

const activeTab = ref<'users' | 'runtime' | 'security'>('users')

// ------------------------- Users -------------------------

const users = ref<User[]>([])
const usersLoading = ref(false)

const userDialogOpen = ref(false)
const userEditing = ref<User | null>(null)
const userForm = reactive({ username: '', password: '', role: 'user' as Role })
const userSubmitting = ref(false)

const pwdDialogOpen = ref(false)
const pwdTarget = ref<User | null>(null)
const pwdForm = reactive({ password: '' })
const pwdSubmitting = ref(false)

async function loadUsers() {
  usersLoading.value = true
  try {
    users.value = await listUsers()
  } finally {
    usersLoading.value = false
  }
}

function openCreateUser() {
  userEditing.value = null
  Object.assign(userForm, { username: '', password: '', role: 'user' as Role })
  userDialogOpen.value = true
}

function openEditUser(u: User) {
  userEditing.value = u
  Object.assign(userForm, { username: u.username, password: '', role: u.role })
  userDialogOpen.value = true
}

async function submitUser() {
  if (!userForm.username.trim()) {
    Message.warning(t('user.username_required'))
    return
  }
  if (!userEditing.value && userForm.password.length < 8) {
    Message.warning(t('user.password_too_short'))
    return
  }
  userSubmitting.value = true
  try {
    if (userEditing.value) {
      await updateUser(userEditing.value.id, { role: userForm.role })
      Message.success(t('common.updated'))
    } else {
      await createUser({
        username: userForm.username.trim(),
        password: userForm.password,
        role: userForm.role,
      })
      Message.success(t('common.created'))
    }
    userDialogOpen.value = false
    await loadUsers()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.saveFailed'))
  } finally {
    userSubmitting.value = false
  }
}

async function toggleDisabled(u: User) {
  try {
    await updateUser(u.id, { disabled: !u.disabled })
    await loadUsers()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.saveFailed'))
  }
}

async function removeUser(u: User) {
  if (!confirm(t('user.confirm_delete', { name: u.username }))) return
  try {
    await deleteUser(u.id)
    Message.success(t('common.deleted'))
    await loadUsers()
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.deleteFailed'))
  }
}

function openResetPassword(u: User) {
  pwdTarget.value = u
  pwdForm.password = ''
  pwdDialogOpen.value = true
}

async function submitResetPassword() {
  if (!pwdTarget.value) return
  if (pwdForm.password.length < 8) {
    Message.warning(t('user.password_too_short'))
    return
  }
  pwdSubmitting.value = true
  try {
    await resetUserPassword(pwdTarget.value.id, pwdForm.password)
    Message.success(t('user.password_reset'))
    pwdDialogOpen.value = false
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.saveFailed'))
  } finally {
    pwdSubmitting.value = false
  }
}

// ------------------------- Login history -------------------------

const loginAttempts = ref<LoginAttempt[]>([])
const loginAttemptsLoading = ref(false)
const loginAttemptsFilter = ref('')

async function loadLoginHistory() {
  loginAttemptsLoading.value = true
  try {
    loginAttempts.value = await fetchLoginHistory({
      username: loginAttemptsFilter.value.trim() || undefined,
      limit: 200,
    })
  } catch {
    loginAttempts.value = []
  } finally {
    loginAttemptsLoading.value = false
  }
}

const isAdmin = computed(() => auth.isAdmin)

onMounted(async () => {
  if (!isAdmin.value) return
  await loadUsers()
})
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h1 class="text-2xl font-semibold">{{ t('settings.title') }}</h1>
      <p class="text-sm text-muted-foreground">{{ t('settings.subtitle') }}</p>
    </div>

    <Tabs v-model="activeTab">
      <TabsList>
        <TabsTrigger value="users">
          <UsersIcon class="size-4 mr-1" />{{ t('settings.tabs.users') }}
        </TabsTrigger>
        <TabsTrigger value="runtime">
          <SettingsIcon class="size-4 mr-1" />{{ t('settings.tabs.runtime') }}
        </TabsTrigger>
        <TabsTrigger value="security" @click="loadLoginHistory">
          <HistoryIcon class="size-4 mr-1" />{{ t('settings.tabs.security') }}
        </TabsTrigger>
      </TabsList>

      <!-- Users -->
      <TabsContent value="users" class="flex flex-col gap-3">
        <div class="flex items-center justify-between">
          <p class="text-sm text-muted-foreground">{{ t('settings.users_hint') }}</p>
          <div class="flex gap-2">
            <Button variant="outline" :disabled="usersLoading" @click="loadUsers">
              <RefreshCw class="size-4" :class="{ 'animate-spin': usersLoading }" />
              <span>{{ t('common.refresh') }}</span>
            </Button>
            <Button @click="openCreateUser">
              <Plus class="size-4" />{{ t('user.add') }}
            </Button>
          </div>
        </div>

        <Table v-if="users.length">
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('user.field.username') }}</TableHead>
              <TableHead>{{ t('user.field.role') }}</TableHead>
              <TableHead>{{ t('user.field.disabled') }}</TableHead>
              <TableHead>{{ t('user.field.created_at') }}</TableHead>
              <TableHead class="text-right">{{ t('common.actions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="u in users" :key="u.id">
              <TableCell class="font-medium">{{ u.username }}</TableCell>
              <TableCell>
                <Badge :variant="u.role === 'admin' ? 'default' : 'secondary'">
                  {{ t(`role.${u.role}`) }}
                </Badge>
              </TableCell>
              <TableCell>
                <Switch :checked="!u.disabled" @update:checked="toggleDisabled(u)" />
              </TableCell>
              <TableCell class="font-mono text-xs">{{ dayjs(u.created_at).format('YYYY-MM-DD') }}</TableCell>
              <TableCell class="text-right">
                <Button size="icon" variant="ghost" @click="openEditUser(u)"><Pencil class="size-4" /></Button>
                <Button size="icon" variant="ghost" @click="openResetPassword(u)"><Lock class="size-4" /></Button>
                <Button size="icon" variant="ghost" class="text-destructive" @click="removeUser(u)"><Trash2 class="size-4" /></Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <EmptyState v-else icon="👤" :title="t('user.empty')" />
      </TabsContent>

      <!-- Runtime -->
      <TabsContent value="runtime">
        <RuntimeSettingsForm />
      </TabsContent>

      <!-- Security: login history -->
      <TabsContent value="security" class="flex flex-col gap-3">
        <div class="flex items-center gap-2 flex-wrap">
          <Input v-model="loginAttemptsFilter" :placeholder="t('settings.security.filter_username')" class="max-w-xs" />
          <Button variant="outline" :disabled="loginAttemptsLoading" @click="loadLoginHistory">
            <RefreshCw class="size-4" :class="{ 'animate-spin': loginAttemptsLoading }" />
            <span>{{ t('common.refresh') }}</span>
          </Button>
        </div>
        <Table v-if="loginAttempts.length">
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('history.field.time') }}</TableHead>
              <TableHead>{{ t('user.field.username') }}</TableHead>
              <TableHead>{{ t('settings.security.field.ip') }}</TableHead>
              <TableHead>{{ t('settings.security.field.success') }}</TableHead>
              <TableHead>{{ t('settings.security.field.reason') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="a in loginAttempts" :key="a.id">
              <TableCell class="font-mono text-xs">{{ dayjs(a.created_at).format('YYYY-MM-DD HH:mm:ss') }}</TableCell>
              <TableCell>{{ a.username }}</TableCell>
              <TableCell class="font-mono text-xs">{{ a.client_ip }}</TableCell>
              <TableCell>
                <Check v-if="a.success" class="size-4 text-emerald-500" />
                <XIcon v-else class="size-4 text-destructive" />
              </TableCell>
              <TableCell class="text-xs text-muted-foreground">{{ a.reason }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <EmptyState v-else icon="🛡️" :title="t('settings.security.empty')" />
      </TabsContent>
    </Tabs>

    <!-- User dialog -->
    <Dialog v-model:open="userDialogOpen">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ userEditing ? t('user.edit') : t('user.add') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('user.field.username') }}</Label>
            <Input v-model="userForm.username" :disabled="!!userEditing" autocomplete="username" />
          </div>
          <div v-if="!userEditing" class="flex flex-col gap-1.5">
            <Label>{{ t('user.field.password') }}</Label>
            <Input v-model="userForm.password" type="password" autocomplete="new-password" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('user.field.role') }}</Label>
            <Select v-model="userForm.role">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="user">{{ t('role.user') }}</SelectItem>
                <SelectItem value="admin">{{ t('role.admin') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="userDialogOpen = false">{{ t('common.cancel') }}</Button>
          <Button :disabled="userSubmitting" @click="submitUser">{{ t('common.confirm') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Reset password dialog -->
    <Dialog v-model:open="pwdDialogOpen">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t('user.reset_password') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-1.5">
          <Label>{{ t('user.field.password') }}</Label>
          <Input v-model="pwdForm.password" type="password" autocomplete="new-password" />
        </div>
        <DialogFooter>
          <Button variant="outline" @click="pwdDialogOpen = false">{{ t('common.cancel') }}</Button>
          <Button :disabled="pwdSubmitting" @click="submitResetPassword">{{ t('common.confirm') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
