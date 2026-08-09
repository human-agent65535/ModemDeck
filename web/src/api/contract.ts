import { parseCallRecord, parseMessage, parsePageMeta } from './normalize.ts'
import type {
  ActiveCallSnapshot,
  CallAction,
  CallControlState,
  CallDirection,
  CallMediaICEConfiguration,
  CallMediaICEServer,
  CallLeaseStatus,
  CallPhase,
  CallRecording,
  CallRecordingSegment,
  CallRecordingSnapshot,
  CallRecordingState,
  CallRecordingStatus,
  CallSession,
  CallPolicyEnforcement,
  CreateMemberInput,
  CreateProxyInput,
  DataConnection,
  DeviceConfiguration,
  DeviceConfigurationCapabilities,
  DeviceFeatureCapability,
  DeviceHardwareConfiguration,
  EffectiveIncomingCallPolicy,
  CloudflareOriginRouteStatus,
  CloudflareOriginTLSStatus,
  ExternalAccessStatus,
  GlobalCallSettings,
  IncomingCallActionResult,
  IncomingCallPolicy,
  IPConfiguration,
  LineColorPresetID,
  LineLabelResult,
  LineSettings,
  LineIncomingCallConfiguration,
  LineMessagingConfiguration,
  Message,
  MessageDeliveryReportSupport,
  MessageReadInput,
  IOSPairingAvailability,
  IOSDeviceInfo,
  IOSPairingResult,
  IOSPairingStatus,
  IOSTestCallResult,
  InstallCloudflareOriginTLSInput,
  MobileNetwork,
  MobileNetworkScan,
  MobileNetworkStatus,
  NetworkLineStatus,
  NetworkSelectionMode,
  NetworkSelectionPolicy,
  NetworkProxyStatus,
  NetworkStatus,
  Page,
  NetworkUsage,
  NetworkUsageTotal,
  OutgoingCallReservation,
  ProxyApplyState,
  ProxyApplyStatus,
  ProxyDeleteResult,
  ProxyInstance,
  ProxyMode,
  ProxyMutation,
  ProxyRuntimeState,
  RecordingEntry,
  RecordingSettings,
  RecordingStatus,
  SendMessageInput,
  ServingRadio,
  SystemLanguage,
  SystemSettings,
  ESIMStatus,
  SIMProfileManagementCapability,
  SIMSlot,
  SIMStatus,
  SIMType,
  TelegramUnit,
  TelegramUnitInput,
  TLSMode,
  TLSSettings,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput,
  UpdateLineLabelInput,
  UpdateLineSettingsInput,
  UpdateMemberInput,
  UpdateSystemSettingsInput,
  UpdateNetworkSelectionInput,
  UpdateProxyInput,
  UpdateTLSSettingsInput,
  UserAccount
} from './types.ts'
import { isLineColorPresetID } from './types.ts'

type JsonRecord = Record<string, unknown>

const CALL_PHASES = new Set<CallPhase>([
  'unknown',
  'dialing',
  'ringing',
  'connecting',
  'active',
  'ending',
  'ended',
  'failed'
])
const CALL_DIRECTIONS = new Set<CallDirection>(['incoming', 'outgoing'])
const CALL_CONTROL_STATES = new Set<CallControlState>([
  'available',
  'owned',
  'occupied'
])
const CALL_ACTIONS = new Set<CallAction>(['answer', 'reject', 'hangup'])
const RECORDING_STATUSES = new Set<RecordingStatus>([
  'pending',
  'recording',
  'ready',
  'failed'
])
const CALL_RECORDING_STATUSES = new Set<CallRecordingStatus>([
  'off',
  ...RECORDING_STATUSES
])
const INCOMING_CALL_POLICIES = new Set<IncomingCallPolicy>([
  'follow_global',
  'receive',
  'do_not_disturb'
])
const EFFECTIVE_INCOMING_CALL_POLICIES = new Set<EffectiveIncomingCallPolicy>([
  'receive',
  'do_not_disturb'
])
const MESSAGE_DELIVERY_REPORT_SUPPORT = new Set<MessageDeliveryReportSupport>([
  'unknown',
  'unsupported'
])
const PROXY_MODES = new Set<ProxyMode>(['http', 'socks5'])
const PROXY_RUNTIME_STATES = new Set<ProxyRuntimeState>([
  'disabled',
  'waiting_for_bearer',
  'running',
  'error'
])
const PROXY_APPLY_STATES = new Set<ProxyApplyState>([
  'applied',
  'pending_create',
  'pending_update',
  'pending_delete'
])
const PROXY_APPLY_STATUSES = new Set<ProxyApplyStatus>([
  'pending',
  'applied',
  'agent_unavailable',
  'agent_rejected',
  'runtime_unavailable'
])
const TLS_MODES = new Set<TLSMode>(['automatic', 'user'])
const SIM_TYPES = new Set<SIMType>(['unknown', 'physical', 'esim'])
const ESIM_STATUSES = new Set<ESIMStatus>(['unknown', 'no_profiles', 'with_profiles'])
const NETWORK_SELECTION_MODES = new Set<NetworkSelectionMode>(['auto', 'manual'])
const MOBILE_NETWORK_STATUSES = new Set<MobileNetworkStatus>([
  'unknown',
  'available',
  'current',
  'forbidden'
])

function objectValue(value: unknown, path: string): JsonRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${path} 必须是对象`)
  }
  return value as JsonRecord
}

function requiredString(source: JsonRecord, path: string, key: string, allowEmpty = false): string {
  const value = source[key]
  if (typeof value !== 'string') throw new Error(`${path} 缺少 ${key}`)
  const trimmed = value.trim()
  if (!allowEmpty && !trimmed) throw new Error(`${path} 缺少 ${key}`)
  return trimmed
}

function optionalString(source: JsonRecord, key: string): string | undefined {
  const value = source[key]
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function optionalStringArray(source: JsonRecord, path: string, key: string): string[] {
  const value = source[key]
  if (value === undefined) return []
  if (!Array.isArray(value)) throw new Error(`${path}.${key} 必须是数组`)
  return value.map((item, index) => {
    if (typeof item !== 'string' || !item.trim()) {
      throw new Error(`${path}.${key}[${index}] 必须是非空字符串`)
    }
    return item.trim()
  })
}

function requiredTimestamp(source: JsonRecord, path: string, key: string): string {
  const value = requiredString(source, path, key)
  if (!Number.isFinite(Date.parse(value))) throw new Error(`${path}.${key} 不是有效时间`)
  return value
}

function optionalTimestamp(source: JsonRecord, path: string, key: string): string | undefined {
  const value = optionalString(source, key)
  if (!value) return undefined
  if (!Number.isFinite(Date.parse(value))) throw new Error(`${path}.${key} 不是有效时间`)
  return value
}

function requiredBoolean(source: JsonRecord, path: string, key: string): boolean {
  if (typeof source[key] !== 'boolean') throw new Error(`${path}.${key} 必须是布尔值`)
  return source[key]
}

function optionalBoolean(
  source: JsonRecord,
  path: string,
  key: string,
  fallback = false
): boolean {
  const value = source[key]
  if (value === undefined) return fallback
  if (typeof value !== 'boolean') throw new Error(`${path}.${key} 必须是布尔值`)
  return value
}

function requiredRevision(source: JsonRecord, path: string): number {
  const revision = Number(source.revision)
  if (!Number.isSafeInteger(revision) || revision < 1) {
    throw new Error(`${path}.revision 必须是正整数`)
  }
  return revision
}

function requiredNonNegativeInteger(source: JsonRecord, path: string, key: string): number {
  const value = Number(source[key])
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path}.${key} 必须是非负整数`)
  }
  return value
}

