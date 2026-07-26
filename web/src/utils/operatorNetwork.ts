export type OperatorNetworkSource = {
  state?: string
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
  emergency_only?: boolean
}

export type OperatorFact = {
  id: 'current' | 'serving' | 'home'
  label: string
  value: string
}

type Translator = (key: string) => string

const registeredStates = new Set([
  'home',
  'roaming',
  'home-sms-only',
  'roaming-sms-only',
  'home-csfb-not-preferred',
  'roaming-csfb-not-preferred',
  'attached-rlos'
])

const voiceReadyStates = new Set([
  'home',
  'roaming',
  'home-csfb-not-preferred',
  'roaming-csfb-not-preferred'
])

const messagingReadyStates = new Set([
  ...voiceReadyStates,
  'home-sms-only',
  'roaming-sms-only'
])

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
  const registrationState = clean(
    source?.registration_state
  ).toLocaleLowerCase()
  return (
    source?.roaming === true ||
    registrationState === 'roaming' ||
    registrationState === 'roaming-sms-only' ||
    registrationState === 'roaming-csfb-not-preferred'
  )
}

export function isRegisteredNetwork(
  source?: OperatorNetworkSource | null
): boolean {
  if (!source) return false
  if (source.registration_state_known) {
    return registeredStates.has(
      clean(source.registration_state).toLocaleLowerCase()
    )
  }
  return ['registered', 'connected'].includes(
    clean(source.state).toLocaleLowerCase()
  )
}

function isServiceReady(
  source: OperatorNetworkSource | null | undefined,
  readyStates: ReadonlySet<string>
): boolean {
  if (!source || source.emergency_only) return false
  if (source.registration_state_known) {
    return readyStates.has(
      clean(source.registration_state).toLocaleLowerCase()
    )
  }
  return ['registered', 'connected'].includes(
    clean(source.state).toLocaleLowerCase()
  )
}

export function isVoiceServiceReady(
  source?: OperatorNetworkSource | null
): boolean {
  return isServiceReady(source, voiceReadyStates)
}

export function isMessagingServiceReady(
  source?: OperatorNetworkSource | null
): boolean {
  return isServiceReady(source, messagingReadyStates)
}

export function operatorFacts(
  source?: OperatorNetworkSource | null,
  unknownLabel = '未识别',
  translator?: Translator
): OperatorFact[] {
  const label = (key: string, fallback: string) => translator?.(key) || fallback
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

  if (!isRegisteredNetwork(source)) {
    return [
      {
        id: 'home',
        label: label('network.homeOperator', '归属运营商'),
        value: home || unknownLabel
      }
    ]
  }

  if (isRoamingNetwork(source)) {
    return [
      {
        id: 'serving',
        label: label('network.currentNetwork', '当前网络'),
        value: serving || unknownLabel
      },
      {
        id: 'home',
        label: label('network.homeOperator', '归属运营商'),
        value: home || unknownLabel
      }
    ]
  }

  return [
    {
      id: 'current',
      label: label('network.operator', '运营商'),
      value: serving || home || unknownLabel
    }
  ]
}

export function registrationStateLabel(
  source: OperatorNetworkSource,
  fallback: string,
  translator?: Translator
): string {
  const label = (key: string, defaultLabel: string) => translator?.(key) || defaultLabel
  if (isRoamingNetwork(source)) return label('network.roaming', '漫游')
  if (!source.registration_state_known) return fallback
  if (
    source.emergency_only &&
    clean(source.registration_state).toLocaleLowerCase() !== 'searching'
  ) {
    return label('network.emergencyOnly', '仅限紧急呼叫')
  }

  switch (clean(source.registration_state).toLocaleLowerCase()) {
    case 'home':
      return label('network.home', '本地驻网')
    case 'home-sms-only':
      return label('network.homeSmsOnly', '本地驻网 · 仅短信')
    case 'roaming-sms-only':
      return label('network.roamingSmsOnly', '漫游 · 仅短信')
    case 'home-csfb-not-preferred':
      return label('network.home', '本地驻网')
    case 'roaming-csfb-not-preferred':
      return label('network.roaming', '漫游')
    case 'attached-rlos':
      return label('network.restricted', '受限驻网')
    case 'emergency-only':
      return label('network.emergencyOnly', '仅限紧急呼叫')
    case 'searching':
      return label('network.searching', '正在搜网')
    case 'denied':
      return label('network.denied', '注册被拒绝')
    case 'idle':
      return label('network.idle', '等待驻网')
    case 'unknown':
      return label('network.unknown', '状态未知')
    default:
      return clean(source.registration_state) || fallback
  }
}
