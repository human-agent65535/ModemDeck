import { parseMessage } from './normalize.ts'
import type {
  CallAction,
  CallDirection,
  CallPhase,
  CallRecording,
  CallRecordingState,
  CallSession,
  CallPolicyEnforcement,
  DataConnection,
  DeviceConfiguration,
  DeviceConfigurationCapabilities,
  DeviceFeatureCapability,
  DeviceHardwareConfiguration,
  EffectiveIncomingCallPolicy,
  GlobalCallSettings,
  IncomingCallActionResult,
  IncomingCallPolicy,
  IPConfiguration,
  LineIncomingCallConfiguration,
  Message,
  RecordingSettings,
  SendMessageInput,
  TelegramUnit,
  TelegramUnitInput,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput
} from './types.ts'

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
const CALL_ACTIONS = new Set<CallAction>(['answer', 'reject', 'hangup'])
const INCOMING_CALL_POLICIES = new Set<IncomingCallPolicy>([
  'follow_global',
  'receive',
  'do_not_disturb'
])
const EFFECTIVE_INCOMING_CALL_POLICIES = new Set<EffectiveIncomingCallPolicy>([
  'receive',
  'do_not_disturb'
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

export const communicationPaths = {
  messages: '/api/v1/messages',
  calls: '/api/v1/calls',
  activeCalls: '/api/v1/calls/active',
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
  startCall: {
    method: 'POST',
    path: communicationPaths.calls,
    successStatus: 201
  },
  activeCalls: {
    method: 'GET',
    path: communicationPaths.activeCalls,
    successStatus: 200
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

export function callRecordingContract(id: string): {
  update: { method: 'PUT'; path: string; successStatus: 200 }
  list: { method: 'GET'; path: string; successStatus: 200 }
} {
  return {
    update: { method: 'PUT', path: callRecordingPath(id), successStatus: 200 },
    list: { method: 'GET', path: callRecordingsPath(id), successStatus: 200 }
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

export function callActionContract(id: string, action: CallAction | 'dtmf'): {
  method: 'POST'
  path: string
  successStatus: 200
} {
  return {
    method: 'POST',
    path: callActionPath(id, action),
    successStatus: 200
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
  const lineID = input.line_id?.trim()
  const iccid = input.iccid?.trim()
  const to = input.to.trim()
  const content = input.content.trim()
  const requestID = input.request_id?.trim()
  if ((!lineID && !iccid) || !to || !content) {
    throw new Error('line_id 或 iccid，以及 to 和 content 不能为空')
  }
  return {
    ...(requestID ? { request_id: requestID } : {}),
    ...(lineID ? { line_id: lineID } : {}),
    ...(iccid ? { iccid } : {}),
    to,
    content
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
  requestID?: string
): { request_id?: string } {
  if (!CALL_ACTIONS.has(action)) throw new Error(`未知通话操作：${action}`)
  const normalizedRequestID = requestID?.trim()
  return normalizedRequestID ? { request_id: normalizedRequestID } : {}
}

export function createDTMFPayload(
  digits: string,
  requestID?: string
): { request_id?: string; digits: string } {
  const normalizedDigits = digits.trim().toUpperCase()
  const normalizedRequestID = requestID?.trim()
  if (!/^[0-9*#A-D]+$/.test(normalizedDigits)) throw new Error('DTMF 按键无效')
  return {
    ...(normalizedRequestID ? { request_id: normalizedRequestID } : {}),
    digits: normalizedDigits
  }
}

export function createCallMediaPayload(offerSDP: string): { offer_sdp: string } {
  const normalizedOffer = offerSDP.trim()
  if (!normalizedOffer) throw new Error('offer_sdp 不能为空')
  return { offer_sdp: normalizedOffer }
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
  }
}

export function createTelegramUnitPayload(input: TelegramUnitInput): TelegramUnitInput {
  const displayName = input.display_name.trim()
  const chatID = input.chat_id.trim()
  const adminID = input.admin_id.trim()
  const lineScopes = input.line_scopes.map(value => value.trim())
  const token = input.bot_token?.trim()
  if (!displayName || !chatID || !adminID) {
    throw new Error('display_name、chat_id 和 admin_id 不能为空')
  }
  if (lineScopes.some(value => !value) || new Set(lineScopes).size !== lineScopes.length) {
    throw new Error('line_scopes 必须是无重复的非空字符串')
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
    alias: parseFeatureCapability(source.alias, 'hardware.capabilities.alias'),
    esim: parseFeatureCapability(source.esim, 'hardware.capabilities.esim'),
    at_terminal: parseFeatureCapability(
      source.at_terminal,
      'hardware.capabilities.at_terminal'
    ),
    ussd: parseFeatureCapability(source.ussd, 'hardware.capabilities.ussd'),
    connection_profile: parseFeatureCapability(
      source.connection_profile,
      'hardware.capabilities.connection_profile'
    )
  }
}

function parseDeviceHardwareConfiguration(value: unknown): DeviceHardwareConfiguration {
  const source = objectValue(value, 'hardware')
  const identity = objectValue(source.identity, 'hardware.identity')
  const radio = objectValue(source.radio, 'hardware.radio')
  const volte = objectValue(source.volte, 'hardware.volte')
  const policyValue = optionalString(volte, 'policy')
  if (policyValue && policyValue !== 'enabled' && policyValue !== 'disabled') {
    throw new Error(`hardware.volte.policy 未知：${policyValue}`)
  }
  if (!Array.isArray(source.data_connections)) {
    throw new Error('hardware.data_connections 必须是数组')
  }
  const profileID = optionalString(volte, 'profile_id')
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
    data_connections: source.data_connections.map(parseDataConnection),
    volte: {
      policy_known: requiredBoolean(volte, 'hardware.volte', 'policy_known'),
      ...(policyValue ? { policy: policyValue as 'enabled' | 'disabled' } : {}),
      ...(profileID ? { profile_id: profileID } : {})
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
  if (!hardware && !incomingCalls) {
    throw new Error('device_configuration 至少需要 hardware 或 incoming_calls')
  }
  return {
    ...(hardware ? { hardware } : {}),
    ...(incomingCalls ? { incoming_calls: incomingCalls } : {})
  }
}

export function parseCallSession(value: unknown): CallSession {
  const source = objectValue(value, 'call')
  const direction = requiredString(source, 'call', 'direction') as CallDirection
  const phase = requiredString(source, 'call', 'phase') as CallPhase
  if (!CALL_DIRECTIONS.has(direction)) throw new Error(`call.direction 未知：${direction}`)
  if (!CALL_PHASES.has(phase)) throw new Error(`call.phase 未知：${phase}`)

  const displayName = optionalString(source, 'display_name')
  const activeAt = optionalTimestamp(source, 'call', 'active_at')
  const endedAt = optionalTimestamp(source, 'call', 'ended_at')
  const bearer = optionalString(source, 'bearer')
  const failureReason = optionalString(source, 'failure_reason')
  return {
    id: requiredString(source, 'call', 'id'),
    line_key: requiredString(source, 'call', 'line_key'),
    direction,
    remote_number: requiredString(source, 'call', 'remote_number', true),
    ...(displayName ? { display_name: displayName } : {}),
    phase,
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
  return requiredString(source, 'response', 'answer_sdp')
}

export function parseRecordingSettingsResponse(value: unknown): RecordingSettings {
  const source = objectValue(value, 'recording_settings')
  return {
    default_enabled: requiredBoolean(source, 'recording_settings', 'default_enabled'),
    revision: requiredRevision(source, 'recording_settings')
  }
}

export function parseCallRecordingState(value: unknown): CallRecordingState {
  const source = objectValue(value, 'call_recording')
  const startedAt = optionalTimestamp(source, 'call_recording', 'started_at')
  const recordingError = optionalString(source, 'error')
  return {
    call_id: requiredString(source, 'call_recording', 'call_id'),
    enabled: requiredBoolean(source, 'call_recording', 'enabled'),
    active: requiredBoolean(source, 'call_recording', 'active'),
    ...(startedAt ? { started_at: startedAt } : {}),
    ...(recordingError ? { error: recordingError } : {})
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

export function parseCallRecordingsResponse(value: unknown): CallRecording[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.recordings)) throw new Error('response.recordings 必须是数组')
  return source.recordings.map(parseCallRecording)
}

export function parseCallResponse(value: unknown): CallSession {
  const source = objectValue(value, 'response')
  return parseCallSession(source.call)
}

export function parseActiveCallsResponse(value: unknown): CallSession[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source.calls)) throw new Error('response.calls 必须是数组')
  if (source.calls.length > 1) throw new Error('服务端返回了多个活动通话')
  return source.calls.map(parseCallSession)
}

export function parseMessageResponse(value: unknown): Message {
  const source = objectValue(value, 'response')
  return parseMessage(source.message)
}

export function parseTelegramUnit(value: unknown): TelegramUnit {
  const unit = objectValue(value, 'telegram_unit')
  const botUsername = optionalString(unit, 'bot_username')
  return {
    id: requiredString(unit, 'telegram_unit', 'id'),
    display_name: requiredString(unit, 'telegram_unit', 'display_name'),
    enabled: requiredBoolean(unit, 'telegram_unit', 'enabled'),
    chat_id: requiredString(unit, 'telegram_unit', 'chat_id'),
    admin_id: requiredString(unit, 'telegram_unit', 'admin_id'),
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