function nullableFiniteNumber(source: JsonRecord, path: string, key: string): number | null {
  const value = source[key]
  if (value === undefined || value === null) return null
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${path}.${key} 必须是有限数字或 null`)
  }
  return value
}

function nullableUint32(source: JsonRecord, path: string, key: string): number | null {
  const value = source[key]
  if (value === undefined || value === null || value === 0) return null
  if (
    typeof value !== 'number' ||
    !Number.isSafeInteger(value) ||
    value < 0 ||
    value > 0xffffffff
  ) {
    throw new Error(`${path}.${key} 必须是 uint32 或 null`)
  }
  return value
}

function requiredPositiveInteger(source: JsonRecord, path: string, key: string): number {
  const value = requiredNonNegativeInteger(source, path, key)
  if (value < 1) throw new Error(`${path}.${key} 必须是正整数`)
  return value
}

function simType(source: JsonRecord, path: string): SIMType {
  const value = requiredString(source, path, 'sim_type') as SIMType
  if (!SIM_TYPES.has(value)) throw new Error(`${path}.sim_type 无效`)
  return value
}

function esimStatus(source: JsonRecord, path: string): ESIMStatus {
  const value = requiredString(source, path, 'esim_status') as ESIMStatus
  if (!ESIM_STATUSES.has(value)) throw new Error(`${path}.esim_status 无效`)
  return value
}

function maskedEID(source: JsonRecord, path: string): string | undefined {
  const value = source.eid
  if (value === undefined || value === '') return undefined
  if (typeof value !== 'string' || !/^\*{4}\d{4}$/.test(value)) {
    throw new Error(`${path}.eid 必须为空或掩码 EID`)
  }
  return value
}

function simSlot(value: unknown, path: string): SIMSlot {
  const source = objectValue(value, path)
  const eid = maskedEID(source, path)
  return {
    index: requiredPositiveInteger(source, path, 'index'),
    present: requiredBoolean(source, path, 'present'),
    current: requiredBoolean(source, path, 'current'),
    sim_type: simType(source, path),
    esim_status: esimStatus(source, path),
    ...(eid ? { eid } : {})
  }
}

function simSlots(source: JsonRecord, path: string): SIMSlot[] {
  if (!Array.isArray(source.sim_slots)) throw new Error(`${path}.sim_slots 必须是数组`)
  const slots = source.sim_slots.map((value, index) =>
    simSlot(value, `${path}.sim_slots[${index}]`)
  )
  const indexes = slots.map(slot => slot.index)
  if (new Set(indexes).size !== indexes.length) {
    throw new Error(`${path}.sim_slots.index 不能重复`)
  }
  return slots
}

function knownSIMSlot(
  source: JsonRecord,
  path: string,
  valueKey: 'primary_sim_slot' | 'current_sim_slot',
  knownKey: 'primary_sim_slot_known' | 'current_sim_slot_known'
): { value: number; known: boolean } {
  const value = requiredNonNegativeInteger(source, path, valueKey)
  const known = requiredBoolean(source, path, knownKey)
  if (known && value < 1) throw new Error(`${path}.${valueKey} 必须是正整数`)
  if (!known && value !== 0) throw new Error(`${path}.${valueKey} 未知时必须为 0`)
  return { value, known }
}

function simProfileManagement(
  source: JsonRecord,
  path: string
): SIMProfileManagementCapability {
  const capability = objectValue(source.profile_management, `${path}.profile_management`)
  return {
    supported: requiredBoolean(
      capability,
      `${path}.profile_management`,
      'supported'
    ),
    reason: requiredString(
      capability,
      `${path}.profile_management`,
      'reason',
      true
    )
  }
}

function stringList(source: JsonRecord, path: string, key: string): string[] {
  if (!Array.isArray(source[key])) throw new Error(`${path}.${key} 必须是数组`)
  const values = source[key].map((value, index) => {
    if (typeof value !== 'string' || !value.trim()) {
      throw new Error(`${path}.${key}[${index}] 必须是非空字符串`)
    }
    return value.trim()
  })
  if (new Set(values).size !== values.length) throw new Error(`${path}.${key} 不能重复`)
  return values
}

function proxyMode(source: JsonRecord, path: string): ProxyMode {
  const value = requiredString(source, path, 'mode') as ProxyMode
  if (!PROXY_MODES.has(value)) throw new Error(`${path}.mode 无效`)
  return value
}

function proxyRuntimeState(source: JsonRecord, path: string): ProxyRuntimeState {
  const value = requiredString(source, path, 'state') as ProxyRuntimeState
  if (!PROXY_RUNTIME_STATES.has(value)) throw new Error(`${path}.state 无效`)
  return value
}

function proxyApplyState(source: JsonRecord, path: string): ProxyApplyState {
  const value = requiredString(source, path, 'apply_state') as ProxyApplyState
  if (!PROXY_APPLY_STATES.has(value)) throw new Error(`${path}.apply_state 无效`)
  return value
}

function proxyApplyStatus(
  source: JsonRecord,
  path: string,
  key = 'status'
): ProxyApplyStatus {
  const value = requiredString(source, path, key) as ProxyApplyStatus
  if (!PROXY_APPLY_STATUSES.has(value)) throw new Error(`${path}.${key} 无效`)
  return value
}

export const communicationPaths = {
  messages: '/api/v1/messages',
  messageThreads: '/api/v1/messages/threads',
  messageThreadState: '/api/v1/messages/threads/state',
  messageRead: '/api/v1/messages/read',
  calls: '/api/v1/calls',
  callsBatch: '/api/v1/calls/batch',
  missedCallsRead: '/api/v1/calls/missed/read',
  activeCalls: '/api/v1/calls/active',
  recordings: '/api/v1/recordings',
  recordingsBatch: '/api/v1/recordings/batch',
  callSettings: '/api/v1/settings/calls',
  recordingSettings: '/api/v1/settings/recording',
  telegram: '/api/v1/settings/telegram'
} as const

export const communicationContracts = {
  sendMessage: {
    method: 'POST',
    path: communicationPaths.messages,
    successStatus: 201
  },
  markMessageRead: {
    method: 'PATCH',
    path: communicationPaths.messageRead,
    successStatus: 204
  },
  deleteMessageThread: {
    method: 'DELETE',
    path: communicationPaths.messageThreads,
    successStatus: 204
  },
  updateMessageThreads: {
    method: 'PATCH',
    path: communicationPaths.messageThreadState,
    successStatus: 204
  },
  startCall: {
    method: 'POST',
    path: communicationPaths.calls,
    successStatus: 201
  },
  markMissedCallsRead: {
    method: 'PATCH',
    path: communicationPaths.missedCallsRead,
    successStatus: 204
  },
  updateCalls: {
    method: 'PATCH',
    path: communicationPaths.callsBatch,
    successStatus: 204
  },
  activeCalls: {
    method: 'GET',
    path: communicationPaths.activeCalls,
    successStatus: 200
  },
  listRecordings: {
    method: 'GET',
    path: communicationPaths.recordings,
    successStatus: 200
  },
  updateRecordings: {
    method: 'PATCH',
    path: communicationPaths.recordingsBatch,
    successStatus: 204
  },
  getCallSettings: {
    method: 'GET',
    path: communicationPaths.callSettings,
    successStatus: 200
  },
  updateCallSettings: {
    method: 'PATCH',
    path: communicationPaths.callSettings,
    successStatus: 200
  },
  getRecordingSettings: {
    method: 'GET',
    path: communicationPaths.recordingSettings,
    successStatus: 200
  },
  updateRecordingSettings: {
    method: 'PUT',
    path: communicationPaths.recordingSettings,
    successStatus: 200
  },
  listTelegram: {
    method: 'GET',
    path: communicationPaths.telegram,
    successStatus: 200
  },
  createTelegram: {
    method: 'POST',
    path: communicationPaths.telegram,
    successStatus: 201
  }
} as const

export const iosPairingPath = '/api/v1/mobile/pairing'
export const iosTestCallPath = '/api/v1/mobile/push/test-call'
export const externalAccessStatusPath = '/api/v1/external-access/status'
export const externalAccessRefreshPath = '/api/v1/external-access/refresh'

export const iosPairingContract = {
  get: {
    method: 'GET',
    path: iosPairingPath,
    successStatus: 200
  },
  create: {
    method: 'POST',
    path: iosPairingPath,
    successStatus: 201
  },
  revoke: {
    method: 'DELETE',
    path: iosPairingPath,
    successStatus: 204
  },
  testCall: {
    method: 'POST',
    path: iosTestCallPath,
    successStatus: 202
  }
} as const

export const externalAccessContract = {
  getStatus: {
    method: 'GET',
    path: externalAccessStatusPath,
    successStatus: 200
  },
  refresh: {
    method: 'POST',
    path: externalAccessRefreshPath,
    successStatus: 200
  },
  installOriginTLS: {
    method: 'PUT',
    path: '/api/v1/external-access/origin-tls',
    successStatus: 200
  },
  disableOriginTLS: {
    method: 'DELETE',
    path: '/api/v1/external-access/origin-tls',
    successStatus: 200
  }
} as const

export const tlsSettingsPath = '/api/v1/settings/tls'
export const tlsCAPath = `${tlsSettingsPath}/ca`

export const tlsSettingsContract = {
  get: {
    method: 'GET',
    path: tlsSettingsPath,
    successStatus: 200
  },
  update: {
    method: 'PUT',
    path: tlsSettingsPath,
    successStatus: 200
  },
  downloadCA: {
    method: 'GET',
    path: tlsCAPath,
    successStatus: 200
  }
} as const

export const networkPaths = {
  status: '/api/v1/network',
  proxies: '/api/v1/proxies'
} as const

export const networkContracts = {
  status: {
    method: 'GET',
    path: networkPaths.status,
    successStatus: 200
  },
  listProxies: {
    method: 'GET',
    path: networkPaths.proxies,
    successStatus: 200
  },
  createProxy: {
    method: 'POST',
    path: networkPaths.proxies,
    successStatus: 201
  }
} as const

export function proxyResourcePath(id: string): string {
  const proxyID = id.trim()
  if (!proxyID) throw new Error('proxy id 不能为空')
  return `${networkPaths.proxies}/${encodeURIComponent(proxyID)}`
}

export function networkSelectionPath(lineID: string): string {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) throw new Error('line id 不能为空')
  return `/api/v1/devices/${encodeURIComponent(normalizedLineID)}/network-selection`
}

export function mobileNetworkScanPath(lineID: string): string {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) throw new Error('line id 不能为空')
  return `/api/v1/devices/${encodeURIComponent(normalizedLineID)}/network-scan`
}

export function networkSelectionContract(lineID: string): {
  get: { method: 'GET'; path: string; successStatus: 200 }
  update: { method: 'PUT'; path: string; successStatus: 200 }
  scan: { method: 'POST'; path: string; successStatus: 200 }
} {
  return {
    get: { method: 'GET', path: networkSelectionPath(lineID), successStatus: 200 },
    update: { method: 'PUT', path: networkSelectionPath(lineID), successStatus: 200 },
    scan: { method: 'POST', path: mobileNetworkScanPath(lineID), successStatus: 200 }
  }
}

export function proxyResourceContract(id: string): {
  update: { method: 'PATCH'; path: string; successStatus: 200 }
  delete: { method: 'DELETE'; path: string; successStatus: 200 }
} {
  const path = proxyResourcePath(id)
  return {
    update: { method: 'PATCH', path, successStatus: 200 },
    delete: { method: 'DELETE', path, successStatus: 200 }
  }
}

export function proxyDeletePath(id: string, revision: number): string {
  if (!Number.isSafeInteger(revision) || revision < 1) {
    throw new Error('proxy revision 必须是正整数')
  }
  return `${proxyResourcePath(id)}?revision=${revision}`
}

export function callActionPath(id: string, action: CallAction | 'dtmf'): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  if (action !== 'dtmf' && !CALL_ACTIONS.has(action)) throw new Error(`未知通话操作：${action}`)
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}/${action}`
}

export function callMediaPath(id: string): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}/media`
}

export function callMediaICEPath(id: string): string {
  return `${callMediaPath(id)}/ice`
}

export function callLeasePath(id: string): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}/lease`
}

export function callRecordingPath(id: string): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}/recording`
}

export function callRecordingsPath(id: string): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}/recordings`
}

export function callRecordPath(id: string): string {
  const callID = id.trim()
  if (!callID) throw new Error('call id 不能为空')
  return `${communicationPaths.calls}/${encodeURIComponent(callID)}`
}

export function missedCallReadPath(id: string): string {
  return `${callRecordPath(id)}/read`
}

export function callRecordingResourcePath(callID: string, recordingID: string): string {
  const normalizedRecordingID = recordingID.trim()
  if (!normalizedRecordingID) throw new Error('recording id 不能为空')
  return `${callRecordingsPath(callID)}/${encodeURIComponent(normalizedRecordingID)}`
}

export function callRecordingContract(id: string): {
  update: { method: 'PUT'; path: string; successStatus: 200 }
  list: { method: 'GET'; path: string; successStatus: 200 }
} {
  return {
    update: { method: 'PUT', path: callRecordingPath(id), successStatus: 200 },
    list: { method: 'GET', path: callRecordingsPath(id), successStatus: 200 }
  }
}

export function lineLabelPath(lineID: string): string {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) throw new Error('line_id 不能为空')
  return `/api/v1/lines/${encodeURIComponent(normalizedLineID)}/label`
}

export function createLineLabelPayload(input: UpdateLineLabelInput): UpdateLineLabelInput {
  const lineLabel = input.line_label.trim()
  if (Array.from(lineLabel).length > 16) throw new Error('线路标签不能超过 16 个字符')
  if (input.line_color !== undefined && !isLineColorPresetID(input.line_color)) {
    throw new Error('线路标签颜色不是受支持的预设')
  }
  return {
    line_label: lineLabel,
    ...(input.line_color === undefined ? {} : { line_color: input.line_color })
  }
}

export function parseLineLabelResponse(value: unknown): LineLabelResult {
  const response = objectValue(value, 'line_label_response')
  const source = objectValue(response.line ?? response, 'line_label_response.line')
  const rawLineColor = optionalString(source, 'line_color') || ''
  if (rawLineColor && !isLineColorPresetID(rawLineColor)) {
    throw new Error('line_label_response.line.line_color 不是受支持的预设')
  }
  return {
    line_id: requiredString(source, 'line_label_response.line', 'line_id'),
    line_label: requiredString(source, 'line_label_response.line', 'line_label', true),
    line_color: rawLineColor as LineColorPresetID | ''
  }
}

