import { createI18n, type LocaleMessageValue } from 'vue-i18n'
import type { SystemLanguage } from '../api/types'
import enUS from './locales/en-US'

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
let languageChangeGeneration = 0

type LocaleCatalog = Record<string, LocaleMessageValue>
type LocaleModule = { default: LocaleCatalog }
type LocaleLoader = () => Promise<LocaleModule>

const localeLoaders: Record<Exclude<ResolvedLocale, 'en-US'>, LocaleLoader> = {
  'zh-CN': () => import('./locales/zh-CN'),
  'zh-TW': () => import('./locales/zh-TW'),
  'ja-JP': () => import('./locales/ja-JP'),
  'vi-VN': () => import('./locales/vi-VN'),
  'es-ES': () => import('./locales/es-ES'),
  'de-DE': () => import('./locales/de-DE'),
  'fr-FR': () => import('./locales/fr-FR'),
  'pt-BR': () => import('./locales/pt-BR')
}
const loadedLocales = new Set<ResolvedLocale>(['en-US'])
const localeLoads = new Map<ResolvedLocale, Promise<void>>()

const initialMessages: Record<ResolvedLocale, LocaleCatalog> = {
  'zh-CN': {},
  'zh-TW': {},
  'en-US': enUS,
  'ja-JP': {},
  'vi-VN': {},
  'es-ES': {},
  'de-DE': {},
  'fr-FR': {},
  'pt-BR': {}
}

export const i18n = createI18n({
  legacy: false,
  locale: 'en-US' as ResolvedLocale,
  fallbackLocale: 'en-US',
  messages: initialMessages
})

async function loadLocale(locale: ResolvedLocale): Promise<void> {
  if (loadedLocales.has(locale)) return
  const existing = localeLoads.get(locale)
  if (existing) return existing

  const loader = locale === 'en-US' ? undefined : localeLoaders[locale]
  if (!loader) return
  const request = loader()
    .then(module => {
      i18n.global.setLocaleMessage(locale, module.default)
      loadedLocales.add(locale)
    })
    .finally(() => {
      localeLoads.delete(locale)
    })
  localeLoads.set(locale, request)
  return request
}

export async function setSystemLanguage(
  language: SystemLanguage
): Promise<ResolvedLocale> {
  configuredLanguage = language
  const resolved = resolveSystemLanguage(language)
  const generation = ++languageChangeGeneration
  if (loadedLocales.has(resolved)) {
    i18n.global.locale.value = resolved
    if (typeof document !== 'undefined') document.documentElement.lang = resolved
    return resolved
  }
  await loadLocale(resolved)
  if (generation !== languageChangeGeneration) {
    return i18n.global.locale.value
  }
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
    if (configuredLanguage === 'auto') void setSystemLanguage('auto')
  })
}
