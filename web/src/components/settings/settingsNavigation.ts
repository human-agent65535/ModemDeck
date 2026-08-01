export type SettingsSection =
  | 'preferences'
  | 'security'
  | 'users'
  | 'contacts'
  | 'audio'
  | 'pairing'
  | 'devices'
  | 'telegram'
  | 'connectivity'
  | 'diagnostics'
  | 'about'

export type SettingsSectionGroup = 'personal' | 'management'

export type SettingsVisibility = {
  isAdmin: boolean
  canPairIOS: boolean
}

export const SETTINGS_SECTION_ORDER: readonly SettingsSection[] = [
  'preferences',
  'security',
  'audio',
  'pairing',
  'contacts',
  'users',
  'devices',
  'telegram',
  'connectivity',
  'diagnostics',
  'about'
]

const PERSONAL_SECTIONS = new Set<SettingsSection>([
  'preferences',
  'security',
  'audio',
  'pairing',
  'contacts'
])

const ADMIN_ONLY_SECTIONS = new Set<SettingsSection>([
  'users',
  'connectivity',
  'diagnostics',
  'about'
])

export function settingsSectionGroup(section: SettingsSection): SettingsSectionGroup {
  return PERSONAL_SECTIONS.has(section) ? 'personal' : 'management'
}

export function visibleSettingsSectionIDs(
  visibility: SettingsVisibility
): SettingsSection[] {
  return SETTINGS_SECTION_ORDER.filter(section => {
    if (section === 'pairing') return visibility.canPairIOS
    if (ADMIN_ONLY_SECTIONS.has(section)) return visibility.isAdmin
    return true
  })
}