export function parseSIMStatusResponse(value: unknown): SIMStatus {
  const response = objectValue(value, 'sim_response')
  const source = objectValue(response.sim, 'sim_response.sim')
  const path = 'sim_response.sim'
  const eid = maskedEID(source, path)
  const slots = simSlots(source, path)
  const slotsKnown = requiredBoolean(source, path, 'sim_slots_known')
  const primarySlot = knownSIMSlot(
    source,
    path,
    'primary_sim_slot',
    'primary_sim_slot_known'
  )
  const currentSlot = knownSIMSlot(
    source,
    path,
    'current_sim_slot',
    'current_sim_slot_known'
  )
  const unlockRetriesSource = objectValue(
    source.unlock_retries,
    `${path}.unlock_retries`
  )
  const unlockRetries: Record<string, number> = {}
  for (const [name, retryValue] of Object.entries(unlockRetriesSource)) {
    if (typeof retryValue !== 'number' || !Number.isSafeInteger(retryValue) || retryValue < 0) {
      throw new Error(`${path}.unlock_retries.${name} 必须是非负整数`)
    }
    unlockRetries[name] = retryValue
  }
  return {
    line_id: requiredString(source, path, 'line_id'),
    present: requiredBoolean(source, path, 'present'),
    active: requiredBoolean(source, path, 'active'),
    identifier: requiredString(source, path, 'identifier', true),
    imsi: requiredString(source, path, 'imsi', true),
    sim_type: simType(source, path),
    esim_status: esimStatus(source, path),
    ...(eid ? { eid } : {}),
    sim_slots: slots,
    sim_slots_known: slotsKnown,
    primary_sim_slot: primarySlot.value,
    primary_sim_slot_known: primarySlot.known,
    current_sim_slot: currentSlot.value,
    current_sim_slot_known: currentSlot.known,
    profile_management: simProfileManagement(source, path),
    home_operator_code: requiredString(source, path, 'home_operator_code', true),
    home_operator_name: requiredString(source, path, 'home_operator_name', true),
    home_country_iso: requiredString(source, path, 'home_country_iso', true),
    serving_operator_code: requiredString(source, path, 'serving_operator_code', true),
    serving_operator_name: requiredString(source, path, 'serving_operator_name', true),
    serving_country_iso: requiredString(source, path, 'serving_country_iso', true),
    registration_state_known: requiredBoolean(
      source,
      path,
      'registration_state_known'
    ),
    registration_state_code: requiredNonNegativeInteger(
      source,
      path,
      'registration_state_code'
    ),
    registration_state: requiredString(source, path, 'registration_state', true),
    roaming: requiredBoolean(source, path, 'roaming'),
    operator_identifier: requiredString(source, path, 'operator_identifier', true),
    operator_name: requiredString(source, path, 'operator_name', true),
    unlock_required: requiredString(source, path, 'unlock_required', true),
    unlock_required_code: requiredNonNegativeInteger(
      source,
      path,
      'unlock_required_code'
    ),
    unlock_retries: unlockRetries,
    observed_at: requiredTimestamp(source, path, 'observed_at')
  }
}

function normalizedProxyFields(
  input: CreateProxyInput | UpdateProxyInput,
  requirePassword: boolean
): Omit<CreateProxyInput, 'password'> & { password?: string } {
  const name = input.name.trim()
  const lineID = input.line_id.trim()
  const listenAddress = input.listen_address.trim()
  const username = input.username.trim()
  const password = input.password
  if (!name) throw new Error('代理名称不能为空')
  if (!lineID) throw new Error('请选择线路')
  if (!PROXY_MODES.has(input.mode)) throw new Error('代理协议无效')
  if (!listenAddress) throw new Error('监听地址不能为空')
  if (
    !Number.isSafeInteger(input.listen_port) ||
    input.listen_port < 1024 ||
    input.listen_port > 65535
  ) {
    throw new Error('监听端口必须在 1024 到 65535 之间')
  }
  if (input.auth_enabled && (!username || (requirePassword && !password))) {
    throw new Error('启用认证时必须填写用户名和密码')
  }
  if (!input.auth_enabled && (username || password)) {
    throw new Error('未启用认证时不能提交凭据')
  }
  return {
    name,
    line_id: lineID,
    enabled: input.enabled,
    mode: input.mode,
    listen_address: listenAddress,
    listen_port: input.listen_port,
    auth_enabled: input.auth_enabled,
    username,
    ...(password ? { password } : {})
  }
}

export function createProxyPayload(
  input: CreateProxyInput
): CreateProxyInput & { revision: 0 } {
  const fields = normalizedProxyFields(input, input.auth_enabled)
  return {
    revision: 0,
    ...fields,
    password: fields.password || ''
  }
}

export function createProxyUpdatePayload(
  input: UpdateProxyInput
): UpdateProxyInput {
  if (!Number.isSafeInteger(input.revision) || input.revision < 1) {
    throw new Error('proxy revision 必须是正整数')
  }
  return {
    revision: input.revision,
    ...normalizedProxyFields(input, false)
  }
}

function parseNetworkLine(value: unknown, index: number): NetworkLineStatus {
  const path = `network.lines[${index}]`
  const source = objectValue(value, path)
  return {
    line_id: requiredString(source, path, 'line_id'),
    connected: requiredBoolean(source, path, 'connected'),
    interface: requiredString(source, path, 'interface', true),
    addresses: stringList(source, path, 'addresses'),
    dns: stringList(source, path, 'dns'),
    rx_bytes: requiredNonNegativeInteger(source, path, 'rx_bytes'),
    tx_bytes: requiredNonNegativeInteger(source, path, 'tx_bytes'),
    error: requiredString(source, path, 'error', true)
  }
}

function parseNetworkProxy(value: unknown, index: number): NetworkProxyStatus {
  const path = `network.proxies[${index}]`
  const source = objectValue(value, path)
  return {
    id: requiredString(source, path, 'id'),
    line_id: requiredString(source, path, 'line_id'),
    state: proxyRuntimeState(source, path),
    running: requiredBoolean(source, path, 'running'),
    mode: proxyMode(source, path),
    listen_address: requiredString(source, path, 'listen_address'),
    listen_port: requiredPositiveInteger(source, path, 'listen_port'),
    interface: requiredString(source, path, 'interface', true),
    runtime_epoch: requiredString(source, path, 'runtime_epoch', true),
    started_at: optionalTimestamp(source, path, 'started_at'),
    bytes_up: requiredNonNegativeInteger(source, path, 'bytes_up'),
    bytes_down: requiredNonNegativeInteger(source, path, 'bytes_down'),
    connections: requiredNonNegativeInteger(source, path, 'connections'),
    active_connections: requiredNonNegativeInteger(source, path, 'active_connections'),
    last_error: requiredString(source, path, 'last_error', true)
  }
}

function parseNetworkUsage(value: unknown, path: string): NetworkUsage {
  const source = objectValue(value, path)
  const scopeKind = requiredString(source, path, 'scope_kind')
  if (scopeKind !== 'line' && scopeKind !== 'proxy') {
    throw new Error(`${path}.scope_kind 无效`)
  }
  return {
    scope_kind: scopeKind,
    scope_id: requiredString(source, path, 'scope_id'),
    rx_bytes: requiredNonNegativeInteger(source, path, 'rx_bytes'),
    tx_bytes: requiredNonNegativeInteger(source, path, 'tx_bytes')
  }
}

function parseNetworkUsageTotal(value: unknown, path: string): NetworkUsageTotal {
  const source = objectValue(value, path)
  return {
    rx_bytes: requiredNonNegativeInteger(source, path, 'rx_bytes'),
    tx_bytes: requiredNonNegativeInteger(source, path, 'tx_bytes')
  }
}

export function parseNetworkStatusResponse(value: unknown): NetworkStatus {
  const source = objectValue(value, 'network')
  if (!Array.isArray(source.lines)) throw new Error('network.lines 必须是数组')
  if (!Array.isArray(source.proxies)) throw new Error('network.proxies 必须是数组')
  if (!Array.isArray(source.today_usage)) throw new Error('network.today_usage 必须是数组')
  if (!Array.isArray(source.month_usage)) throw new Error('network.month_usage 必须是数组')
  return {
    available: requiredBoolean(source, 'network', 'available'),
    state: requiredString(source, 'network', 'state'),
    unavailable_reason: optionalString(source, 'unavailable_reason'),
    boot_epoch: requiredString(source, 'network', 'boot_epoch', true),
    observed_at: optionalTimestamp(source, 'network', 'observed_at'),
    lines: source.lines.map(parseNetworkLine),
    proxies: source.proxies.map(parseNetworkProxy),
    today_total: parseNetworkUsageTotal(source.today_total, 'network.today_total'),
    today_usage: source.today_usage.map((item, index) =>
      parseNetworkUsage(item, `network.today_usage[${index}]`)
    ),
    month_total: parseNetworkUsageTotal(source.month_total, 'network.month_total'),
    month_usage: source.month_usage.map((item, index) =>
      parseNetworkUsage(item, `network.month_usage[${index}]`)
    ),
    stale: requiredBoolean(source, 'network', 'stale'),
    apply_pending: requiredBoolean(source, 'network', 'apply_pending'),
    apply_status: proxyApplyStatus(source, 'network', 'apply_status'),
    apply_attempts: requiredNonNegativeInteger(source, 'network', 'apply_attempts'),
    apply_exhausted: requiredBoolean(source, 'network', 'apply_exhausted')
  }
}

function networkSelectionMode(source: JsonRecord, path: string): NetworkSelectionMode {
  const mode = requiredString(source, path, 'mode') as NetworkSelectionMode
  if (!NETWORK_SELECTION_MODES.has(mode)) throw new Error(`${path}.mode 无效`)
  return mode
}

function normalizedOperatorCode(value: string, path: string): string {
  const operatorCode = value.trim()
  if (!/^\d{5,6}$/.test(operatorCode)) {
    throw new Error(`${path} 必须是 5 或 6 位 MCCMNC`)
  }
  return operatorCode
}

export function createNetworkSelectionPayload(
  input: UpdateNetworkSelectionInput
): UpdateNetworkSelectionInput {
  if (!NETWORK_SELECTION_MODES.has(input.mode)) throw new Error('网络选择模式无效')
  if (!Number.isSafeInteger(input.expected_revision) || input.expected_revision < 1) {
    throw new Error('network selection revision 必须是正整数')
  }
  if (input.mode === 'auto') {
    return { mode: 'auto', expected_revision: input.expected_revision }
  }
  return {
    mode: 'manual',
    operator_code: normalizedOperatorCode(input.operator_code || '', 'operator_code'),
    expected_revision: input.expected_revision
  }
}

export function parseNetworkSelectionResponse(value: unknown): NetworkSelectionPolicy {
  const path = 'network_selection'
  const source = objectValue(value, path)
  const mode = networkSelectionMode(source, path)
  const rawOperatorCode = optionalString(source, 'operator_code')
  const operatorCode = rawOperatorCode
    ? normalizedOperatorCode(rawOperatorCode, `${path}.operator_code`)
    : undefined
  if (mode === 'manual' && !operatorCode) {
    throw new Error(`${path}.operator_code 缺失`)
  }
  return {
    line_id: requiredString(source, path, 'line_id'),
    mode,
    ...(operatorCode ? { operator_code: operatorCode } : {}),
    revision: requiredRevision(source, path),
    applied: requiredBoolean(source, path, 'applied'),
    last_error: optionalString(source, 'last_error'),
    applied_at: optionalTimestamp(source, path, 'applied_at')
  }
}

function mobileNetworkStatus(source: JsonRecord, path: string): MobileNetworkStatus {
  const status = requiredString(source, path, 'status') as MobileNetworkStatus
  if (!MOBILE_NETWORK_STATUSES.has(status)) throw new Error(`${path}.status 无效`)
  return status
}

function parseMobileNetwork(value: unknown, index: number): MobileNetwork {
  const path = `network_scan.networks[${index}]`
  const source = objectValue(value, path)
  return {
    status: mobileNetworkStatus(source, path),
    operator_code: normalizedOperatorCode(
      requiredString(source, path, 'operator_code'),
      `${path}.operator_code`
    ),
    operator_long: requiredString(source, path, 'operator_long', true),
    operator_short: requiredString(source, path, 'operator_short', true),
    access_technologies: requiredNonNegativeInteger(source, path, 'access_technologies'),
    access_technology_names: stringList(source, path, 'access_technology_names')
  }
}

export function parseMobileNetworkScanResponse(value: unknown): MobileNetworkScan {
  const path = 'network_scan'
  const source = objectValue(value, path)
  if (!Array.isArray(source.networks)) throw new Error(`${path}.networks 必须是数组`)
  return {
    line_id: requiredString(source, path, 'line_id'),
    observed_at: requiredTimestamp(source, path, 'observed_at'),
    networks: source.networks.map(parseMobileNetwork)
  }
}

