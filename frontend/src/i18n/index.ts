import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import enUS from './en-US'

const saved = localStorage.getItem('frpdeck.lang')
const browser = navigator.language.startsWith('zh') ? 'zh-CN' : 'en-US'

const i18n = createI18n({
  legacy: false,
  locale: saved ?? browser,
  fallbackLocale: 'en-US',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS
  }
})

export function setLocale(lang: 'zh-CN' | 'en-US') {
  i18n.global.locale.value = lang
  localStorage.setItem('frpdeck.lang', lang)
}

export default i18n
