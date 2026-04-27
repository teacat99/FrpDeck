<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Home,
  Server,
  Network,
  History,
  Settings,
  Share2,
  Layers,
  LogOut,
  User as UserIcon,
  Lock,
  Moon,
  Sun,
  SunMoon,
  Eye,
  EyeOff,
  Smartphone
} from 'lucide-vue-next'
import { isNativeBridge, nativeIsAndroid } from '@/composables/useNativeBridge'
import LanguageIcon from '@/components/LanguageIcon.vue'
import { setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { changeOwnPassword } from '@/api/auth'
import { Message } from '@/lib/toast'
import logoUrl from '@/assets/logo.svg'

import PwaInstallPrompt from '@/components/PwaInstallPrompt.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator
} from '@/components/ui/dropdown-menu'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter
} from '@/components/ui/dialog'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const auth = useAuthStore()
const theme = useThemeStore()

const pwdModal = ref(false)
const pwdSubmitting = ref(false)
const pwdForm = ref({ old_password: '', new_password: '', confirm: '' })
const showPwdOld = ref(false)
const showPwdNew = ref(false)
const showPwdConfirm = ref(false)

onMounted(async () => {
  await auth.refreshStatus()
  if (auth.token || !auth.required) {
    await auth.fetchMe()
  }
  // Request notification permission once the operator lands on the
  // authenticated shell — `tunnel_expiring` warnings rely on it. We
  // do NOT ask while the user is still on the login page (auth.me is
  // null there) so the prompt feels contextual.
  if (auth.me && typeof window !== 'undefined' && 'Notification' in window) {
    if (Notification.permission === 'default') {
      try { await Notification.requestPermission() } catch { /* user denied / unsupported */ }
    }
  }
})

const currentKey = computed(() => String(route.name ?? 'home'))
const isAdmin = computed(() => auth.isAdmin)
const canChangePassword = computed(
  () => auth.required && auth.mode === 'password' && !!auth.me
)

// Localised admin tag text — "管理员" in zh, "root" in en, matches the
// Phase-2 requirement that the top-right role badge speaks user-friendly
// language instead of the internal enum.
const adminTagText = computed(() =>
  locale.value === 'zh-CN' ? t('role.admin') : 'root'
)

interface NavItem {
  key: string
  label: string
  icon: typeof Home
}

const isAndroidShell = computed(() => isNativeBridge() && nativeIsAndroid())

const navItems = computed<NavItem[]>(() => {
  const base: NavItem[] = [
    { key: 'home', label: t('menu.home'), icon: Home },
    { key: 'endpoints', label: t('menu.endpoints'), icon: Server },
    { key: 'tunnels', label: t('menu.tunnels'), icon: Network },
    { key: 'history', label: t('menu.history'), icon: History }
  ]
  if (isAdmin.value) {
    if (auth.mode === 'password') {
      base.push({ key: 'remote', label: t('menu.remote'), icon: Share2 })
    }
    base.push({ key: 'profiles', label: t('menu.profiles'), icon: Layers })
    base.push({ key: 'settings', label: t('menu.settings'), icon: Settings })
  }
  if (isAndroidShell.value) {
    base.push({ key: 'android', label: t('menu.android'), icon: Smartphone })
  }
  return base
})

function navigate(key: string) {
  router.push({ name: key })
}

function toggleLocale() {
  const next = locale.value === 'zh-CN' ? 'en-US' : 'zh-CN'
  setLocale(next as 'zh-CN' | 'en-US')
}

function logout() {
  auth.logout()
  router.push({ name: 'login' })
}

function openPasswordModal() {
  pwdForm.value = { old_password: '', new_password: '', confirm: '' }
  showPwdOld.value = false
  showPwdNew.value = false
  showPwdConfirm.value = false
  pwdModal.value = true
}

const pwdStrength = computed(() => {
  const v = pwdForm.value.new_password
  if (!v) return { score: 0, label: '' }
  let score = 0
  if (v.length >= 6) score++
  if (v.length >= 10) score++
  if (/[A-Z]/.test(v) && /[a-z]/.test(v)) score++
  if (/\d/.test(v)) score++
  if (/[^A-Za-z0-9]/.test(v)) score++
  const map = ['', '太弱', '一般', '中等', '不错', '很强']
  return { score, label: map[score] || '' }
})