function parseProxyInstance(value: unknown, path: string): ProxyInstance {
  const source = objectValue(value, path)
  const revision = requiredRevision(source, path)
  const appliedRevision = requiredNonNegativeInteger(source, path, 'applied_revision')
  if (appliedRevision > revision) {
    throw new Error(`${path}.applied_revision 不能大于 revision`)
  }
  return {
    id: requiredString(source, path, 'id'),
    name: requiredString(source, path, 'name'),
    line_id: requiredString(source, path, 'line_id'),
    enabled: requiredBoolean(source, path, 'enabled'),
    mode: proxyMode(source, path),
    listen_address: requiredString(source, path, 'listen_address'),
    listen_port: requiredPositiveInteger(source, path, 'listen_port'),
    auth_enabled: requiredBoolean(source, path, 'auth_enabled'),
    username: requiredString(source, path, 'username', true),
    has_password: requiredBoolean(source, path, 'has_password'),
    revision,
    applied_revision: appliedRevision,
    apply_state: proxyApplyState(source, path),
    created_at: requiredString(source, path, 'created_at'),
    updated_at: requiredString(source, path, 'updated_at')
  }
}

export function parseProxyCollectionResponse(value: unknown): ProxyInstance[] {
  const source = objectValue(value, 'proxy_collection')
  if (!Array.isArray(source.proxies)) {
    throw new Error('proxy_collection.proxies 必须是数组')
  }
  return source.proxies.map((proxy, index) =>
    parseProxyInstance(proxy, `proxy_collection.proxies[${index}]`)
  )
}

export function parseProxyMutationResponse(value: unknown): ProxyMutation {
  const source = objectValue(value, 'proxy_mutation')
  return {
    proxy: parseProxyInstance(source.proxy, 'proxy_mutation.proxy'),
    applied: requiredBoolean(source, 'proxy_mutation', 'applied'),
    status: proxyApplyStatus(source, 'proxy_mutation')
  }
}

export function parseProxyDeleteResponse(value: unknown): ProxyDeleteResult {
  const source = objectValue(value, 'proxy_delete')
  return {
    id: requiredString(source, 'proxy_delete', 'id'),
    applied: requiredBoolean(source, 'proxy_delete', 'applied'),
    status: proxyApplyStatus(source, 'proxy_delete')
  }
}

export function deviceConfigurationPath(lineID: string): string {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) throw new Error('line id 不能为空')
  return `/api/v1/devices/${encodeURIComponent(normalizedLineID)}/configuration`
}

export function deviceConfigurationContract(lineID: string): {
  get: { method: 'GET'; path: string; successStatus: 200 }
  update: { method: 'PATCH'; path: string; successStatus: 200 }
} {
  const path = deviceConfigurationPath(lineID)
  return {
    get: { method: 'GET', path, successStatus: 200 },
    update: { method: 'PATCH', path, successStatus: 200 }
  }
}

export function diagnosticDeviceConfigurationContract(lineID: string): {
  get: { method: 'GET'; path: string; successStatus: 200 }
  resetUSB: { method: 'PATCH'; path: string; successStatus: 200 }
} {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) throw new Error('line id 不能为空')
  const path =
    `/api/v1/diagnostics/devices/${encodeURIComponent(normalizedLineID)}` +
    '/configuration'
  return {
    get: { method: 'GET', path, successStatus: 200 },
    resetUSB: { method: 'PATCH', path, successStatus: 200 }
  }
}

export function callMediaContract(id: string): {
  method: 'POST'
  path: string
  successStatus: 200
} {
  return {
    method: 'POST',
    path: callMediaPath(id),
    successStatus: 200
  }
}

export function callMediaICEContract(id: string): {
  method: 'POST'
  path: string
  successStatus: 200
} {
  return {
    method: 'POST',
    path: callMediaICEPath(id),
    successStatus: 200
  }
}

export function callMediaReleaseContract(id: string): {
  method: 'DELETE'
  path: string
  successStatus: 204
} {
  return {
    method: 'DELETE',
    path: callMediaPath(id),
    successStatus: 204
  }
}

export function callLeaseContract(id: string): {
  method: 'PUT'
  path: string
  successStatus: 200
} {
  return {
    method: 'PUT',
    path: callLeasePath(id),
    successStatus: 200
  }
}

export function callActionContract(id: string, action: CallAction | 'dtmf'): {
  method: 'POST'
  path: string
  successStatus: 202
} {
  return {
    method: 'POST',
    path: callActionPath(id, action),
    successStatus: 202
  }
}

export function telegramUnitPath(id: string): string {
  const unitID = id.trim()
  if (!unitID) throw new Error('Telegram unit id 不能为空')
  return `${communicationPaths.telegram}/${encodeURIComponent(unitID)}`
}

export function telegramUnitContract(id: string): {
  update: { method: 'PUT'; path: string; successStatus: 200 }
  delete: { method: 'DELETE'; path: string; successStatus: 204 }
} {
  const path = telegramUnitPath(id)
  return {
    update: { method: 'PUT', path, successStatus: 200 },
    delete: { method: 'DELETE', path, successStatus: 204 }
  }
}

export function telegramUnitDeletePath(id: string, revision: number): string {
  if (!Number.isSafeInteger(revision) || revision < 1) {
    throw new Error('Telegram unit revision 必须是正整数')
  }
  return `${telegramUnitPath(id)}?revision=${revision}`
}

export function createMessagePayload(input: SendMessageInput): SendMessageInput {
  const lineID = input.line_id.trim()
  const to = input.to.trim()
  const content = input.content.trim()
  const requestID = input.request_id?.trim()
  if (!lineID || !to || !content) {
    throw new Error('line_id、to 和 content 不能为空')
  }
  return {
    ...(requestID ? { request_id: requestID } : {}),
    line_id: lineID,
    to,
    content
  }
}

export function createMessageReadPayload(input: MessageReadInput): MessageReadInput {
  const lineID = input.line_id.trim()
  const peer = input.peer.trim()
  if (!lineID || !peer) {
    throw new Error('line_id 和 peer 不能为空')
  }
  return {
    line_id: lineID,
    peer
  }
}

export function createCallPayload(
  lineID: string,
  number: string,
  requestID?: string,
  recordingEnabled?: boolean
): {
  request_id?: string
  line_id: string
  number: string
  recording_enabled?: boolean
} {
  const normalizedLineID = lineID.trim()
  const normalizedNumber = number.trim()
  const normalizedRequestID = requestID?.trim()
  if (!normalizedLineID || !normalizedNumber) throw new Error('line_id 和 number 不能为空')
  return {
    ...(normalizedRequestID ? { request_id: normalizedRequestID } : {}),
    line_id: normalizedLineID,
    number: normalizedNumber,
    ...(typeof recordingEnabled === 'boolean' ? { recording_enabled: recordingEnabled } : {})
  }
}

export function createCallActionPayload(
  action: CallAction,
  requestID: string | undefined,
  recordingEnabled?: boolean
): { request_id?: string; recording_enabled?: boolean } {
  if (!CALL_ACTIONS.has(action)) throw new Error(`未知通话操作：${action}`)
  const normalizedRequestID = requestID?.trim()
  return {
    ...(normalizedRequestID ? { request_id: normalizedRequestID } : {}),
    ...(action === 'answer' && typeof recordingEnabled === 'boolean'
      ? { recording_enabled: recordingEnabled }
      : {})
  }
}

