import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './assets/globals.css'

import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { registerPWA } from './pwa'
import { useThemeStore } from './stores/theme'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)

// Initialise theme before mounting so the first paint already matches the
// user's preferred / persisted scheme — avoids the white flash.
useThemeStore().init()

app.mount('#app')

registerPWA()
