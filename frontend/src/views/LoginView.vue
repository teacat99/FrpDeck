<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import LanguageIcon from '@/components/LanguageIcon.vue'
import { useAuthStore } from '@/stores/auth'
import { setLocale } from '@/i18n'
import { Message } from '@/lib/toast'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Eye, EyeOff } from 'lucide-vue-next'
import { fetchCaptcha } from '@/api/auth'

const { t, locale } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

function toggleLocale() {
  setLocale(locale.value === 'zh-CN' ? 'en-US' : 'zh-CN')
}

const username = ref('')
const password = ref('')
const showPassword = ref(false)
const loading = ref(false)

// Captcha state. The backend asks for a math challenge once it's seen
// enough recent failures from this IP/user; we lazy-load on first need
// and refresh whenever the answer is rejected.
const captchaId = ref('')
const captchaQuestion = ref('')
const captchaAnswer = ref('')
const captchaRequired = ref(false)
const captchaLoading = ref(false)

async function loadCaptcha() {
  if (captchaLoading.value) return
  captchaLoading.value = true
  try {
    const c = await fetchCaptcha()
    captchaId.value = c.id
    captchaQuestion.value = c.question
    captchaAnswer.value = ''
    captchaRequired.value = true
  } catch {
    captchaRequired.value = false
  } finally {
    captchaLoading.value = false
  }
}

// Brute-force lockout state. When the backend returns 429 with
// retry_after_secs we surface an in-page banner + countdown so the user
// understands why the submit button is disabled, and we don't send any
// further requests that would just fail again.
const lockoutSecs = ref(0)
const lockoutMessage = ref('')
let lockoutTimer: ReturnType<typeof setInterval> | null = null

const submitDisabled = computed(
  () =>
    loading.value ||
    !username.value.trim() ||
    !password.value ||
    lockoutSecs.value > 0 ||
    (captchaRequired.value && !captchaAnswer.value.trim()),
)

function startLockout(secs: number, code: string) {
  if (lockoutTimer) clearInterval(lockoutTimer)
  lockoutSecs.value = secs
  lockoutMessage.value = t(`login.error.${code}`)
  lockoutTimer = setInterval(() => {
    lockoutSecs.value = Math.max(0, lockoutSecs.value - 1)
    if (lockoutSecs.value === 0 && lockoutTimer) {
      clearInterval(lockoutTimer)
      lockoutTimer = null
      lockoutMessage.value = ''
    }
  }, 1000)
}

onBeforeUnmount(() => {
  if (lockoutTimer) clearInterval(lockoutTimer)
})

onMounted(async () => {
  await auth.refreshStatus()
})