export function createDTMFPayload(
  digits: string,
  requestID: string | undefined
): { request_id?: string; digits: string } {
  const normalizedDigits = digits.trim().toUpperCase()
  const normalizedRequestID = requestID?.trim()
  if (!/^[0-9*#A-D]+$/.test(normalizedDigits)) throw new Error('DTMF 按键无效')
  return {
    ...(normalizedRequestID ? { request_id: normalizedRequestID } : {}),
    digits: normalizedDigits
  }
}

export function createCallMediaPayload(
  ownerToken: string,
  offerSDP: string
): { owner_token: string; offer_sdp: string } {
  const normalizedOwnerToken = ownerToken.trim()
  if (!normalizedOwnerToken) throw new Error('owner_token 不能为空')
  if (!offerSDP.trim()) throw new Error('offer_sdp 不能为空')
  return {
    owner_token: normalizedOwnerToken,
    offer_sdp: offerSDP
  }
}

export function createCallMediaICEPayload(): Record<string, never> {
  return {}
}

export function createCallMediaReleasePayload(
  ownerToken: string
): { owner_token: string } {
  const normalizedOwnerToken = ownerToken.trim()
  if (!normalizedOwnerToken) throw new Error('owner_token 不能为空')
  return {
    owner_token: normalizedOwnerToken
  }
}

export function createCallLeasePayload(): Record<string, never> {
  return {}
}

export function createRecordingSettingsPayload(
  settings: RecordingSettings
): RecordingSettings {
  if (!Number.isSafeInteger(settings.revision) || settings.revision < 1) {
    throw new Error('recording settings revision 必须是正整数')
  }
  return {
    default_enabled: settings.default_enabled,
    revision: settings.revision
  }
}

export function createTLSSettingsPayload(
  input: UpdateTLSSettingsInput
): UpdateTLSSettingsInput {
  if (input.operation === 'use_automatic') {
    return { operation: 'use_automatic' }
  }
  if (input.operation !== 'install_user') {
    throw new Error('未知 Web 证书操作')
  }
  if (!input.certificate_pem.trim()) throw new Error('certificate_pem 不能为空')
  if (!input.private_key_pem.trim()) throw new Error('private_key_pem 不能为空')
  return {
    operation: 'install_user',
    certificate_pem: input.certificate_pem,
    private_key_pem: input.private_key_pem
  }
}

export function createCloudflareOriginTLSPayload(
  input: InstallCloudflareOriginTLSInput
): InstallCloudflareOriginTLSInput {
  if (!input.certificate_pem.trim()) throw new Error('certificate_pem 不能为空')
  if (!input.private_key_pem.trim()) throw new Error('private_key_pem 不能为空')
  return {
    certificate_pem: input.certificate_pem,
    private_key_pem: input.private_key_pem
  }
}

export function createCallRecordingPayload(enabled: boolean): { enabled: boolean } {
  return { enabled }
}

export function createGlobalCallSettingsPayload(
  input: UpdateGlobalCallSettingsInput
): UpdateGlobalCallSettingsInput {
  if (!Number.isSafeInteger(input.expected_revision) || input.expected_revision < 1) {
    throw new Error('call settings expected_revision 必须是正整数')
  }
  return {
    receive_calls: input.receive_calls,
    expected_revision: input.expected_revision
  }
}

export function createLineSettingsPayload(
  input: UpdateLineSettingsInput
): UpdateLineSettingsInput {
  const defaultLineID = input.default_line_id.trim()
  if (!defaultLineID) throw new Error('default_line_id 不能为空')
  if (!Number.isSafeInteger(input.expected_revision) || input.expected_revision < 1) {
    throw new Error('line settings expected_revision 必须是正整数')
  }
  return {
    default_line_id: defaultLineID,
    expected_revision: input.expected_revision
  }
}

export function createSystemSettingsPayload(
  input: UpdateSystemSettingsInput
): UpdateSystemSettingsInput {
  if (
    input.language !== 'auto' &&
    input.language !== 'zh-CN' &&
    input.language !== 'zh-TW' &&
    input.language !== 'en-US' &&
    input.language !== 'ja-JP' &&
    input.language !== 'vi-VN' &&
    input.language !== 'es-ES' &&
    input.language !== 'de-DE' &&
    input.language !== 'fr-FR' &&
    input.language !== 'pt-BR'
  ) {
    throw new Error('system settings language is invalid')
  }
  if (!Number.isSafeInteger(input.expected_revision) || input.expected_revision < 1) {
    throw new Error('system settings expected_revision must be a positive integer')
  }
  return {
    language: input.language,
    expected_revision: input.expected_revision
  }
}

export function createDeviceConfigurationPayload(
  input: UpdateDeviceConfigurationInput
): UpdateDeviceConfigurationInput {
  if (input.operation === 'set_incoming_call_policy') {
    if (
      !INCOMING_CALL_POLICIES.has(input.incoming_call_policy) ||
      !Number.isSafeInteger(input.expected_policy_revision) ||
      input.expected_policy_revision < 1
    ) {
      throw new Error('线路来电策略或 expected_policy_revision 无效')
    }
    return {
      operation: input.operation,
      expected_policy_revision: input.expected_policy_revision,
      incoming_call_policy: input.incoming_call_policy
    }
  }
  if (input.operation === 'set_delivery_reports_enabled') {
    if (
      !Number.isSafeInteger(input.expected_message_policy_revision) ||
      input.expected_message_policy_revision < 1
    ) {
      throw new Error('expected_message_policy_revision 必须是正整数')
    }
    return {
      operation: input.operation,
      expected_message_policy_revision: input.expected_message_policy_revision,
      delivery_reports_enabled: input.delivery_reports_enabled
    }
  }

  const requestID = input.request_id.trim()
  const expectedRevision = input.expected_device_revision.trim()
  if (!requestID || !expectedRevision) {
    throw new Error('设备配置 request_id 和 expected_device_revision 不能为空')
  }
  switch (input.operation) {
    case 'set_radio_enabled':
      return {
        request_id: requestID,
        operation: input.operation,
        expected_device_revision: expectedRevision,
        radio_enabled: input.radio_enabled
      }
    case 'connect_data': {
      const apn = input.apn.trim()
      if (!['auto', 'ipv4', 'ipv6', 'ipv4v6'].includes(input.ip_family)) {
        throw new Error('ip_family 必须是 auto、ipv4、ipv6 或 ipv4v6')
      }
      if (apn && !/^[A-Za-z0-9.-]+$/.test(apn)) {
        throw new Error('APN 只能包含 ASCII 字母、数字、点和连字符')
      }
      return {
        request_id: requestID,
        operation: input.operation,
        expected_device_revision: expectedRevision,
        apn,
        ip_family: input.ip_family
      }
    }
    case 'disconnect_data':
      return {
        request_id: requestID,
        operation: input.operation,
        expected_device_revision: expectedRevision
      }
    case 'set_volte_policy':
      if (input.volte_policy !== 'enabled' && input.volte_policy !== 'disabled') {
        throw new Error('volte_policy 必须是 enabled 或 disabled')
      }
      return {
        request_id: requestID,
        operation: input.operation,
        expected_device_revision: expectedRevision,
        volte_policy: input.volte_policy
      }
    case 'reprobe_voice':
    case 'restart_modem':
    case 'reset_usb':
      return {
        request_id: requestID,
        operation: input.operation,
        expected_device_revision: expectedRevision
      }
  }
}

export function createTelegramUnitPayload(input: TelegramUnitInput): TelegramUnitInput {
  const displayName = input.display_name.trim()
  const chatID = input.chat_id.trim()
  const adminID = input.admin_id.trim()
  const token = input.bot_token?.trim()
  if (!displayName || !chatID || !adminID) {
    throw new Error('display_name、chat_id 和 admin_id 不能为空')
  }
  const lineScopes = input.line_scopes.map(value => value.trim())
  if (lineScopes.some(value => !value) || new Set(lineScopes).size !== lineScopes.length) {
    throw new Error('line_scopes 必须是无重复的非空字符串')
  }
  const assignedUserID = input.assigned_user_id.trim()
  if (!assignedUserID) {
    throw new Error('assigned_user_id is required')
  }
  if (
    input.revision !== undefined &&
    (!Number.isSafeInteger(input.revision) || input.revision < 1)
  ) {
    throw new Error('revision 必须是正整数')
  }
  return {
    display_name: displayName,
    enabled: input.enabled,
    chat_id: chatID,
    admin_id: adminID,
    assigned_user_id: assignedUserID,
    line_scopes: lineScopes,
    incoming_sms: input.incoming_sms,
    missed_calls: input.missed_calls,
    ...(token ? { bot_token: token } : {}),
    ...(input.revision !== undefined ? { revision: input.revision } : {})
  }
}

function parseCallPolicyEnforcement(value: unknown, path: string): CallPolicyEnforcement {
  const source = objectValue(value, path)
  const reason = optionalString(source, 'reason')
  return {
    mode: requiredString(source, path, 'mode'),
    max_submissions_per_call: requiredNonNegativeInteger(
      source,
      path,
      'max_submissions_per_call'
    ),
    new_incoming_ringing_only: requiredBoolean(
      source,
      path,
      'new_incoming_ringing_only'
    ),
    available: requiredBoolean(source, path, 'available'),
    config_only: requiredBoolean(source, path, 'config_only'),
    ...(reason ? { reason } : {})
  }
}

function parseIncomingCallPolicy(
  source: JsonRecord,
  path: string,
  key: string
): IncomingCallPolicy {
  const policy = requiredString(source, path, key) as IncomingCallPolicy
  if (!INCOMING_CALL_POLICIES.has(policy)) throw new Error(`${path}.${key} 未知：${policy}`)
  return policy
}

function parseEffectiveIncomingCallPolicy(
  source: JsonRecord,
  path: string,
  key: string
): EffectiveIncomingCallPolicy {
  const policy = requiredString(source, path, key) as EffectiveIncomingCallPolicy
  if (!EFFECTIVE_INCOMING_CALL_POLICIES.has(policy)) {
    throw new Error(`${path}.${key} 未知：${policy}`)
  }
  return policy
}

function parseIncomingCallAction(value: unknown): IncomingCallActionResult {
  const source = objectValue(value, 'incoming_calls.last_action')
  const errorCode = optionalString(source, 'error_code')
  return {
    call_id: requiredString(source, 'incoming_calls.last_action', 'call_id'),
    effective_policy: parseEffectiveIncomingCallPolicy(
      source,
      'incoming_calls.last_action',
      'effective_policy'
    ),
    status: requiredString(source, 'incoming_calls.last_action', 'status'),
    ...(errorCode ? { error_code: errorCode } : {}),
    created_at: requiredString(source, 'incoming_calls.last_action', 'created_at'),
    updated_at: requiredString(source, 'incoming_calls.last_action', 'updated_at')
  }
}

function parseLineIncomingCallConfiguration(value: unknown): LineIncomingCallConfiguration {
  const source = objectValue(value, 'incoming_calls')
  const lastAction =
    source.last_action === undefined ? undefined : parseIncomingCallAction(source.last_action)
  return {
    policy: parseIncomingCallPolicy(source, 'incoming_calls', 'policy'),
    revision: requiredRevision(source, 'incoming_calls'),
    effective_policy: parseEffectiveIncomingCallPolicy(
      source,
      'incoming_calls',
      'effective_policy'
    ),
    global_receive_calls: requiredBoolean(
      source,
      'incoming_calls',
      'global_receive_calls'
    ),
    global_revision: requiredNonNegativeInteger(
      source,
      'incoming_calls',
      'global_revision'
    ),
    updated_at: requiredString(source, 'incoming_calls', 'updated_at'),
    enforcement: parseCallPolicyEnforcement(
      source.enforcement,
      'incoming_calls.enforcement'
    ),
    ...(lastAction ? { last_action: lastAction } : {})
  }
}

function parseLineMessagingConfiguration(value: unknown): LineMessagingConfiguration {
  const source = objectValue(value, 'messaging')
  const support = requiredString(
    source,
    'messaging',
    'delivery_reports_support'
  ) as MessageDeliveryReportSupport
  if (!MESSAGE_DELIVERY_REPORT_SUPPORT.has(support)) {
    throw new Error(`messaging.delivery_reports_support 未知：${support}`)
  }
  return {
    delivery_reports_enabled: requiredBoolean(
      source,
      'messaging',
      'delivery_reports_enabled'
    ),
    delivery_reports_support: support,
    revision: requiredRevision(source, 'messaging')
  }
}

function parseFeatureCapability(value: unknown, path: string): DeviceFeatureCapability {
  const source = objectValue(value, path)
  const reason = optionalString(source, 'reason')
  return {
    backend: requiredString(source, path, 'backend', true),
    supported: requiredBoolean(source, path, 'supported'),
    implemented: requiredBoolean(source, path, 'implemented'),
    readable: requiredBoolean(source, path, 'readable'),
    writable: requiredBoolean(source, path, 'writable'),
    ...(reason ? { reason } : {})
  }
}

function parseIPConfiguration(value: unknown, path: string): IPConfiguration {
  const source = objectValue(value, path)
  if (!Array.isArray(source.dns)) throw new Error(`${path}.dns 必须是数组`)
  const dns = source.dns.map((value, index) => {
    if (typeof value !== 'string') throw new Error(`${path}.dns[${index}] 必须是字符串`)
    return value
  })
  return {
    method: requiredString(source, path, 'method', true),
    address: requiredString(source, path, 'address', true),
    prefix: requiredNonNegativeInteger(source, path, 'prefix'),
    gateway: requiredString(source, path, 'gateway', true),
    dns,
    mtu: requiredNonNegativeInteger(source, path, 'mtu')
  }
}

function parseDataConnection(value: unknown, index: number): DataConnection {
  const path = `hardware.data_connections[${index}]`
  const source = objectValue(value, path)
  return {
    id: requiredString(source, path, 'id'),
    connected: requiredBoolean(source, path, 'connected'),
    apn: requiredString(source, path, 'apn', true),
    ip_family: requiredString(source, path, 'ip_family', true),
    interface: requiredString(source, path, 'interface', true),
    ipv4: parseIPConfiguration(source.ipv4, `${path}.ipv4`),
    ipv6: parseIPConfiguration(source.ipv6, `${path}.ipv6`)
  }
}

function parseDeviceCapabilities(value: unknown): DeviceConfigurationCapabilities {
  const source = objectValue(value, 'hardware.capabilities')
  return {
    voice: parseFeatureCapability(source.voice, 'hardware.capabilities.voice'),
    radio: parseFeatureCapability(source.radio, 'hardware.capabilities.radio'),
    data_connection: parseFeatureCapability(
      source.data_connection,
      'hardware.capabilities.data_connection'
    ),
    flight_mode: parseFeatureCapability(
      source.flight_mode,
      'hardware.capabilities.flight_mode'
    ),
    vowifi: parseFeatureCapability(source.vowifi, 'hardware.capabilities.vowifi'),
    volte: parseFeatureCapability(source.volte, 'hardware.capabilities.volte'),
    esim: parseFeatureCapability(source.esim, 'hardware.capabilities.esim'),
    at_terminal: parseFeatureCapability(
      source.at_terminal,
      'hardware.capabilities.at_terminal'
    ),
    ussd: parseFeatureCapability(source.ussd, 'hardware.capabilities.ussd'),
    connection_profile: parseFeatureCapability(
      source.connection_profile,
      'hardware.capabilities.connection_profile'
    ),
    usb_reset: parseFeatureCapability(source.usb_reset, 'hardware.capabilities.usb_reset')
  }
}

function parseServingRadio(value: unknown, path: string): ServingRadio | undefined {
  if (value === undefined || value === null) return undefined
  const source = objectValue(value, path)
  const channelValue = source.channel
  let channel: number | undefined
  if (channelValue !== undefined && channelValue !== null) {
    channel = requiredNonNegativeInteger(source, path, 'channel')
  }
  const duplexMode = optionalString(source, 'duplex_mode')
  const band = optionalString(source, 'band')
  const channelType = optionalString(source, 'channel_type')
  const telemetrySource = optionalString(source, 'source')
  return {
    access_technology: requiredString(source, path, 'access_technology'),
    ...(duplexMode ? { duplex_mode: duplexMode } : {}),
    ...(band ? { band } : {}),
    ...(channel !== undefined ? { channel } : {}),
    ...(channelType ? { channel_type: channelType } : {}),
    ...(telemetrySource ? { source: telemetrySource } : {})
  }
}

function parseDeviceHardwareConfiguration(value: unknown): DeviceHardwareConfiguration {
  const source = objectValue(value, 'hardware')
  const identity = objectValue(source.identity, 'hardware.identity')
  const details =
    source.details === undefined
      ? ({} as JsonRecord)
      : objectValue(source.details, 'hardware.details')
  const radio = objectValue(source.radio, 'hardware.radio')
  const volte = objectValue(source.volte, 'hardware.volte')
  const provisioning = objectValue(
    volte.provisioning,
    'hardware.volte.provisioning'
  )
  const voiceVerification =
    source.voice_verification === undefined || source.voice_verification === null
      ? undefined
      : objectValue(source.voice_verification, 'hardware.voice_verification')
  const usbConfiguration = voiceVerification
    ? requiredString(
        voiceVerification,
        'hardware.voice_verification',
        'usb_configuration'
      )
    : ''
  const mediaRouting = voiceVerification
    ? requiredString(voiceVerification, 'hardware.voice_verification', 'media_routing')
    : ''
  const usbConfigurationStatuses = new Set([
    'enabled',
    'disabled',
    'read_failed',
    'invalid_response'
  ])
  const mediaRoutingStatuses = new Set([
    'enabled',
    'disabled',
    'read_failed',
    'invalid_response',
    'rejected',
    'inactive',
    'probe_pending',
    'supported'
  ])
  if (voiceVerification && !usbConfigurationStatuses.has(usbConfiguration)) {
    throw new Error(
      `hardware.voice_verification.usb_configuration 未知：${usbConfiguration}`
    )
  }
  if (voiceVerification && !mediaRoutingStatuses.has(mediaRouting)) {
    throw new Error(`hardware.voice_verification.media_routing 未知：${mediaRouting}`)
  }
  const policyValue = optionalString(volte, 'policy')
  if (policyValue && policyValue !== 'enabled' && policyValue !== 'disabled') {
    throw new Error(`hardware.volte.policy 未知：${policyValue}`)
  }
  if (!Array.isArray(source.data_connections)) {
    throw new Error('hardware.data_connections 必须是数组')
  }
  const rawPorts = details.ports
  if (rawPorts !== undefined && rawPorts !== null && !Array.isArray(rawPorts)) {
    throw new Error('hardware.details.ports 必须是数组')
  }
  const ports = (Array.isArray(rawPorts) ? rawPorts : []).map((value, index) => {
    const path = `hardware.details.ports[${index}]`
    const port = objectValue(value, path)
    return {
      name: requiredString(port, path, 'name'),
      type: requiredString(port, path, 'type'),
      type_code: requiredNonNegativeInteger(port, path, 'type_code')
    }
  })
  const servingRadio = parseServingRadio(
    details.serving_radio,
    'hardware.details.serving_radio'
  )
  const profileID = optionalString(volte, 'profile_id')
  const configurationMode = optionalString(volte, 'configuration_mode')
  const carrierConfiguration = optionalString(
    provisioning,
    'carrier_configuration'
  )
  const carrierConfigurationRevision = optionalString(
    provisioning,
    'carrier_configuration_revision'
  )
  if (
    configurationMode &&
    configurationMode !== 'automatic' &&
    configurationMode !== 'forced_enabled' &&
    configurationMode !== 'forced_disabled'
  ) {
    throw new Error(`hardware.volte.configuration_mode 未知：${configurationMode}`)
  }
  return {
    line_id: requiredString(source, 'hardware', 'line_id'),
    revision: requiredString(source, 'hardware', 'revision'),
    observed_at: requiredTimestamp(source, 'hardware', 'observed_at'),
    identity: {
      manufacturer: requiredString(identity, 'hardware.identity', 'manufacturer', true),
      model: requiredString(identity, 'hardware.identity', 'model', true),
      firmware: requiredString(identity, 'hardware.identity', 'firmware', true),
      equipment_identifier: requiredString(
        identity,
        'hardware.identity',
        'equipment_identifier',
        true
      )
    },
    details: {
      hardware_revision: optionalString(details, 'hardware_revision') || '',
      primary_port: optionalString(details, 'primary_port') || '',
      access_technologies: nullableUint32(
        details,
        'hardware.details',
        'access_technologies'
      ),
      ...(servingRadio ? { serving_radio: servingRadio } : {}),
      snr: nullableFiniteNumber(details, 'hardware.details', 'snr'),
      ports
    },
    radio: {
      enabled: requiredBoolean(radio, 'hardware.radio', 'enabled'),
      enabled_known: requiredBoolean(radio, 'hardware.radio', 'enabled_known'),
      power_state: requiredString(radio, 'hardware.radio', 'power_state', true),
      power_state_code: requiredNonNegativeInteger(
        radio,
        'hardware.radio',
        'power_state_code'
      )
    },
    flight_mode: requiredBoolean(source, 'hardware', 'flight_mode'),
    flight_mode_known: requiredBoolean(source, 'hardware', 'flight_mode_known'),
    network_enabled: requiredBoolean(source, 'hardware', 'network_enabled'),
    automatic_apn: optionalString(source, 'automatic_apn') || '',
    data_connections: source.data_connections.map(parseDataConnection),
    ...(voiceVerification
      ? {
          voice_verification: {
            usb_configuration:
              usbConfiguration as NonNullable<
                DeviceHardwareConfiguration['voice_verification']
              >['usb_configuration'],
            media_routing:
              mediaRouting as NonNullable<
                DeviceHardwareConfiguration['voice_verification']
              >['media_routing']
          }
        }
      : {}),
    volte: {
      policy_known: requiredBoolean(volte, 'hardware.volte', 'policy_known'),
      ...(policyValue ? { policy: policyValue as 'enabled' | 'disabled' } : {}),
      ...(configurationMode
        ? {
            configuration_mode: configurationMode as
              | 'automatic'
              | 'forced_enabled'
              | 'forced_disabled'
          }
        : {}),
      modem_capability_known: optionalBoolean(
        volte,
        'hardware.volte',
        'modem_capability_known'
      ),
      modem_capability_enabled: optionalBoolean(
        volte,
        'hardware.volte',
        'modem_capability_enabled'
      ),
      restart_required: optionalBoolean(volte, 'hardware.volte', 'restart_required'),
      ...(profileID ? { profile_id: profileID } : {}),
      provisioning: {
        backend: requiredString(
          provisioning,
          'hardware.volte.provisioning',
          'backend'
        ),
        carrier_configuration_reported: requiredBoolean(
          provisioning,
          'hardware.volte.provisioning',
          'carrier_configuration_reported'
        ),
        carrier_configuration_revision_reported: requiredBoolean(
          provisioning,
          'hardware.volte.provisioning',
          'carrier_configuration_revision_reported'
        ),
        ims_profile_reported: requiredBoolean(
          provisioning,
          'hardware.volte.provisioning',
          'ims_profile_reported'
        ),
        ims_profile_present: requiredBoolean(
          provisioning,
          'hardware.volte.provisioning',
          'ims_profile_present'
        ),
        ...(carrierConfiguration
          ? { carrier_configuration: carrierConfiguration }
          : {}),
        ...(carrierConfigurationRevision
          ? { carrier_configuration_revision: carrierConfigurationRevision }
          : {})
      }
    },
    capabilities: parseDeviceCapabilities(source.capabilities)
  }
}

export function parseGlobalCallSettings(value: unknown): GlobalCallSettings {
  const source = objectValue(value, 'call_settings')
  return {
    receive_calls: requiredBoolean(source, 'call_settings', 'receive_calls'),
    revision: requiredRevision(source, 'call_settings')
  }
}

export function parseDeviceConfigurationResponse(value: unknown): DeviceConfiguration {
  const source = objectValue(value, 'device_configuration')
  const hardware =
    source.hardware === undefined ? undefined : parseDeviceHardwareConfiguration(source.hardware)
  const incomingCalls =
    source.incoming_calls === undefined
      ? undefined
      : parseLineIncomingCallConfiguration(source.incoming_calls)
  const messaging =
    source.messaging === undefined
      ? undefined
      : parseLineMessagingConfiguration(source.messaging)
  if (!hardware && !incomingCalls && !messaging) {
    throw new Error('device_configuration 至少需要 hardware、incoming_calls 或 messaging')
  }
  return {
    ...(hardware ? { hardware } : {}),
    ...(incomingCalls ? { incoming_calls: incomingCalls } : {}),
    ...(messaging ? { messaging } : {})
  }
}

export function parseCallSession(value: unknown): CallSession {
  const source = objectValue(value, 'call')
  const direction = requiredString(source, 'call', 'direction') as CallDirection
  const phase = requiredString(source, 'call', 'phase') as CallPhase
  const controlState = requiredString(
    source,
    'call',
    'control_state'
  ) as CallControlState
  if (!CALL_DIRECTIONS.has(direction)) throw new Error(`call.direction 未知：${direction}`)
  if (!CALL_PHASES.has(phase)) throw new Error(`call.phase 未知：${phase}`)
  if (!CALL_CONTROL_STATES.has(controlState)) {
    throw new Error(`call.control_state 未知：${controlState}`)
  }

  const displayName = optionalString(source, 'display_name')
  const activeAt = optionalTimestamp(source, 'call', 'active_at')
  const endedAt = optionalTimestamp(source, 'call', 'ended_at')
  const bearer = optionalString(source, 'bearer')
  const failureReason = optionalString(source, 'failure_reason')
  return {
    id: requiredString(source, 'call', 'id'),
    line_id: requiredString(source, 'call', 'line_id'),
    direction,
    remote_number: requiredString(source, 'call', 'remote_number', true),
    ...(displayName ? { display_name: displayName } : {}),
    phase,
    control_state: controlState,
    media_available: requiredBoolean(source, 'call', 'media_available'),
    created_at: requiredTimestamp(source, 'call', 'created_at'),
    ...(activeAt ? { active_at: activeAt } : {}),
    ...(endedAt ? { ended_at: endedAt } : {}),
    ...(bearer ? { bearer } : {}),
    ...(failureReason ? { failure_reason: failureReason } : {})
  }
}

export function parseCallMediaResponse(value: unknown): string {
  const source = objectValue(value, 'response')
  const answerSDP = source.answer_sdp
  if (typeof answerSDP !== 'string' || !answerSDP.trim()) {
    throw new Error('response 缺少 answer_sdp')
  }
  return answerSDP
}

function parseCallMediaICEServer(
  value: unknown,
  index: number
): CallMediaICEServer {
  const path = `call_media_ice.ice_servers[${index}]`
  const source = objectValue(value, path)
  const urls = optionalStringArray(source, path, 'urls')
  if (!urls.length) throw new Error(`${path}.urls must not be empty`)
  const username = optionalString(source, 'username')
  const credential = optionalString(source, 'credential')
  return {
    urls,
    ...(username ? { username } : {}),
    ...(credential ? { credential } : {})
  }
}

export function parseCallMediaICEConfiguration(
  value: unknown
): CallMediaICEConfiguration {
  const source = objectValue(value, 'call_media_ice')
  if (!Array.isArray(source.ice_servers)) {
    throw new Error('call_media_ice.ice_servers must be an array')
  }
  const policy = requiredString(
    source,
    'call_media_ice',
    'ice_transport_policy'
  )
  if (policy !== 'all' && policy !== 'relay') {
    throw new Error('call_media_ice.ice_transport_policy is invalid')
  }
  const expiresAt = optionalString(source, 'expires_at')
  return {
    ice_servers: source.ice_servers.map(parseCallMediaICEServer),
    ice_transport_policy: policy,
    ...(expiresAt ? { expires_at: expiresAt } : {})
  }
}

export function parseRecordingSettingsResponse(value: unknown): RecordingSettings {
  const response = objectValue(value, 'recording_settings_response')
  const source = objectValue(response.settings, 'recording_settings')
  return {
    default_enabled: requiredBoolean(source, 'recording_settings', 'default_enabled'),
    revision: requiredRevision(source, 'recording_settings')
  }
}

export function parseTLSSettingsResponse(value: unknown): TLSSettings {
  const response = objectValue(value, 'tls_settings_response')
  const source = objectValue(response.tls, 'tls_settings_response.tls')
  const mode = requiredString(source, 'tls_settings_response.tls', 'mode') as TLSMode
  if (!TLS_MODES.has(mode)) throw new Error('tls_settings_response.tls.mode 无效')
  return {
    mode,
    subject: requiredString(source, 'tls_settings_response.tls', 'subject', true),
    issuer: requiredString(source, 'tls_settings_response.tls', 'issuer', true),
    dns_names: stringList(source, 'tls_settings_response.tls', 'dns_names'),
    ip_addresses: stringList(source, 'tls_settings_response.tls', 'ip_addresses'),
    not_before: requiredTimestamp(source, 'tls_settings_response.tls', 'not_before'),
    not_after: requiredTimestamp(source, 'tls_settings_response.tls', 'not_after'),
    fingerprint_sha256: requiredString(
      source,
      'tls_settings_response.tls',
      'fingerprint_sha256'
    ),
    expired: requiredBoolean(source, 'tls_settings_response.tls', 'expired'),
    renews_automatically: requiredBoolean(
      source,
      'tls_settings_response.tls',
      'renews_automatically'
    )
  }
}

export function parseCallRecordingState(value: unknown): CallRecordingState {
  const response = objectValue(value, 'call_recording_response')
  const source = objectValue(response.state, 'call_recording')
  const status = requiredString(
    source,
    'call_recording',
    'status'
  ) as CallRecordingStatus
  if (!CALL_RECORDING_STATUSES.has(status)) {
    throw new Error(`call_recording.status 未知：${status}`)
  }
  const activeSegmentID = optionalString(source, 'active_segment_id')
  const lastErrorCode = optionalString(source, 'last_error_code')
  return {
    call_id: requiredString(source, 'call_recording', 'call_id'),
    enabled: requiredBoolean(source, 'call_recording', 'enabled'),
    status,
    ...(activeSegmentID ? { active_segment_id: activeSegmentID } : {}),
    ...(lastErrorCode ? { last_error_code: lastErrorCode } : {})
  }
}

function authenticatedDownloadURL(source: JsonRecord, path: string): string {
  const value = requiredString(source, path, 'download_url')
  const base = new URL('https://modemdeck.invalid')
  const parsed = new URL(value, base)
  if (parsed.origin !== base.origin || !parsed.pathname.startsWith('/api/v1/')) {
    throw new Error(`${path}.download_url 必须是同源 API 路径`)
  }
  return `${parsed.pathname}${parsed.search}`
}

export function parseCallRecording(value: unknown): CallRecording {
  const source = objectValue(value, 'recording')
  const endedAt = optionalTimestamp(source, 'recording', 'ended_at')
  return {
    id: requiredString(source, 'recording', 'id'),
    call_id: requiredString(source, 'recording', 'call_id'),
    started_at: requiredTimestamp(source, 'recording', 'started_at'),
    ...(endedAt ? { ended_at: endedAt } : {}),
    duration_seconds: requiredNonNegativeInteger(source, 'recording', 'duration_seconds'),
    content_type: requiredString(source, 'recording', 'content_type'),
    size_bytes: requiredNonNegativeInteger(source, 'recording', 'size_bytes'),
    download_url: authenticatedDownloadURL(source, 'recording')
  }
}

export function parseCallRecordingsResponse(value: unknown): CallRecordingSegment[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.segments)) throw new Error('response.segments 必须是数组')
  return source.segments.map((value, index): CallRecordingSegment => {
    const path = `response.segments[${index}]`
    const segment = objectValue(value, path)
    const id = requiredString(segment, path, 'id')
    const callID = requiredString(segment, path, 'call_id')
    const status = requiredString(segment, path, 'status') as RecordingStatus
    if (!RECORDING_STATUSES.has(status)) {
      throw new Error(`${path}.status 未知：${status}`)
    }
    const segmentIndex = requiredNonNegativeInteger(segment, path, 'segment_index')
    if (segmentIndex < 1) throw new Error(`${path}.segment_index 必须是正整数`)
    const startedAt = optionalTimestamp(segment, path, 'started_at')
    const endedAt = optionalTimestamp(segment, path, 'ended_at')
    const createdAt = requiredTimestamp(segment, path, 'created_at')
    const durationMS = requiredNonNegativeInteger(segment, path, 'duration_ms')
    const failureCode = optionalString(segment, 'failure_code')
    const playable = status === 'ready'
    return {
      id,
      call_id: callID,
      segment_index: segmentIndex,
      status,
      recorded_at: startedAt || createdAt,
      ...(startedAt ? { started_at: startedAt } : {}),
      ...(endedAt ? { ended_at: endedAt } : {}),
      duration_seconds: Math.floor(durationMS / 1000),
      size_bytes: requiredNonNegativeInteger(segment, path, 'size_bytes'),
      ...(failureCode ? { failure_code: failureCode } : {}),
      playable,
      ...(playable
        ? {
            content_type: 'audio/ogg; codecs=opus',
            download_url: `/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`
          }
        : {})
    }
  })
}

