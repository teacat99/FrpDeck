import { registerSW } from 'virtual:pwa-register'
import { toast } from 'vue-sonner'

// registerPWA wires Service Worker update events to sonner toasts. Update
// prompts use a manual-dismiss toast with an action button so the user
// can choose when to reload, avoiding interrupting in-flight edits.
export function registerPWA() {
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
