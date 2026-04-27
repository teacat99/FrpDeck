<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Message } from '@/lib/toast'
import { Smartphone, ShieldCheck, ShieldAlert, Save, Upload, Cpu } from 'lucide-vue-next'
import {
  isNativeBridge,
  nativeIsAndroid,
  nativeVersion,
  nativeIsVpnPermissionGranted,
  requestVpnPermission,
  exportBackup,
  importBackup,
} from '@/composables/useNativeBridge'

// AndroidSettingsView.vue — the only place in the SPA that surfaces
// Android-specific affordances. Navigated to from the side dock when
// `window.frpdeck` reports platform=android. Stays inert on desktop /
// browser builds because the route guard kicks the user back home.
//
// Three sections:
//   1. VPN takeover    — pre-authorise the system VPN consent dialog so
//                         frpc-driven socks5 visitor flips don't get
//                         interrupted at runtime.
//   2. Backup          — drive the SAF Create/Open Document launchers
//                         that the kotlin host owns. Restore stops + restarts
//                         the engine; we surface the state via toasts.
//   3. About           — bridge-reported native version + engine origin,
//                         useful for issue reports.
//
// Every action is async: the bridge calls return Promises that the
// kotlin side resolves through `window.frpdeck.__resolve(reqId, ...)`.
// On the desktop side these methods reject synchronously, so the UI
// shows a "native bridge unavailable" message instead of pretending it
// can do SAF.

const { t } = useI18n()
const router = useRouter()

const available = computed(() => isNativeBridge() && nativeIsAndroid())
const version = ref('')
const vpnGranted = ref(false)
const vpnRequesting = ref(false)
const exporting = ref(false)
const importing = ref(false)

function refreshState() {
  version.value = nativeVersion()
  vpnGranted.value = nativeIsVpnPermissionGranted()
}

onMounted(() => {
  if (!available.value) {
    router.replace({ name: 'home' })
    return
  }
  refreshState()
})

async function onRequestVpn() {
  if (vpnRequesting.value) return
  vpnRequesting.value = true
  try {
    await requestVpnPermission()
    vpnGranted.value = true
    Message.success(t('android.vpn_permission_ok'))
  } catch (err) {
    const msg = (err as Error)?.message || ''
    if (msg.includes('denied') || msg.includes('cancelled')) {
      Message.warning(t('android.vpn_permission_denied'))
    } else {
      Message.error(t('android.vpn_permission_failed', { msg }))
    }
    vpnGranted.value = nativeIsVpnPermissionGranted()
  } finally {
    vpnRequesting.value = false
  }
}

async function onExportBackup() {
  if (exporting.value) return
  exporting.value = true
  try {
    const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19)
    const res = await exportBackup(`frpdeck-backup-${ts}.zip`)
    const bytes = (res as { bytes?: number }).bytes ?? 0
    Message.success(t('android.backup_export_ok', { bytes }))
  } catch (err) {
    Message.error(t('android.backup_failed', { msg: (err as Error)?.message ?? '' }))
  } finally {
    exporting.value = false
  }
}

async function onImportBackup() {
  if (importing.value) return
  importing.value = true
  try {
    const res = await importBackup()
    const entries = (res as { entries?: number }).entries ?? 0
    Message.success(t('android.backup_import_ok', { entries }))
  } catch (err) {
    Message.error(t('android.backup_failed', { msg: (err as Error)?.message ?? '' }))
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-6 max-w-3xl">
    <header class="flex items-start gap-3">
      <Smartphone class="size-7 text-primary mt-0.5" />
      <div>
        <h1 class="text-xl font-semibold">{{ t('android.title') }}</h1>
        <p class="text-sm text-muted-foreground mt-1">{{ t('android.subtitle') }}</p>
      </div>
    </header>

    <Card>
      <CardHeader>
        <CardTitle class="flex items-center gap-2 text-base">
          <ShieldCheck v-if="vpnGranted" class="size-4 text-emerald-500" />
          <ShieldAlert v-else class="size-4 text-amber-500" />
          {{ t('android.vpn_section') }}
          <Badge v-if="vpnGranted" variant="default">{{ t('android.vpn_permission_granted') }}</Badge>
          <Badge v-else variant="secondary">{{ t('android.vpn_permission_pending') }}</Badge>
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted-foreground leading-relaxed">{{ t('android.vpn_explainer') }}</p>
        <div>
          <Button :disabled="vpnRequesting" @click="onRequestVpn">
            <ShieldCheck class="size-4" />
            {{ t('android.vpn_request_permission') }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle class="text-base">{{ t('android.backup_section') }}</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <div class="flex flex-col gap-1">
          <p class="text-sm font-medium">{{ t('android.backup_export') }}</p>
          <p class="text-xs text-muted-foreground">{{ t('android.backup_export_hint') }}</p>
          <div class="mt-2">
            <Button variant="outline" :disabled="exporting" @click="onExportBackup">
              <Save class="size-4" />
              {{ t('android.backup_export') }}
            </Button>
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <p class="text-sm font-medium">{{ t('android.backup_import') }}</p>
          <p class="text-xs text-muted-foreground">{{ t('android.backup_import_hint') }}</p>
          <div class="mt-2">
            <Button variant="outline" :disabled="importing" @click="onImportBackup">
              <Upload class="size-4" />
              {{ t('android.backup_import') }}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle class="text-base flex items-center gap-2">
          <Cpu class="size-4" />
          {{ t('android.about_section') }}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl class="grid grid-cols-[max-content,1fr] gap-x-4 gap-y-1 text-sm">
          <dt class="text-muted-foreground">{{ t('android.about_version') }}</dt>
          <dd class="font-mono text-xs">{{ version || '—' }}</dd>
        </dl>
      </CardContent>
    </Card>
  </div>
</template>