export function parseCallRecordingSnapshotResponse(
  value: unknown
): CallRecordingSnapshot {
  return {
    state: parseCallRecordingState(value),
    segments: parseCallRecordingsResponse(value)
  }
}

export function parseRecordingEntriesResponse(value: unknown): Page<RecordingEntry> {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.recordings)) throw new Error('response.recordings 必须是数组')
  const items = source.recordings.map((value, index) => {
    const path = `response.recordings[${index}]`
    const entry = objectValue(value, path)
    const segmentPath = `${path}.segment`
    const segment = objectValue(entry.segment, segmentPath)
    const call = parseCallRecord(entry.call)
    const id = requiredString(segment, segmentPath, 'id')
    const callID = requiredString(segment, segmentPath, 'call_id')
    if (call.id !== callID) throw new Error(`${path}.call.id 与 segment.call_id 不一致`)

    const status = requiredString(segment, segmentPath, 'status') as RecordingStatus
    if (!RECORDING_STATUSES.has(status)) {
      throw new Error(`${segmentPath}.status 未知：${status}`)
    }

    const segmentIndex = requiredNonNegativeInteger(segment, segmentPath, 'segment_index')
    if (segmentIndex < 1) throw new Error(`${segmentPath}.segment_index 必须是正整数`)
    const startedAt = optionalTimestamp(segment, segmentPath, 'started_at')
    const endedAt = optionalTimestamp(segment, segmentPath, 'ended_at')
    const createdAt = requiredTimestamp(segment, segmentPath, 'created_at')
    const durationMS = requiredNonNegativeInteger(segment, segmentPath, 'duration_ms')
    const failureCode = optionalString(segment, 'failure_code')
    const playable = requiredBoolean(entry, path, 'playable')
    const favorite = requiredBoolean(entry, path, 'favorite')
    if (playable && status !== 'ready') {
      throw new Error(`${path}.playable 只能用于 ready 录音`)
    }

    const result: RecordingEntry = {
      id,
      call_id: callID,
      segment_index: segmentIndex,
      status,
      recorded_at: startedAt || createdAt,
      ...(startedAt ? { started_at: startedAt } : {}),
      ...(endedAt ? { ended_at: endedAt } : {}),
      duration_seconds: Math.floor(durationMS / 1000),
      size_bytes: requiredNonNegativeInteger(segment, segmentPath, 'size_bytes'),
      ...(failureCode ? { failure_code: failureCode } : {}),
      playable,
      favorite,
      call
    }
    if (playable) {
      result.content_type = 'audio/ogg; codecs=opus'
      result.download_url = `/api/v1/calls/${encodeURIComponent(callID)}/recordings/${encodeURIComponent(id)}/download`
    }
    return result
  })
  return {
    items,
    meta: parsePageMeta(source.meta)
  }
}

