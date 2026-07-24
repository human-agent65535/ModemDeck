export type OperatorNetworkSource = {
  operator?: string
  operator_identifier?: string
  operator_name?: string
  home_operator_code?: string
  home_operator_name?: string
  serving_operator_code?: string
  serving_operator_name?: string
  registration_state_known?: boolean
  registration_state?: string
  roaming?: boolean
}

export type OperatorFact = {
  id: 'current' | 'serving' | 'home'
  label: '运营商' | '当前网络' | '归属运营商'
  value: string
}

function clean(value?: string): string {
  return value?.trim() || ''
}

export function formatOperator(name?: string, code?: string): string {
  const normalizedName = clean(name)
  const normalizedCode = clean(code)
  if (normalizedName && normalizedCode && normalizedName !== normalizedCode) {
    return `${normalizedName}（${normalizedCode}）`
  }
  return normalizedName || normalizedCode
}

export function isRoamingNetwork(source?: OperatorNetworkSource | null): boolean {
  return (
    source?.roaming === true ||
    clean(source?.registration_state).toLocaleLowerCase() === 'roaming'
  )
}

export function operatorFacts(
  source?: OperatorNetworkSource | null,
  unknownLabel = '未识别'
): OperatorFact[] {
  const home = formatOperator(
    clean(source?.home_operator_name) ||
      clean(source?.operator_name) ||
      clean(source?.operator),
    clean(source?.home_operator_code) || clean(source?.operator_identifier)
  )
  const serving = formatOperator(
    source?.serving_operator_name,
    source?.serving_operator_code
  )

  if (isRoamingNetwork(source)) {
    return [
      {
        id: 'serving',
        label: '当前网络',
        value: serving || unknownLabel
      },
      {
        id: 'home',
        label: '归属运营商',
        value: home || unknownLabel
      }
    ]
  }

  return [
    {
      id: 'current',
      label: '运营商',
      value: serving || home || unknownLabel
    }
  ]
}

export function registrationStateLabel(
  source: OperatorNetworkSource,
  fallback: string
): string {
  if (isRoamingNetwork(source)) return '漫游'
  if (!source.registration_state_known) return fallback

  switch (clean(source.registration_state).toLocaleLowerCase()) {
    case 'home':
      return '本地驻网'
    case 'searching':
      return '正在搜网'
    case 'denied':
      return '注册被拒绝'
    case 'idle':
      return '未注册'
    case 'unknown':
      return '状态未知'
    default:
      return clean(source.registration_state) || fallback
  }
}
