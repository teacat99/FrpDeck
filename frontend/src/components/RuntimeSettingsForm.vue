<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bell, RotateCcw, Save, Send, Shield, ShieldCheck, Sliders } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Message } from '@/lib/toast'
import {
  fetchRuntimeSettings,
  testNotify,
  updateRuntimeSettings,
  type RuntimeBundle,
  type RuntimeSettings,
} from '@/api/runtime'

const { t } = useI18n()

// Notifies the parent (SettingsView) when persisted values change so that
// the overview cards above the tab strip can re-fetch /api/settings and
// stay in sync. Without this the stat tiles would show stale env defaults.
const emit = defineEmits<{ (e: 'saved'): void }>()

// Mirror the wire shape but keep every field stringy so the inputs can
// distinguish "not yet edited" from "0", and so we can submit only the
// keys the operator actually changed.
type FormState = Record<keyof RuntimeSettings, string>

const bundle = ref<RuntimeBundle | null>(null)
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)

const form = reactive<FormState>(emptyForm())

function emptyForm(): FormState {
  return {
    max_duration_hours: '',
    history_retention_days: '',
    tunnel_expiring_notify_minutes: '',
    max_rules_per_ip: '',
    rate_limit_per_minute_per_ip: '',
    login_fail_max_per_ip: '',
    login_fail_window_ip_min: '',
    login_fail_max_per_user: '',
    login_fail_window_user_min: '',
    login_lockout_ip_min: '',
    login_lockout_user_min: '',
    login_min_password_len: '',
    login_fail_subnet_bits: '',
    captcha_threshold: '',
    ntfy_url: '',
    ntfy_topic: '',
    ntfy_token: '',
  }
}

function syncFormFromBundle(b: RuntimeBundle) {
  for (const k of Object.keys(form) as (keyof FormState)[]) {
    const v = (b.settings as any)[k]
    form[k] = v == null ? '' : String(v)
  }
  // Token field stays empty when the server returned a redacted blob;
  // any non-empty value the user types is treated as a new write. This
  // means the masked value is shown as a placeholder, not in the input.
  if (form.ntfy_token && form.ntfy_token.includes('****')) {
    form.ntfy_token = ''
  }
}