async function submit(e?: Event) {
  e?.preventDefault()
  if (submitDisabled.value) return
  loading.value = true
  try {
    await auth.login({
      username: username.value.trim(),
      password: password.value,
      captcha_id: captchaRequired.value ? captchaId.value : undefined,
      captcha_answer: captchaRequired.value ? captchaAnswer.value.trim() : undefined,
    })
    captchaRequired.value = false
    captchaAnswer.value = ''
    captchaId.value = ''
    captchaQuestion.value = ''
    const redirect = (route.query.redirect as string) || '/'
    router.replace(redirect)
  } catch (err: any) {
    const status = err?.response?.status as number | undefined
    const code = err?.response?.data?.code as string | undefined
    const english = err?.response?.data?.error as string | undefined
    const retryAfter = err?.response?.data?.retry_after_secs as number | undefined

    if (status === 429 && retryAfter && code) {
      startLockout(retryAfter, code)
      Message.error(t(`login.error.${code}`))
      // Lockout invalidates the previous challenge.
      captchaRequired.value = false
      return
    }

    const captchaRequiredFlag =
      err?.response?.data?.captcha_required === true || code === 'captcha_required'

    // The backend signals "show me a captcha now" via:
    //  - captcha_required: this attempt itself triggered the threshold
    //    OR the gate was already on and we sent nothing.
    //  - captcha_wrong: we sent an answer but it was wrong.
    // In both cases fetch a fresh challenge so the user can retry
    // without reloading the page.
    if (captchaRequiredFlag || code === 'captcha_wrong') {
      await loadCaptcha()
      // For "your password was wrong AND you must now solve a captcha",
      // surface the password message rather than the captcha one.
      const errKey =
        code === 'captcha_wrong'
          ? 'login.error.captcha_wrong'
          : code === 'captcha_required'
            ? 'login.error.captcha_required'
            : code
              ? `login.error.${code}`
              : 'login.failed'
      const errTr = t(errKey)
      Message.error(errTr === errKey ? english ?? t('login.failed') : errTr)
      return
    }

    // Backend returns `{code, error}` for auth/login failures. Prefer
    // the localised `login.error.<code>` bundle so toggling locale
    // translates the message immediately; English `error` is a fallback.
    const localisedKey = code ? `login.error.${code}` : ''
    const translated =
      localisedKey && t(localisedKey) !== localisedKey ? t(localisedKey) : undefined
    Message.error(translated ?? english ?? t('login.failed'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center p-5 relative overflow-hidden bg-background">
    <!-- Ambient brand gradient. Kept as an absolutely-positioned layer so
         the form card sits on a neutral surface while still picking up the
         brand identity. -->
    <div class="absolute inset-0 pointer-events-none" aria-hidden="true">
      <div class="absolute -top-40 -left-40 size-[600px] rounded-full bg-brand-500/20 blur-3xl"></div>
      <div class="absolute -bottom-40 -right-40 size-[600px] rounded-full bg-brand-300/20 blur-3xl"></div>
    </div>

    <div class="absolute top-3 right-3 z-10">
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
    </div>

    <div
      class="relative w-full max-w-sm rounded-lg border border-border bg-card shadow-modal p-7"
    >
      <div class="flex items-center gap-3 mb-6">
        <img src="@/assets/logo.svg" alt="FrpDeck" class="size-11 rounded-xl" />
        <div class="flex flex-col">
          <span class="font-semibold text-lg">{{ t('login.title') }}</span>
          <span class="text-xs text-muted-foreground mt-0.5">{{ t('app.subtitle') }}</span>
        </div>
      </div>

      <div
        v-if="lockoutSecs > 0"
        class="mb-4 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        role="alert"
      >
        <div class="font-medium">{{ lockoutMessage || t('login.lockedUntil') }}</div>
        <div class="mt-0.5 text-xs opacity-80">
          {{ t('login.retryIn', { seconds: lockoutSecs }) }}
        </div>
      </div>

      <form class="flex flex-col gap-4" @submit="submit">
        <div class="flex flex-col gap-1.5">
          <Label for="login-username">{{ t('login.username') }}</Label>
          <Input
            id="login-username"
            v-model="username"
            :placeholder="t('login.usernamePlaceholder')"
            autocomplete="username"
            class="h-11 text-base"
          />
        </div>

        <div class="flex flex-col gap-1.5">
          <Label for="login-password">{{ t('login.password') }}</Label>
          <div class="relative">
            <Input
              id="login-password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              :placeholder="t('login.passwordPlaceholder')"
              autocomplete="current-password"
              class="h-11 text-base pr-10"
              @keydown.enter="submit"
            />
            <button
              type="button"
              tabindex="-1"
              class="absolute inset-y-0 right-0 flex items-center px-2.5 text-muted-foreground hover:text-foreground transition-colors"
              @click="showPassword = !showPassword"
            >
              <EyeOff v-if="showPassword" class="size-4" />
              <Eye v-else class="size-4" />
            </button>
          </div>
        </div>

        <div v-if="captchaRequired" class="flex flex-col gap-1.5">
          <Label for="login-captcha">{{ t('login.captcha') }}</Label>
          <div class="flex items-center gap-2">
            <span
              class="inline-flex h-11 select-none items-center rounded-md border border-border bg-muted px-3 font-mono text-base tracking-wider text-foreground"
              aria-label="captcha"
            >{{ captchaQuestion }}</span>
            <Input
              id="login-captcha"
              v-model="captchaAnswer"
              :placeholder="t('login.captchaPlaceholder')"
              inputmode="numeric"
              autocomplete="off"
              class="h-11 text-base flex-1"
              @keydown.enter="submit"
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              :disabled="captchaLoading"
              @click="loadCaptcha"
            >{{ t('login.captchaRefresh') }}</Button>
          </div>
        </div>

        <Button
          type="submit"
          size="lg"
          class="w-full mt-2"
          :disabled="submitDisabled"
        >
          <span v-if="loading" class="inline-block size-4 rounded-full border-2 border-primary-foreground/50 border-t-transparent animate-spin" />
          <span v-if="lockoutSecs > 0">{{ t('login.retryIn', { seconds: lockoutSecs }) }}</span>
          <span v-else>{{ t('action.login') }}</span>
        </Button>
      </form>
    </div>
  </div>
</template>
