export type SettingsSkeletonShape =
  | 'preference-rows'
  | 'preferences'
  | 'form'
  | 'modules-one'
  | 'modules-two'
  | 'modules'
  | 'connectivity'
  | 'master-detail'
  | 'workbench'
  | 'detail-form'
  | 'diagnostics'

export function settingsSkeletonShape(section: string): SettingsSkeletonShape {
  switch (section) {
    case 'preferences':
      return 'preference-rows'
    case 'security':
      return 'form'
    case 'audio':
      return 'preferences'
    case 'users':
    case 'telegram':
      return 'master-detail'
    case 'contacts':
      return 'modules-two'
    case 'pairing':
      return 'modules-one'
    case 'connectivity':
      return 'connectivity'
    case 'about':
      return 'modules'
    case 'devices':
      return 'workbench'
    case 'diagnostics':
      return 'diagnostics'
    default:
      return 'preferences'
  }
}
