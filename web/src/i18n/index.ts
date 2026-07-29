import { createI18n } from 'vue-i18n'
import type { SystemLanguage } from '../api/types'
import deDE from './locales/de-DE'
import enUS from './locales/en-US'
import esES from './locales/es-ES'
import frFR from './locales/fr-FR'
import jaJP from './locales/ja-JP'
import ptBR from './locales/pt-BR'
import viVN from './locales/vi-VN'
import zhCN from './locales/zh-CN'
import zhTW from './locales/zh-TW'

export type ResolvedLocale = Exclude<SystemLanguage, 'auto'>

function browserLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages?.length ? navigator.languages : [navigator.language]
}

export function resolveSystemLanguage(
  language: SystemLanguage,
  preferredLanguages: readonly string[] = browserLanguages()
): ResolvedLocale {
  if (language !== 'auto') return language
  for (const candidate of preferredLanguages) {
    const normalized = candidate.toLocaleLowerCase()
    if (
      normalized.startsWith('zh-tw') ||
      normalized.startsWith('zh-hant') ||
      normalized.startsWith('zh-hk') ||
      normalized.startsWith('zh-mo')
    ) {
      return 'zh-TW'
    }
    if (normalized.startsWith('zh')) return 'zh-CN'
    if (normalized.startsWith('en')) return 'en-US'
    if (normalized.startsWith('ja')) return 'ja-JP'
    if (normalized.startsWith('vi')) return 'vi-VN'
    if (normalized.startsWith('es')) return 'es-ES'
    if (normalized.startsWith('de')) return 'de-DE'
    if (normalized.startsWith('fr')) return 'fr-FR'
    if (normalized.startsWith('pt')) return 'pt-BR'
  }
  return 'en-US'
}

let configuredLanguage: SystemLanguage = 'auto'

export const i18n = createI18n({
  legacy: false,
  locale: resolveSystemLanguage(configuredLanguage),
  fallbackLocale: 'en-US',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS,
    'ja-JP': jaJP,
    'vi-VN': viVN,
    'zh-TW': zhTW,
    'es-ES': esES,
    'de-DE': deDE,
    'fr-FR': frFR,
    'pt-BR': ptBR
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
