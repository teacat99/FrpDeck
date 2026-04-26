import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

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
        path: 'settings',
        name: 'settings',
        meta: { adminOnly: true },
        component: () => import('@/views/SettingsView.vue')
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

router.beforeEach(async (to, _from, next) => {
  const auth = useAuthStore()
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
  next()
})

export default router
