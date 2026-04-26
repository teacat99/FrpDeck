import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { authStatus, getMe, login as apiLogin, type LastLoginInfo, type LoginPayload } from '@/api/auth'
import { exchangeMgmtToken } from '@/api/remote'
import type { Me, Role } from '@/api/types'

// useAuthStore centralises everything the UI needs about the current
// session: the mode enforced by the backend, the bearer token, and the
// authenticated principal (id / username / role). Consumers should treat
// the `me` ref as the single source of truth for role-based gating.
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem('frpdeck.token') ?? '')
  const mode = ref<string>('password')
  const required = ref<boolean>(true)
  const me = ref<Me | null>(null)
  // `lastLogin` is populated from the /auth/login response so the Home
  // dashboard can show "last signed in from X at Y" — a lightweight way
  // for real users to spot unauthorised access to their account.
  const lastLogin = ref<LastLoginInfo | null>(null)

  const isAdmin = computed<boolean>(() => me.value?.role === 'admin')
  const role = computed<Role | null>(() => me.value?.role ?? null)

  async function refreshStatus() {
    try {
      const s = await authStatus()
      mode.value = s.mode
      required.value = s.required
    } catch {
      // Leave defaults; unauthenticated users will be redirected by guard.
    }
  }

  async function fetchMe() {
    try {
      me.value = await getMe()
    } catch {
      me.value = null
    }
  }

  async function login(payload: LoginPayload) {
    const resp = await apiLogin(payload)
    token.value = resp.token
    localStorage.setItem('frpdeck.token', resp.token)
    lastLogin.value = resp.last_login ?? null
    me.value = {
      id: 0, // real id comes from /auth/me; login response has no id yet
      username: resp.username,
      role: resp.role,
      auth_mode: 'password'
    }
    await fetchMe()
  }

  // redeemMgmtToken swaps a management-token (issued by a peer FrpDeck
  // through an invitation) for a regular session token on this
  // instance. Used by the router guard when the URL carries
  // ?_redeem=<mgmt_token> so the operator lands directly inside the
  // peer's UI without a manual login round-trip.
  async function redeemMgmtToken(mgmtToken: string) {
    const resp = await exchangeMgmtToken(mgmtToken)
    token.value = resp.token
    localStorage.setItem('frpdeck.token', resp.token)
    me.value = {
      id: 0,
      username: resp.username,
      role: resp.role,
      auth_mode: 'password'
    }
    await fetchMe()
  }

  function logout() {
    token.value = ''
    me.value = null
    lastLogin.value = null
    localStorage.removeItem('frpdeck.token')
    // Tear down the realtime channel here rather than at the call site
    // so every logout path (manual button, 401 interceptor, page
    // unmount) gets the same treatment. We `import()` lazily so the
    // realtime store doesn't appear before pinia is initialised.
    void import('./realtime').then(({ useRealtimeStore }) => {
      useRealtimeStore().disconnect()
    })
  }

  return {
    token,
    mode,
    required,
    me,
    lastLogin,
    isAdmin,
    role,
    refreshStatus,
    fetchMe,
    login,
    redeemMgmtToken,
    logout
  }
})
