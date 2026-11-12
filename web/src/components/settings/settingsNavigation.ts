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

// The translation keys of every section's name and description. The settings
// page and the mobile shell title both read them from here.
export const SETTINGS_SECTION_TEXT: Record<
  SettingsSection,
  { label: string; description: string }
> = {
  preferences: { label: 'settings.system', description: 'settings.systemDescription' },
  security: { label: 'settings.account', description: 'settings.accountDescription' },
  audio: { label: 'settings.audio', description: 'settings.audioDescription' },
  pairing: { label: 'settings.iosApp', description: 'settings.iosAppDescription' },
  contacts: { label: 'settings.contactsSync', description: 'settings.contactsSyncDescription' },
  users: { label: 'settings.users', description: 'settings.usersDescription' },
  devices: { label: 'settings.devices', description: 'settings.devicesDescription' },
  telegram: { label: 'settings.telegram', description: 'settings.telegramDescription' },
  connectivity: { label: 'settings.tls', description: 'settings.tlsDescription' },
  diagnostics: { label: 'settings.diagnostics', description: 'settings.diagnosticsDescription' },
  about: { label: 'settings.about', description: 'settings.aboutDescription' }
}

export function isSettingsSection(value: string): value is SettingsSection {
  return (SETTINGS_SECTION_ORDER as readonly string[]).includes(value)
}

// Section names used by earlier releases and where each one lives now. A name
// that has no destination for this user is not redirected: the settings page
// then reports an unknown section instead of silently moving elsewhere.
export function legacySettingsSectionDestination(
  section: string,
  visibility: SettingsVisibility
): SettingsSection | '' {
  switch (section) {
    case 'account':
      return visibility.isAdmin ? 'users' : 'preferences'
    case 'external-access':
      if (visibility.isAdmin) return 'connectivity'
      return visibility.canPairIOS ? 'pairing' : ''
    case 'web-certificate':
      return visibility.isAdmin ? 'connectivity' : ''
    case 'ios':
      return visibility.canPairIOS ? 'pairing' : ''
    default:
      return ''
  }
}