const barColor = (i: number) => {
  if (i > pwdStrength.value.score) return 'bg-border'
  const s = pwdStrength.value.score
  if (s <= 2) return 'bg-destructive'
  if (s === 3) return 'bg-amber-500'
  if (s === 4) return 'bg-yellow-500'
  return 'bg-emerald-500'
}

async function submitPassword() {
  if (pwdForm.value.new_password.length < 6) {
    Message.warning(t('password.too_short'))
    return
  }
  if (pwdForm.value.new_password !== pwdForm.value.confirm) {
    Message.warning(t('password.mismatch'))
    return
  }
  pwdSubmitting.value = true
  try {
    await changeOwnPassword(pwdForm.value.old_password, pwdForm.value.new_password)
    Message.success(t('password.changed'))
    pwdModal.value = false
  } finally {
    pwdSubmitting.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex flex-col bg-background text-foreground">
    <!--
      Header. Sticky so navigation + user controls are always reachable on
      long pages. Use backdrop-blur so content scrolling beneath feels
      native without the header obscuring it.
    -->
    <header class="sticky top-0 z-30 h-14 md:h-16 border-b border-border bg-card/95 backdrop-blur flex items-center justify-between gap-4 px-3 md:px-5">
      <div class="flex items-center gap-3 min-w-0 shrink-0">
        <img :src="logoUrl" alt="FrpDeck" class="size-8 rounded-md shrink-0" />
        <div class="flex flex-col min-w-0">
          <span class="font-semibold text-sm md:text-base leading-tight truncate">{{ t('app.title') }}</span>
          <span class="hidden md:block text-[11px] text-muted-foreground truncate">{{ t('app.subtitle') }}</span>
        </div>
      </div>

      <!--
        Desktop primary nav sits in the header (md+ only). On mobile this
        collapses and navigation lives in the bottom dock. This replaces
        the old left aside so the page gets the full horizontal width for
        tables / forms.
      -->
      <nav class="hidden md:flex items-center gap-1 flex-1 min-w-0" aria-label="primary">
        <button
          v-for="n in navItems"
          :key="n.key"
          type="button"
          class="group inline-flex items-center gap-2 px-3 h-9 rounded-md text-sm font-medium transition-colors"
          :class="currentKey === n.key
            ? 'bg-primary/10 text-primary'
            : 'text-muted-foreground hover:bg-accent hover:text-foreground'"
          @click="navigate(n.key)"
        >
          <component :is="n.icon" class="size-4" />
          <span>{{ n.label }}</span>
        </button>
      </nav>

      <div class="flex items-center gap-1 shrink-0">
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="ghost" size="icon" aria-label="theme" @click="theme.toggle()">
              <SunMoon v-if="theme.mode === 'auto'" />
              <Sun v-else-if="theme.mode === 'light'" />
              <Moon v-else />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {{ theme.mode === 'auto' ? t('theme.switchTo.light') : theme.mode === 'light' ? t('theme.switchTo.dark') : t('theme.switchTo.auto') }}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="ghost" size="icon" aria-label="locale" @click="toggleLocale">
              <LanguageIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {{ locale === 'zh-CN' ? 'Switch to English' : '切换到中文' }}
          </TooltipContent>
        </Tooltip>

        <DropdownMenu v-if="auth.me">
          <DropdownMenuTrigger as-child>
            <Button variant="ghost" class="h-9 px-2 md:px-3 rounded-full">
              <UserIcon class="size-4" />
              <span class="hidden sm:inline font-medium">{{ auth.me.username }}</span>
              <Badge v-if="isAdmin" variant="default" class="ml-1">{{ adminTagText }}</Badge>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" class="w-52">
            <DropdownMenuLabel>
              <div class="flex flex-col gap-0.5">
                <span class="text-sm font-medium text-foreground">{{ auth.me.username }}</span>
                <span class="text-xs text-muted-foreground">
                  {{ isAdmin ? adminTagText : t('role.user') }}
                </span>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem v-if="canChangePassword" @click="openPasswordModal">
              <Lock />
              <span>{{ t('action.change_password') }}</span>
            </DropdownMenuItem>
            <DropdownMenuItem
              v-if="auth.required && auth.token"
              class="text-destructive focus:text-destructive focus:bg-destructive/10"
              @click="logout"
            >
              <LogOut />
              <span>{{ t('action.logout') }}</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          v-else-if="auth.required && auth.token"
          variant="ghost"
          class="text-destructive"
          @click="logout"
        >
          <LogOut class="size-4" />
          <span class="hidden sm:inline">{{ t('action.logout') }}</span>
        </Button>
      </div>
    </header>

    <main class="flex-1 min-w-0 max-w-7xl w-full mx-auto px-3 md:px-6 py-4 md:py-6 pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))] md:pb-6">
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>

    <footer class="hidden md:block border-t border-border py-2 px-5 text-[11px] text-muted-foreground">
      v0.1 · {{ auth.mode }}
    </footer>

    <!--
      Mobile bottom dock. Rendered unconditionally and hidden on md+ via
      Tailwind — this guarantees it appears the moment the viewport shrinks
      under 768px without needing a JS listener, fixing the issue where the
      old `v-if="isMobile"` dock didn't show when the browser window was
      resized.
    -->
    <nav
      class="md:hidden fixed inset-x-0 bottom-0 z-40 flex justify-around items-stretch bg-card border-t border-border px-1 pt-1 pb-[calc(0.25rem+env(safe-area-inset-bottom,0px))] shadow-[0_-4px_14px_-10px_rgba(0,0,0,0.25)]"
      aria-label="primary"
    >
      <button
        v-for="n in navItems"
        :key="n.key"
        type="button"
        class="relative flex-1 min-w-0 flex flex-col items-center justify-center gap-0.5 py-1 rounded-lg text-[11px] transition-colors"
        :class="currentKey === n.key ? 'text-primary font-semibold' : 'text-muted-foreground'"
        @click="navigate(n.key)"
      >
        <span
          v-if="currentKey === n.key"
          class="absolute top-0 left-1/2 -translate-x-1/2 w-6 h-[3px] rounded-b-[3px] bg-primary"
        />
        <component :is="n.icon" class="size-5" />
        <span class="truncate max-w-full">{{ n.label }}</span>
      </button>
    </nav>

    <PwaInstallPrompt v-if="auth.me || !auth.required" />

    <Dialog v-model:open="pwdModal">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t('action.change_password') }}</DialogTitle>
        </DialogHeader>
        <div class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('password.old') }}</Label>
            <div class="relative">
              <Input
                v-model="pwdForm.old_password"
                :type="showPwdOld ? 'text' : 'password'"
                autocomplete="current-password"
                class="pr-10"
              />
              <button type="button" tabindex="-1" class="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted-foreground hover:text-foreground transition-colors" @click="showPwdOld = !showPwdOld">
                <EyeOff v-if="showPwdOld" class="size-4" />
                <Eye v-else class="size-4" />
              </button>
            </div>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('password.new') }}</Label>
            <div class="relative">
              <Input
                v-model="pwdForm.new_password"
                :type="showPwdNew ? 'text' : 'password'"
                autocomplete="new-password"
                class="pr-10"
              />
              <button type="button" tabindex="-1" class="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted-foreground hover:text-foreground transition-colors" @click="showPwdNew = !showPwdNew">
                <EyeOff v-if="showPwdNew" class="size-4" />
                <Eye v-else class="size-4" />
              </button>
            </div>
            <div v-if="pwdForm.new_password" class="flex items-center gap-2 mt-1">
              <div class="flex gap-0.5 flex-1">
                <span
                  v-for="i in 5"
                  :key="i"
                  class="flex-1 h-1 rounded-sm transition-colors"
                  :class="barColor(i)"
                />
              </div>
              <span class="text-[11px] text-muted-foreground w-8 text-right">{{ pwdStrength.label }}</span>
            </div>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('password.confirm') }}</Label>
            <div class="relative">
              <Input
                v-model="pwdForm.confirm"
                :type="showPwdConfirm ? 'text' : 'password'"
                autocomplete="new-password"
                class="pr-10"
              />
              <button type="button" tabindex="-1" class="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted-foreground hover:text-foreground transition-colors" @click="showPwdConfirm = !showPwdConfirm">
                <EyeOff v-if="showPwdConfirm" class="size-4" />
                <Eye v-else class="size-4" />
              </button>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="pwdModal = false">
            {{ t('common.cancel') }}
          </Button>
          <Button :disabled="pwdSubmitting" @click="submitPassword">
            {{ t('common.confirm') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<style scoped>
.fade-enter-active, .fade-leave-active { transition: opacity 0.15s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
