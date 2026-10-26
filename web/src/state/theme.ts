import { ref } from 'vue'

export type ThemePreference = 'system' | 'light' | 'dark'

const storageKey = 'modemdeck.theme'
const systemDark =
  typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : undefined

function storedPreference(): ThemePreference {
  try {
    const value = localStorage.getItem(storageKey)
    return value === 'light' || value === 'dark' ? value : 'system'
  } catch {
    return 'system'
  }
}

/** Appearance is a per-browser choice, like GitHub's and Linear's theme setting. */
export const themePreference = ref<ThemePreference>(storedPreference())

function applyTheme(): void {
  const dark =
    themePreference.value === 'dark' ||
    (themePreference.value === 'system' && Boolean(systemDark?.matches))
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
}

export function setThemePreference(preference: ThemePreference): void {
  themePreference.value = preference
  try {
    if (preference === 'system') localStorage.removeItem(storageKey)
    else localStorage.setItem(storageKey, preference)
  } catch {
    // Without storage the choice still applies to this page.
  }
  applyTheme()
}

export function initializeTheme(): void {
  applyTheme()
  systemDark?.addEventListener('change', applyTheme)
}
