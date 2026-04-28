import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import { isNativeBridge, nativeIsAndroid } from '@/composables/useNativeBridge'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('@/layouts/AppLayout.vue'),
    children: [
      { path: '', name: 'home', component: () => import('@/views/HomeView.vue') },
      { path: 'endpoints', name: 'endpoints', component: () => import('@/views/EndpointsView.vue') },
      { path: 'tunnels', name: 'tunnels', component: () => import('@/views/TunnelsView.vue') },
      { path: 'history', name: 'history', component: () => import('@/views/HistoryView.vue') },
      {
        path: 'remote',
        name: 'remote',
        meta: { adminOnly: true },
        component: () => import('@/views/RemoteNodesView.vue')
      },
      {
        path: 'profiles',
        name: 'profiles',
        meta: { adminOnly: true },
        component: () => import('@/views/ProfilesView.vue')
      },
      {
        path: 'settings',
        name: 'settings',
        meta: { adminOnly: true },
        component: () => import('@/views/SettingsView.vue')
      },
      {
        path: 'android',
        name: 'android',
        meta: { nativeOnly: true },
        component: () => import('@/views/AndroidSettingsView.vue')
      },
      { path: 'rules', redirect: { name: 'tunnels' } },
      { path: 'users', redirect: { name: 'settings' } }
    ]
  },
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue') }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// Hydrate auth status exactly once per page-load so the guard doesn't
// rely on the store's default values (mode='password', required=true)
// — otherwise an `auth_mode=none` deployment force-redirects every
// route to /login because `required` is still true at first render.
let statusHydrated = false

router.beforeEach(async (to, _from, next) => {
  const auth = useAuthStore()

  if (!statusHydrated) {
    await auth.refreshStatus()
    statusHydrated = true
  }

  // Remote-management auto-login: when a peer FrpDeck opens our UI
  // through an stcp tunnel it appends `?_redeem=<mgmt_token>` to the
  // landing URL. We swap that for a regular session JWT, drop the
  // sensitive query string from the URL, and continue to the requested
  // route. Failure falls back to the normal login redirect below.
  const redeemToken =
    typeof to.query._redeem === 'string' ? (to.query._redeem as string) : ''
  if (redeemToken) {
    try {
      await auth.refreshStatus()
      await auth.redeemMgmtToken(redeemToken)
    } catch (err) {
      const msg = (err as { response?: { data?: { error?: string } }; message?: string })?.response?.data?.error
        ?? (err as Error)?.message
        ?? 'redeem failed'
      const { Message } = await import('@/lib/toast')
      const i18n = await import('@/i18n')
      const t = i18n.default.global.t as (k: string, p?: Record<string, unknown>) => string
      Message.error(t('remote.auto_login_failed', { msg }))
    }
    const cleanQuery = { ...to.query }
    delete cleanQuery._redeem
    next({ path: to.path, query: cleanQuery, hash: to.hash, replace: true })
    return
  }

  // Short-circuit before LoginView ever mounts. There are three ways
  // a navigation can land on /login:
  //   1. Direct URL entry / bookmark
  //   2. router.push from elsewhere in the app
  //   3. axios 401 interceptor calling location.assign('/login')
  //      (whole-page reload)
  // In all three cases the previous LoginView.onMounted fallback used
  // to flash the form for a tick before redirecting. Doing the
  // redirect here keeps the landing surface deterministic — when the
  // backend reports `auth_mode == none` the operator never sees the
  // form. We re-fetch status when the bearer is empty so a server-side
  // mode flip (password→none after admin tweaks settings + user
  // logged out) is picked up without forcing a manual reload.
  if (to.name === 'login' && !auth.token) {
    await auth.refreshStatus()
  }
  if (to.name === 'login' && !auth.required) {
    const fallback =
      (typeof to.query.redirect === 'string' && to.query.redirect) || '/'
    next(fallback)
    return
  }

  if (to.name !== 'login' && auth.required && !auth.token) {
    next({ name: 'login', query: { redirect: to.fullPath } })
    return
  }
  if (to.name !== 'login' && auth.token && !auth.me) {
    await auth.fetchMe()
  }
  if (to.meta?.adminOnly && auth.me && auth.me.role !== 'admin') {
    next({ name: 'home' })
    return
  }
  // Native-only routes (e.g. AndroidSettingsView) require running
  // inside the Android shell. Browser / desktop visitors are bounced
  // back to home so they don't see a non-functional page.
  if (to.meta?.nativeOnly && !(isNativeBridge() && nativeIsAndroid())) {
    next({ name: 'home' })
    return
  }
  // Bring the realtime channel up as soon as we know the user is
  // authenticated. ensureConnected is idempotent and a no-op without
  // a token, so doing it on every guard run is safe.
  if (auth.token) {
    useRealtimeStore().ensureConnected()
  }
  next()
})

export default router