export function parseLineSettingsResponse(value: unknown): LineSettings {
  const response = objectValue(value, 'line_settings_response')
  const source = objectValue(response.settings, 'line_settings')
  return {
    default_line_id: requiredString(source, 'line_settings', 'default_line_id'),
    revision: requiredRevision(source, 'line_settings')
  }
}

function systemLanguage(value: unknown, path: string): SystemLanguage {
  if (
    value === 'auto' ||
    value === 'zh-CN' ||
    value === 'zh-TW' ||
    value === 'en-US' ||
    value === 'ja-JP' ||
    value === 'vi-VN' ||
    value === 'es-ES' ||
    value === 'de-DE' ||
    value === 'fr-FR' ||
    value === 'pt-BR'
  ) {
    return value
  }
  throw new Error(`${path}.language is not supported`)
}

function parseSystemSettings(value: unknown, path: string): SystemSettings {
  const source = objectValue(value, path)
  return {
    language: systemLanguage(source.language, path),
    revision: requiredRevision(source, path)
  }
}

export function parseSystemSettingsResponse(value: unknown): SystemSettings {
  const response = objectValue(value, 'system_settings_response')
  return parseSystemSettings(response.settings, 'system_settings')
}

function parseCloudflareOriginRouteStatus(
  value: unknown,
  index: number
): CloudflareOriginRouteStatus {
  const path = `cloudflare_tunnel.origin_routes[${index}]`
  const source = objectValue(value, path)
  const kind = requiredString(source, path, 'kind')
  if (kind !== 'api' && kind !== 'web') {
    throw new Error(`${path}.kind must be api or web`)
  }
  return {
    kind,
    public_url: requiredString(source, path, 'public_url'),
    service_url: requiredString(source, path, 'service_url'),
    https: requiredBoolean(source, path, 'https'),
    http2: requiredBoolean(source, path, 'http2'),
    tls_name_configured: requiredBoolean(source, path, 'tls_name_configured'),
    tls_verification: requiredBoolean(source, path, 'tls_verification')
  }
}

function parseCloudflareTunnelStatus(value: unknown) {
  const cloudflare = objectValue(value, 'cloudflare_tunnel')
  const rawOriginRoutes = cloudflare.origin_routes
  if (
    rawOriginRoutes !== undefined &&
    rawOriginRoutes !== null &&
    !Array.isArray(rawOriginRoutes)
  ) {
    throw new Error('cloudflare_tunnel.origin_routes must be an array')
  }
  return {
    enabled: requiredBoolean(cloudflare, 'cloudflare_tunnel', 'enabled'),
    connector_connected: requiredBoolean(
      cloudflare,
      'cloudflare_tunnel',
      'connector_connected'
    ),
    connected: requiredBoolean(cloudflare, 'cloudflare_tunnel', 'connected'),
    public_url: requiredString(cloudflare, 'cloudflare_tunnel', 'public_url', true),
    api_urls: optionalStringArray(cloudflare, 'cloudflare_tunnel', 'api_urls'),
    verified_api_urls: optionalStringArray(
      cloudflare,
      'cloudflare_tunnel',
      'verified_api_urls'
    ),
    web_urls: optionalStringArray(cloudflare, 'cloudflare_tunnel', 'web_urls'),
    origin_routes: (Array.isArray(rawOriginRoutes) ? rawOriginRoutes : []).map(
      parseCloudflareOriginRouteStatus
    )
  }
}

function parseCloudflareOriginTLSStatus(value: unknown): CloudflareOriginTLSStatus {
  const source = objectValue(value, 'cloudflare_origin_tls')
  return {
    enabled: requiredBoolean(source, 'cloudflare_origin_tls', 'enabled'),
    covers_routes: requiredBoolean(source, 'cloudflare_origin_tls', 'covers_routes'),
    subject: requiredString(source, 'cloudflare_origin_tls', 'subject', true),
    issuer: requiredString(source, 'cloudflare_origin_tls', 'issuer', true),
    dns_names: optionalStringArray(source, 'cloudflare_origin_tls', 'dns_names'),
    not_before: optionalTimestamp(source, 'cloudflare_origin_tls', 'not_before') || '',
    not_after: optionalTimestamp(source, 'cloudflare_origin_tls', 'not_after') || '',
    fingerprint_sha256: requiredString(
      source,
      'cloudflare_origin_tls',
      'fingerprint_sha256',
      true
    ),
    expired: requiredBoolean(source, 'cloudflare_origin_tls', 'expired')
  }
}

