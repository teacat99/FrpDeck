import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'

// useThemeStore controls the dark/light/auto colour scheme. The user's
// explicit choice is persisted to localStorage; "auto" follows the system
// preference and reacts live to OS-level changes (prefers-color-scheme).
//
// Tailwind v4 reads a `.dark` class on the <html> element (see globals.css
// @custom-variant dark) so that's what we toggle here. Every hand-rolled
// CSS variable in :root / .dark flips in lockstep.

export type ThemeMode = 'light' | 'dark' | 'auto'

const STORAGE_KEY = 'frpdeck.theme'

function readPersisted(): ThemeMode {
  const v = localStorage.getItem(STORAGE_KEY)
  if (v === 'light' || v === 'dark' || v === 'auto') return v
  return 'auto'
}

function systemPrefersDark(): boolean {
  return typeof window !== 'undefined'
    && window.matchMedia
    && window.matchMedia('(prefers-color-scheme: dark)').matches
}

export const useThemeStore = defineStore('theme', () => {
  const mode = ref<ThemeMode>(readPersisted())
  const systemDark = ref<boolean>(systemPrefersDark())

  // Actual rendered scheme after resolving "auto".
  const isDark = computed<boolean>(() =>
    mode.value === 'dark' || (mode.value === 'auto' && systemDark.value)
  )

  function apply() {
    const root = document.documentElement
    if (isDark.value) {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
    // Drive the browser chrome colour (status bar / address bar) too.
    document.querySelectorAll('meta[name="theme-color"]').forEach(meta => {
      meta.setAttribute('content', isDark.value ? '#0f1216' : '#f6f8fb')
    })
  }

  function setMode(next: ThemeMode) {
    mode.value = next
    localStorage.setItem(STORAGE_KEY, next)
  }

  function toggle() {
    const order: ThemeMode[] = ['auto', 'light', 'dark']
    const idx = order.indexOf(mode.value)
    setMode(order[(idx + 1) % 3])
  }

  function init() {
    if (typeof window !== 'undefined' && window.matchMedia) {
      const mql = window.matchMedia('(prefers-color-scheme: dark)')
      const handler = (e: MediaQueryListEvent) => { systemDark.value = e.matches }
      try { mql.addEventListener('change', handler) }
      catch { mql.addListener(handler) /* Safari < 14 */ }
    }
    apply()
  }

  watch(isDark, () => apply())

  return { mode, isDark, setMode, toggle, init }
})
