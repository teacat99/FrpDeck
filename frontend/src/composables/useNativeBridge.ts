// useNativeBridge — typed wrapper around `window.frpdeck`, the JS bridge
// the Android shell injects via WebView.addJavascriptInterface in
// FrpDeckBridge.kt.
//
// Why a composable:
//   - Single source of truth for "are we running inside the native
//     Android shell?". UI components import `isNativeBridge` to decide
//     whether to render Android-only affordances (VPN authorisation
//     button, SAF backup buttons, the orange "VPN takeover" badge on
//     visitor+socks5 tunnels).
//   - All async bridge calls go through `__resolve(reqId, payload)` —
//     the kotlin side calls `evaluateJavascript("window.frpdeck.__resolve(...)")`
//     to settle a Promise. The composable owns the in-flight map, so
//     individual pages don't have to.
//
// Browser fallback (non-Android, no native shell): every async method
// rejects synchronously with an Error so the caller can branch on a
// rejection and show the desktop equivalent (HTML <input type=file>
// for example).
//
// IMPORTANT: this module mounts a `__resolve` getter onto
// `window.frpdeck`. Because addJavascriptInterface returns a Java
// proxy object (not a plain JS object) we cannot simply assign new
// fields onto it. We work around this by holding the in-flight map in
// the module scope and exposing `__resolve` through a thin wrapper
// installed on `window` (sub-key) instead. Native side calls that
// wrapper rather than the bridge proxy itself.

interface NativeBridgeAsyncResult {
  ok: boolean
  message: string
  [key: string]: unknown
}

interface NativeBridgeSurface {
  // Synchronous discovery
  getPlatform(): string
  getVersion(): string
  getAdminToken(): string
  getServerBase(): string
  isVpnPermissionGranted(): boolean
  // Async — kotlin resolves via window.__frpdeckResolve(reqId, payload)
  requestVpnPermission(reqId: string): void
  exportBackup(reqId: string, suggestedName: string): void
  importBackup(reqId: string): void
  openExternal(url: string): void
}

declare global {
  interface Window {
    frpdeck?: NativeBridgeSurface & { __resolve?: (reqId: string, payload: NativeBridgeAsyncResult) => void }
    __frpdeckResolve?: (reqId: string, payload: NativeBridgeAsyncResult) => void
  }
}

const pending = new Map<string, (payload: NativeBridgeAsyncResult) => void>()

function ensureResolverInstalled() {
  if (typeof window === 'undefined') return
  if (window.__frpdeckResolve) return
  const resolver = (reqId: string, payload: NativeBridgeAsyncResult) => {
    const cb = pending.get(reqId)
    if (cb) {
      pending.delete(reqId)
      cb(payload)
    }
  }
  window.__frpdeckResolve = resolver
  // Best-effort: also expose under the bridge namespace so the kotlin
  // FrpDeckBridge.resolve() helper can hit either path.
  if (window.frpdeck) {
    try {
      ;(window.frpdeck as unknown as Record<string, unknown>).__resolve = resolver
    } catch {
      /* addJavascriptInterface proxies reject extra fields — fall back
         to the window-level resolver, which kotlin also calls. */
    }
  }
}

function nextReqId(): string {
  return `req_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`
}

function call<T extends NativeBridgeAsyncResult>(
  invoke: (reqId: string) => void,
  timeoutMs = 60_000,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    if (!isNativeBridge()) {
      reject(new Error('native bridge unavailable'))
      return
    }
    ensureResolverInstalled()
    const reqId = nextReqId()
    const timer = window.setTimeout(() => {
      if (pending.has(reqId)) {
        pending.delete(reqId)
        reject(new Error('native bridge timeout'))
      }
    }, timeoutMs)
    pending.set(reqId, (payload) => {
      window.clearTimeout(timer)
      if (payload.ok) {
        resolve(payload as T)
      } else {
        reject(new Error(payload.message || 'native bridge error'))
      }
    })
    try {
      invoke(reqId)
    } catch (err) {
      window.clearTimeout(timer)
      pending.delete(reqId)
      reject(err instanceof Error ? err : new Error(String(err)))
    }
  })
}

export function isNativeBridge(): boolean {
  if (typeof window === 'undefined') return false
  const b = window.frpdeck
  return !!b && typeof b.getPlatform === 'function'
}

export function nativePlatform(): string {
  if (!isNativeBridge()) return ''
  try {
    return window.frpdeck!.getPlatform() ?? ''
  } catch {
    return ''
  }
}

export function nativeIsAndroid(): boolean {
  return nativePlatform() === 'android'
}

export function nativeVersion(): string {
  if (!isNativeBridge()) return ''
  try {
    return window.frpdeck!.getVersion() ?? ''
  } catch {
    return ''
  }
}

export function nativeAdminToken(): string {
  if (!isNativeBridge()) return ''
  try {
    return window.frpdeck!.getAdminToken() ?? ''
  } catch {
    return ''
  }
}

export function nativeIsVpnPermissionGranted(): boolean {
  if (!isNativeBridge()) return false
  try {
    return window.frpdeck!.isVpnPermissionGranted()
  } catch {
    return false
  }
}

export function requestVpnPermission(): Promise<NativeBridgeAsyncResult> {
  return call((reqId) => window.frpdeck!.requestVpnPermission(reqId))
}

export function exportBackup(suggestedName = 'frpdeck-backup.zip'): Promise<NativeBridgeAsyncResult> {
  return call((reqId) => window.frpdeck!.exportBackup(reqId, suggestedName))
}

export function importBackup(): Promise<NativeBridgeAsyncResult> {
  return call((reqId) => window.frpdeck!.importBackup(reqId))
}

export function openExternal(url: string): void {
  if (!isNativeBridge()) {
    if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener')
    return
  }
  try {
    window.frpdeck!.openExternal(url)
  } catch {
    if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener')
  }
}

/**
 * Bootstrap the Android admin token into localStorage so the SPA skips
 * /login when running inside the native shell. Call exactly once early
 * in app start — main.ts does this before pinia + router boot.
 *
 * No-op when the bridge is absent (desktop/browser): the user keeps
 * the regular interactive login.
 */
export function bootstrapNativeAdminToken(): void {
  if (!isNativeBridge()) return
  try {
    const existing = localStorage.getItem('frpdeck.token') || ''
    if (existing) return
    const token = nativeAdminToken()
    if (token) localStorage.setItem('frpdeck.token', token)
  } catch {
    /* private mode or quota — ignore, the user will see /login */
  }
}
