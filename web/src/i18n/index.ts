import { createI18n } from 'vue-i18n'
import type { SystemLanguage } from '../api/types'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

export type ResolvedLocale = 'zh-CN' | 'en-US'

function browserLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages?.length ? navigator.languages : [navigator.language]
}

export function resolveSystemLanguage(
  language: SystemLanguage,
  preferredLanguages: readonly string[] = browserLanguages()
): ResolvedLocale {
  if (language === 'zh-CN' || language === 'en-US') return language
  return preferredLanguages.some(candidate => candidate.toLocaleLowerCase().startsWith('zh'))
    ? 'zh-CN'
    : 'en-US'
}

let configuredLanguage: SystemLanguage = 'auto'

export const i18n = createI18n({
  legacy: false,
  locale: resolveSystemLanguage(configuredLanguage),
  fallbackLocale: 'en-US',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS
  }
})

export function setSystemLanguage(language: SystemLanguage): ResolvedLocale {
  configuredLanguage = language
  const resolved = resolveSystemLanguage(language)
  i18n.global.locale.value = resolved
  if (typeof document !== 'undefined') document.documentElement.lang = resolved
  return resolved
}

export function systemLanguage(): SystemLanguage {
  return configuredLanguage
}

export function resolvedLocale(): ResolvedLocale {
  return i18n.global.locale.value
}

export function translate(key: string, parameters?: Record<string, unknown>): string {
  return String(i18n.global.t(key, parameters || {}))
}

if (typeof window !== 'undefined') {
  window.addEventListener('languagechange', () => {
    if (configuredLanguage === 'auto') setSystemLanguage('auto')
  })
}
