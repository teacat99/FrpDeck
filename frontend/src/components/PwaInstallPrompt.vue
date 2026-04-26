<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, X } from 'lucide-vue-next'
import logoUrl from '@/assets/logo.svg'
import { Button } from '@/components/ui/button'

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>
}

const { t } = useI18n()

const DISMISS_KEY = 'pp.pwaDismiss'
const DISMISS_DAYS = 14
const SHOW_DELAY_MS = 3000

const deferred = ref<BeforeInstallPromptEvent | null>(null)
const visible = ref(false)
const iosFallback = ref(false)

function isStandalone(): boolean {
  if (typeof window === 'undefined') return false
  const mm = window.matchMedia?.('(display-mode: standalone)')
  const nav = window.navigator as unknown as { standalone?: boolean }
  return !!mm?.matches || !!nav.standalone
}

function isIOS(): boolean {
  if (typeof navigator === 'undefined') return false
  const ua = navigator.userAgent || ''
  const iPadOS = /Mac/.test(ua) && 'ontouchend' in document
  return /iPad|iPhone|iPod/.test(ua) || iPadOS
}

function recentlyDismissed(): boolean {
  try {
    const raw = localStorage.getItem(DISMISS_KEY)
    if (!raw) return false
    const ts = Number(raw)
    if (!Number.isFinite(ts)) return false
    return Date.now() - ts < DISMISS_DAYS * 24 * 60 * 60 * 1000
  } catch {
    return false
  }
}

function handleBeforeInstall(e: Event) {
  e.preventDefault()
  deferred.value = e as BeforeInstallPromptEvent
  if (!isStandalone() && !recentlyDismissed()) {
    window.setTimeout(() => { visible.value = true }, SHOW_DELAY_MS)
  }
}

function handleInstalled() {
  visible.value = false
  deferred.value = null
}

onMounted(() => {
  if (isStandalone()) return
  window.addEventListener('beforeinstallprompt', handleBeforeInstall as EventListener)
  window.addEventListener('appinstalled', handleInstalled)

  // iOS Safari never fires beforeinstallprompt; after the normal grace
  // period, fall back to a gentle "add to home screen" instruction so the
  // feature is at least discoverable.
  if (isIOS() && !recentlyDismissed()) {
    window.setTimeout(() => {
      if (!deferred.value && !isStandalone()) {
        iosFallback.value = true
        visible.value = true
      }
    }, SHOW_DELAY_MS)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeinstallprompt', handleBeforeInstall as EventListener)
  window.removeEventListener('appinstalled', handleInstalled)
})

async function install() {
  const d = deferred.value
  if (!d) {
    dismiss()
    return
  }
  try {
    await d.prompt()
    await d.userChoice
  } finally {
    visible.value = false
    deferred.value = null
  }
}

function dismiss() {
  try {
    localStorage.setItem(DISMISS_KEY, String(Date.now()))
  } catch {
    // Silently tolerate quota errors — worst case we re-prompt next load.
  }
  visible.value = false
}

const showIOSFallback = computed(() => iosFallback.value && !deferred.value)
</script>

<template>
  <transition
    enter-active-class="transition duration-300 ease-out"
    enter-from-class="opacity-0 translate-y-2"
    enter-to-class="opacity-100 translate-y-0"
    leave-active-class="transition duration-200 ease-in"
    leave-from-class="opacity-100 translate-y-0"
    leave-to-class="opacity-0 translate-y-2"
  >
    <div
      v-if="visible"
      class="fixed right-5 bottom-5 md:bottom-5 bottom-[calc(4.5rem+env(safe-area-inset-bottom,0px))] left-3 md:left-auto z-[120] max-w-sm flex items-start gap-3 py-3.5 pl-3.5 pr-8 rounded-xl border border-border bg-card shadow-modal"
      role="dialog"
      aria-live="polite"
    >
      <img :src="logoUrl" class="size-10 rounded-lg shrink-0" alt="" />
      <div class="flex-1 min-w-0">
        <div class="font-semibold text-sm text-foreground">{{ t('pwa.title') }}</div>
        <div class="text-xs text-muted-foreground leading-relaxed mt-0.5">
          {{ showIOSFallback ? t('pwa.iosHint') : t('pwa.desc') }}
        </div>
        <div class="flex gap-2 mt-2.5">
          <Button
            v-if="!showIOSFallback"
            size="sm"
            @click="install"
          >
            <Download class="size-3.5" />
            {{ t('pwa.install') }}
          </Button>
          <Button variant="ghost" size="sm" @click="dismiss">
            {{ t('pwa.later') }}
          </Button>
        </div>
      </div>
      <button
        class="absolute top-2 right-2 p-1 rounded-md text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        :aria-label="t('common.close')"
        @click="dismiss"
      >
        <X class="size-4" />
      </button>
    </div>
  </transition>
</template>
