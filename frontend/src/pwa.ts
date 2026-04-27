import { registerSW } from 'virtual:pwa-register'
import { toast } from 'vue-sonner'
import { isNativeBridge } from '@/composables/useNativeBridge'

// registerPWA wires Service Worker update events to sonner toasts. Update
// prompts use a manual-dismiss toast with an action button so the user
// can choose when to reload, avoiding interrupting in-flight edits.
//
// We deliberately skip registration when running inside the Android
// shell. The shell loads the SPA from `http://127.0.0.1:18080/` which is
// the embedded gomobile server — there is no offline benefit, and a
// stale precache would be actively harmful: an APK upgrade ships a new
// dist but an old SW would keep serving the previous bundle until two
// reloads. We also proactively unregister any leftover registrations
// from prior installs.
export function registerPWA() {
  if (isNativeBridge()) {
    void unregisterAndPurge()
    return
  }
  const updateSW = registerSW({
    onNeedRefresh() {
      toast.info('New version available', {
        description: 'Reload to pick up the latest changes.',
        duration: Infinity,
        action: {
          label: 'Reload',
          onClick: () => updateSW(true)
        }
      })
    },
    onOfflineReady() {
      toast.success('FrpDeck is ready to work offline', { duration: 2000 })
    }
  })
}

async function unregisterAndPurge(): Promise<void> {
  try {
    if ('serviceWorker' in navigator) {
      const regs = await navigator.serviceWorker.getRegistrations()
      await Promise.all(regs.map((r) => r.unregister()))
    }
    if ('caches' in window) {
      const keys = await caches.keys()
      await Promise.all(keys.map((k) => caches.delete(k)))
    }
  } catch {
    /* ignore — best-effort cleanup */
  }
}