async function reload() {
  loading.value = true
  try {
    bundle.value = await fetchRuntimeSettings()
    syncFormFromBundle(bundle.value)
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.loadFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(reload)

// Build the diff between the current form and the last bundle so the
// API only re-validates what actually changed. ntfy_token is special:
// the server never returns it verbatim, so any non-empty value is sent.
const diff = computed<Record<string, string>>(() => {
  if (!bundle.value) return {}
  const out: Record<string, string> = {}
  for (const k of Object.keys(form) as (keyof FormState)[]) {
    const cur = String((bundle.value.settings as any)[k] ?? '')
    // Number-typed inputs emit `number` via v-model, so coerce back to
    // string here. The backend PUT handler binds JSON into a
    // map[string]string and would 400 on a bare number/null.
    const rawNext = form[k]
    const next = rawNext == null ? '' : String(rawNext)
    if (k === 'ntfy_token') {
      if (next !== '') out[k] = next
      continue
    }
    if (next !== cur) out[k] = next
  }
  return out
})

const dirty = computed(() => Object.keys(diff.value).length > 0)

async function save() {
  if (!dirty.value || saving.value) return
  saving.value = true
  try {
    bundle.value = await updateRuntimeSettings(diff.value)
    syncFormFromBundle(bundle.value)
    emit('saved')
    Message.success(t('settings.runtime.saved'))
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('msg.invalidInput'))
  } finally {
    saving.value = false
  }
}

function reset() {
  if (bundle.value) syncFormFromBundle(bundle.value)
}

async function runTestNotify() {
  testing.value = true
  try {
    await testNotify()
    Message.success(t('settings.notify.testOk'))
  } catch (e: any) {
    Message.error(e?.response?.data?.error ?? t('settings.notify.testFail'))
  } finally {
    testing.value = false
  }
}

// Field grouping. Kept declarative so we can render rule-limits,
// login hardening and ntfy as separate cards without copy-pasting markup.
interface Field {
  key: keyof FormState
  type?: 'number' | 'text'
  // i18n keys for label and help; help shown as muted text below input.
  label: string
  help?: string
  unit?: string
  placeholder?: string
}

const ruleLimits: Field[] = [
  { key: 'max_duration_hours', type: 'number', label: 'settings.runtime.maxDurationHours', help: 'settings.runtime.maxDurationHoursHelp', unit: 'h' },
  { key: 'history_retention_days', type: 'number', label: 'settings.runtime.historyRetentionDays', help: 'settings.runtime.historyRetentionDaysHelp', unit: 'd' },
  { key: 'tunnel_expiring_notify_minutes', type: 'number', label: 'settings.runtime.tunnelExpiringNotifyMinutes', help: 'settings.runtime.tunnelExpiringNotifyMinutesHelp', unit: 'min' },
  { key: 'max_rules_per_ip', type: 'number', label: 'settings.runtime.maxRulesPerIP', help: 'settings.runtime.maxRulesPerIPHelp' },
  { key: 'rate_limit_per_minute_per_ip', type: 'number', label: 'settings.runtime.rateLimit', help: 'settings.runtime.rateLimitHelp', unit: '/min' },
]

const loginHardening: Field[] = [
  { key: 'login_fail_max_per_ip', type: 'number', label: 'settings.runtime.failMaxIP', help: 'settings.runtime.failMaxIPHelp' },
  { key: 'login_fail_window_ip_min', type: 'number', label: 'settings.runtime.failWindowIP', help: 'settings.runtime.failWindowIPHelp', unit: 'min' },
  { key: 'login_fail_max_per_user', type: 'number', label: 'settings.runtime.failMaxUser', help: 'settings.runtime.failMaxUserHelp' },
  { key: 'login_fail_window_user_min', type: 'number', label: 'settings.runtime.failWindowUser', help: 'settings.runtime.failWindowUserHelp', unit: 'min' },
  { key: 'login_lockout_ip_min', type: 'number', label: 'settings.runtime.lockoutIP', help: 'settings.runtime.lockoutIPHelp', unit: 'min' },
  { key: 'login_lockout_user_min', type: 'number', label: 'settings.runtime.lockoutUser', help: 'settings.runtime.lockoutUserHelp', unit: 'min' },
  { key: 'login_min_password_len', type: 'number', label: 'settings.runtime.minPwd', help: 'settings.runtime.minPwdHelp' },
]

const optionalDefence: Field[] = [
  { key: 'login_fail_subnet_bits', type: 'number', label: 'settings.runtime.subnetBits', help: 'settings.runtime.subnetBitsHelp' },
  { key: 'captcha_threshold', type: 'number', label: 'settings.runtime.captchaThreshold', help: 'settings.runtime.captchaThresholdHelp' },
]

const ntfyFields: Field[] = [
  { key: 'ntfy_url', type: 'text', label: 'settings.runtime.ntfyURL', help: 'settings.runtime.ntfyURLHelp', placeholder: 'https://ntfy.sh' },
  { key: 'ntfy_topic', type: 'text', label: 'settings.runtime.ntfyTopic', help: 'settings.runtime.ntfyTopicHelp', placeholder: 'frpdeck-alerts' },
  { key: 'ntfy_token', type: 'text', label: 'settings.runtime.ntfyToken', help: 'settings.runtime.ntfyTokenHelp', placeholder: '••••' },
]

const ntfyTokenPlaceholder = computed(() => {
  const masked = (bundle.value?.settings.ntfy_token ?? '') as string
  return masked || '••••'
})
</script>

<template>
  <div class="flex flex-col gap-5">
    <Alert variant="info">
      <AlertDescription>
        {{ t('settings.runtime.intro') }}
      </AlertDescription>
    </Alert>

    <!-- Save/reset action bar — sticky-ish on the page so changes feel committable -->
    <div
      class="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/40 px-3 py-2"
    >
      <div class="flex items-center gap-2 min-w-0">
        <Sliders class="size-4 text-muted-foreground shrink-0" />
        <span v-if="dirty" class="text-sm text-foreground">
          {{ t('settings.runtime.dirty', { n: Object.keys(diff).length }) }}
        </span>
        <span v-else class="text-sm text-muted-foreground">
          {{ t('settings.runtime.clean') }}
        </span>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="ghost" size="sm" :disabled="!dirty || saving" @click="reset">
          <RotateCcw class="size-3.5" />
          {{ t('action.reset') }}
        </Button>
        <Button size="sm" :disabled="!dirty || saving" @click="save">
          <Save class="size-3.5" />
          {{ saving ? t('action.saving') : t('action.save') }}
        </Button>
      </div>
    </div>

    <!-- Rule limits -->
    <section class="rounded-lg border border-border bg-card p-4 flex flex-col gap-3">
      <header class="flex items-center gap-2">
        <Sliders class="size-4 text-primary" />
        <h4 class="text-sm font-semibold">{{ t('settings.runtime.sectionRuleLimits') }}</h4>
      </header>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-x-4 gap-y-3">
        <div v-for="f in ruleLimits" :key="f.key" class="flex flex-col gap-1.5">
          <Label :for="`rt-${f.key}`" class="text-xs font-medium">{{ t(f.label) }}</Label>
          <div class="flex items-center gap-2">
            <Input
              :id="`rt-${f.key}`"
              v-model="form[f.key]"
              :type="f.type"
              :placeholder="f.placeholder"
              class="h-9"
            />
            <span v-if="f.unit" class="text-xs text-muted-foreground shrink-0">{{ f.unit }}</span>
          </div>
          <span v-if="f.help" class="text-xs text-muted-foreground">{{ t(f.help) }}</span>
        </div>
      </div>
    </section>

    <!-- Login hardening -->
    <section class="rounded-lg border border-border bg-card p-4 flex flex-col gap-3">
      <header class="flex items-center gap-2">
        <ShieldCheck class="size-4 text-primary" />
        <h4 class="text-sm font-semibold">{{ t('settings.runtime.sectionLogin') }}</h4>
      </header>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-x-4 gap-y-3">
        <div v-for="f in loginHardening" :key="f.key" class="flex flex-col gap-1.5">
          <Label :for="`rt-${f.key}`" class="text-xs font-medium">{{ t(f.label) }}</Label>
          <div class="flex items-center gap-2">
            <Input
              :id="`rt-${f.key}`"
              v-model="form[f.key]"
              :type="f.type"
              class="h-9"
            />
            <span v-if="f.unit" class="text-xs text-muted-foreground shrink-0">{{ f.unit }}</span>
          </div>
          <span v-if="f.help" class="text-xs text-muted-foreground">{{ t(f.help) }}</span>
        </div>
      </div>
    </section>

    <!-- Optional defences (subnet + captcha) -->
    <section class="rounded-lg border border-border bg-card p-4 flex flex-col gap-3">
      <header class="flex items-center gap-2">
        <Shield class="size-4 text-primary" />
        <h4 class="text-sm font-semibold">{{ t('settings.runtime.sectionDefence') }}</h4>
      </header>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-x-4 gap-y-3">
        <div v-for="f in optionalDefence" :key="f.key" class="flex flex-col gap-1.5">
          <Label :for="`rt-${f.key}`" class="text-xs font-medium">{{ t(f.label) }}</Label>
          <Input
            :id="`rt-${f.key}`"
            v-model="form[f.key]"
            :type="f.type"
            class="h-9"
          />
          <span v-if="f.help" class="text-xs text-muted-foreground">{{ t(f.help) }}</span>
        </div>
      </div>
    </section>

    <!-- ntfy notifications -->
    <section class="rounded-lg border border-border bg-card p-4 flex flex-col gap-3">
      <header class="flex items-center justify-between gap-2">
        <div class="flex items-center gap-2">
          <Bell class="size-4 text-primary" />
          <h4 class="text-sm font-semibold">{{ t('settings.runtime.sectionNotify') }}</h4>
        </div>
        <Button variant="outline" size="sm" :disabled="testing || !form.ntfy_topic" @click="runTestNotify">
          <Send class="size-3.5" />
          {{ testing ? t('settings.notify.testing') : t('settings.notify.test') }}
        </Button>
      </header>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-x-4 gap-y-3">
        <div v-for="f in ntfyFields" :key="f.key" class="flex flex-col gap-1.5">
          <Label :for="`rt-${f.key}`" class="text-xs font-medium">{{ t(f.label) }}</Label>
          <Input
            :id="`rt-${f.key}`"
            v-model="form[f.key]"
            :type="f.type"
            :placeholder="f.key === 'ntfy_token' ? ntfyTokenPlaceholder : f.placeholder"
            class="h-9 font-mono text-xs"
          />
          <span v-if="f.help" class="text-xs text-muted-foreground">{{ t(f.help) }}</span>
        </div>
      </div>
    </section>

    <!-- Read-only system info -->
    <section v-if="bundle?.system" class="rounded-lg border border-border bg-card p-4 flex flex-col gap-3">
      <header class="flex items-center gap-2">
        <Sliders class="size-4 text-muted-foreground" />
        <h4 class="text-sm font-semibold">{{ t('settings.runtime.sectionSystem') }}</h4>
      </header>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-y-2 gap-x-4 text-sm">
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">FRPDECK_LISTEN</span>
          <code class="font-mono text-xs">{{ bundle.system.listen }}</code>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">FRPDECK_DATA_DIR</span>
          <code class="font-mono text-xs">{{ bundle.system.data_dir }}</code>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">FRPCD_DRIVER</span>
          <code class="font-mono text-xs">{{ bundle.system.driver }}</code>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">FRP_VERSION</span>
          <code class="font-mono text-xs">{{ bundle.system.frp_version }}</code>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">AUTH_MODE</span>
          <code class="font-mono text-xs">{{ bundle.system.auth_mode }}</code>
        </div>
        <div class="flex items-center gap-2 md:col-span-2">
          <span class="text-xs text-muted-foreground w-32 shrink-0">TRUSTED_PROXIES</span>
          <div class="flex flex-wrap gap-1">
            <Badge
              v-for="p in bundle.system.trusted_proxies"
              :key="p"
              variant="default"
              class="text-[10px] font-mono"
            >{{ p }}</Badge>
            <span v-if="!bundle.system.trusted_proxies?.length" class="text-xs text-muted-foreground">
              {{ t('settings.runtime.notConfigured') }}
            </span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