export function parseCloudflareOriginTLSResponse(
  value: unknown
): CloudflareOriginTLSStatus {
  const response = objectValue(value, 'cloudflare_origin_tls_response')
  return parseCloudflareOriginTLSStatus(response.origin_tls)
}

function parseTURNAvailabilityStatus(value: unknown) {
  const turn = objectValue(value, 'turn')
  return {
    configured: requiredBoolean(turn, 'turn', 'configured'),
    available: requiredBoolean(turn, 'turn', 'available')
  }
}

function parseIOSPairingAvailability(value: unknown): IOSPairingAvailability {
  if (
    value === 'permission_required' ||
    value === 'cloudflare_required' ||
    value === 'connector_unavailable' ||
    value === 'route_unavailable' ||
    value === 'ready'
  ) {
    return value
  }
  throw new Error('ios_pairing.availability is invalid')
}

export function parseIOSDeviceInfo(
  value: unknown,
  path = 'ios_device'
): IOSDeviceInfo | undefined {
  if (value === undefined || value === null) return undefined
  const source = objectValue(value, path)
  const device: IOSDeviceInfo = {
    device_name: optionalString(source, 'device_name'),
    device_model: optionalString(source, 'device_model'),
    device_model_identifier: optionalString(source, 'device_model_identifier'),
    os_name: optionalString(source, 'os_name'),
    os_version: optionalString(source, 'os_version'),
    app_version: optionalString(source, 'app_version'),
    app_build: optionalString(source, 'app_build')
  }
  return Object.values(device).some(Boolean) ? device : undefined
}

function parseIOSPairingStatus(value: unknown): IOSPairingStatus {
  const source = objectValue(value, 'ios_pairing')
  const credentialCreatedAt = optionalTimestamp(
    source,
    'ios_pairing',
    'credential_created_at'
  )
  const pairedAt = optionalTimestamp(source, 'ios_pairing', 'paired_at')
  const lastSeenAt = optionalTimestamp(source, 'ios_pairing', 'last_seen_at')
  const device = parseIOSDeviceInfo(source.device, 'ios_pairing.device')
  return {
    allowed: requiredBoolean(source, 'ios_pairing', 'allowed'),
    availability: parseIOSPairingAvailability(source.availability),
    has_credential: requiredBoolean(source, 'ios_pairing', 'has_credential'),
    paired: requiredBoolean(source, 'ios_pairing', 'paired'),
    server_urls: optionalStringArray(source, 'ios_pairing', 'server_urls'),
    ...(credentialCreatedAt
      ? { credential_created_at: credentialCreatedAt }
      : {}),
    ...(pairedAt ? { paired_at: pairedAt } : {}),
    ...(lastSeenAt ? { last_seen_at: lastSeenAt } : {}),
    ...(device ? { device } : {})
  }
}

export function parseExternalAccessStatusResponse(
  value: unknown
): ExternalAccessStatus {
  const response = objectValue(value, 'external_access_status')
  return {
    cloudflare: parseCloudflareTunnelStatus(response.cloudflare),
    turn: parseTURNAvailabilityStatus(response.turn),
    origin_tls: parseCloudflareOriginTLSStatus(response.origin_tls)
  }
}

export function parseIOSPairingResponse(value: unknown): IOSPairingResult {
  const response = objectValue(value, 'ios_pairing_response')
  const pairing = parseIOSPairingStatus(response.pairing)
  if (response.payload === undefined) return { pairing }

  const payload = objectValue(response.payload, 'ios_pairing_payload')
  if (payload.version !== 1) {
    throw new Error('ios_pairing_payload.version must be 1')
  }
  const type = requiredString(payload, 'ios_pairing_payload', 'type')
  if (type !== 'modemdeck.ios.pairing') {
    throw new Error('ios_pairing_payload.type is invalid')
  }
  return {
    pairing,
    payload: {
      version: 1,
      type,
      server_url: requiredString(payload, 'ios_pairing_payload', 'server_url'),
      token: requiredString(payload, 'ios_pairing_payload', 'token')
    }
  }
}

export function parseIOSTestCallResponse(value: unknown): IOSTestCallResult {
  const response = objectValue(value, 'ios_test_call')
  return {
    id: requiredString(response, 'ios_test_call', 'id'),
    accepted_at: requiredTimestamp(response, 'ios_test_call', 'accepted_at')
  }
}

export function parseCallResponse(value: unknown): CallSession {
  const source = objectValue(value, 'response')
  return parseCallSession(source.call)
}

export function parseCallLeaseStatus(value: unknown): CallLeaseStatus {
  const source = objectValue(value, 'call_lease')
  return {
    call_id: requiredString(source, 'call_lease', 'call_id'),
    expires_at: requiredTimestamp(source, 'call_lease', 'expires_at')
  }
}

export function parseActiveCallSnapshotResponse(value: unknown): ActiveCallSnapshot {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.calls)) throw new Error('response.calls 必须是数组')
  if (!Array.isArray(source.reservations)) {
    throw new Error('response.reservations 必须是数组')
  }
  const calls = source.calls.map(parseCallSession)
  const callIDs = new Set<string>()
  for (const call of calls) {
    if (callIDs.has(call.id)) throw new Error(`response.calls 包含重复通话：${call.id}`)
    callIDs.add(call.id)
  }
  const reservations = source.reservations.map((value, index) => {
    const path = `response.reservations[${index}]`
    const reservation = objectValue(value, path)
    const controlState = requiredString(
      reservation,
      path,
      'control_state'
    ) as OutgoingCallReservation['control_state']
    if (controlState !== 'owned' && controlState !== 'occupied') {
      throw new Error(`${path}.control_state 未知：${controlState}`)
    }
    return {
      request_id: requiredString(reservation, path, 'request_id'),
      line_id: requiredString(reservation, path, 'line_id'),
      control_state: controlState,
      created_at: requiredTimestamp(reservation, path, 'created_at')
    }
  })
  const reservationIDs = new Set<string>()
  for (const reservation of reservations) {
    if (reservationIDs.has(reservation.request_id)) {
      throw new Error(
        `response.reservations 包含重复预占：${reservation.request_id}`
      )
    }
    reservationIDs.add(reservation.request_id)
  }
  return { calls, reservations }
}

export function parseMessageResponse(value: unknown): Message {
  const source = objectValue(value, 'response')
  return parseMessage(source.message)
}

export function parseTelegramUnit(value: unknown): TelegramUnit {
  const unit = objectValue(value, 'telegram_unit')
  const botUsername = optionalString(unit, 'bot_username')
  const assignedUserID = requiredString(unit, 'telegram_unit', 'assigned_user_id')
  const assignedUsername = optionalString(unit, 'assigned_username')
  return {
    id: requiredString(unit, 'telegram_unit', 'id'),
    display_name: requiredString(unit, 'telegram_unit', 'display_name'),
    enabled: requiredBoolean(unit, 'telegram_unit', 'enabled'),
    chat_id: requiredString(unit, 'telegram_unit', 'chat_id'),
    admin_id: requiredString(unit, 'telegram_unit', 'admin_id'),
    assigned_user_id: assignedUserID,
    ...(assignedUsername ? { assigned_username: assignedUsername } : {}),
    all_assigned_lines: requiredBoolean(unit, 'telegram_unit', 'all_assigned_lines'),
    effective_enabled: requiredBoolean(unit, 'telegram_unit', 'effective_enabled'),
    line_scopes: stringList(unit, 'telegram_unit', 'line_scopes'),
    incoming_sms: requiredBoolean(unit, 'telegram_unit', 'incoming_sms'),
    missed_calls: requiredBoolean(unit, 'telegram_unit', 'missed_calls'),
    token_configured: requiredBoolean(unit, 'telegram_unit', 'token_configured'),
    ...(botUsername ? { bot_username: botUsername } : {}),
    revision: requiredRevision(unit, 'telegram_unit')
  }
}

export function parseTelegramUnitsResponse(value: unknown): TelegramUnit[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.units)) throw new Error('response.units 必须是数组')
  return source.units.map(parseTelegramUnit)
}

export function parseTelegramUnitResponse(value: unknown): TelegramUnit {
  const source = objectValue(value, 'response')
  return parseTelegramUnit(source.unit)
}

export function parseUserAccount(value: unknown): UserAccount {
  const user = objectValue(value, 'user')
  const role = requiredString(user, 'user', 'role')
  if (role !== 'admin' && role !== 'member') {
    throw new Error('user.role must be admin or member')
  }
  const profileName = optionalString(user, 'profile_name')
  const profileAvatar = optionalString(user, 'profile_avatar')
  const pairingCredentialCreatedAt = optionalTimestamp(
    user,
    'user',
    'ios_pairing_credential_created_at'
  )
  const pairingPairedAt = optionalTimestamp(
    user,
    'user',
    'ios_pairing_paired_at'
  )
  return {
    id: requiredString(user, 'user', 'id'),
    username: requiredString(user, 'user', 'username'),
    role,
    enabled: requiredBoolean(user, 'user', 'enabled'),
    ios_pairing_enabled: requiredBoolean(user, 'user', 'ios_pairing_enabled'),
    ios_pairing_has_credential: requiredBoolean(
      user,
      'user',
      'ios_pairing_has_credential'
    ),
    ios_pairing_paired: requiredBoolean(
      user,
      'user',
      'ios_pairing_paired'
    ),
    ...(pairingCredentialCreatedAt
      ? { ios_pairing_credential_created_at: pairingCredentialCreatedAt }
      : {}),
    ...(pairingPairedAt
      ? { ios_pairing_paired_at: pairingPairedAt }
      : {}),
    revision: requiredRevision(user, 'user'),
    ...(profileName ? { profile_name: profileName } : {}),
    ...(profileAvatar ? { profile_avatar: profileAvatar } : {}),
    line_ids: stringList(user, 'user', 'line_ids'),
    created_at: requiredString(user, 'user', 'created_at'),
    updated_at: requiredString(user, 'user', 'updated_at')
  }
}

export function parseUsersResponse(value: unknown): UserAccount[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.users)) throw new Error('response.users must be an array')
  return source.users.map(parseUserAccount)
}

export function parseUserResponse(value: unknown): UserAccount {
  const source = objectValue(value, 'response')
  return parseUserAccount(source.user)
}

export function createMemberPayload(input: CreateMemberInput): CreateMemberInput {
  const username = input.username.trim()
  const lineIDs = input.line_ids.map(value => value.trim())
  if (!username || !input.password) throw new Error('username and password are required')
  if (lineIDs.some(value => !value) || new Set(lineIDs).size !== lineIDs.length) {
    throw new Error('line_ids must contain unique non-empty strings')
  }
  return {
    username,
    password: input.password,
    ios_pairing_enabled: input.ios_pairing_enabled,
    line_ids: lineIDs
  }
}

export function createMemberUpdatePayload(input: UpdateMemberInput): UpdateMemberInput {
  const username = input.username.trim()
  const lineIDs = input.line_ids.map(value => value.trim())
  if (!username || !Number.isSafeInteger(input.revision) || input.revision < 1) {
    throw new Error('username and a positive revision are required')
  }
  if (
    lineIDs.some(value => !value) ||
    new Set(lineIDs).size !== lineIDs.length
  ) {
    throw new Error('line_ids are invalid')
  }
  return {
    username,
    ...(input.password ? { password: input.password } : {}),
    enabled: input.enabled,
    ios_pairing_enabled: input.ios_pairing_enabled,
    line_ids: lineIDs,
    revision: input.revision
  }
}
