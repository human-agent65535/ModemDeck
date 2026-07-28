import type {
  ConfiguredModemDeckGateway,
  GatewayInteractions,
  ListQuery,
  ModemDeckGateway
} from './gateway'
import {
  callActionContract,
  callMediaContract,
  callRecordingContract,
  communicationContracts,
  createCallActionPayload,
  createCallMediaPayload,
  createCallPayload,
  createCallRecordingPayload,
  createDeviceConfigurationPayload,
  createDTMFPayload,
  createGlobalCallSettingsPayload,
  createLineSettingsPayload,
  createSystemSettingsPayload,
  createLineLabelPayload,
  createMessageReadPayload,
  createMessagePayload,
  createNetworkSelectionPayload,
  createProxyPayload,
  createProxyUpdatePayload,
  createRecordingSettingsPayload,
  createTelegramUnitPayload,
  createTLSSettingsPayload,
  parseActiveCallsResponse,
  parseCallMediaResponse,
  parseCallRecordingState,
  parseCallRecordingsResponse,
  parseCallResponse,
  parseDeviceConfigurationResponse,
  parseGlobalCallSettings,
  parseLineSettingsResponse,
  parseSystemSettingsResponse,
  parseLineLabelResponse,
  parseMessageResponse,
  parseMobileNetworkScanResponse,
  parseNetworkSelectionResponse,
  parseNetworkStatusResponse,
  parseProxyCollectionResponse,
  parseProxyDeleteResponse,
  parseProxyMutationResponse,
  parseRecordingEntriesResponse,
  parseRecordingSettingsResponse,
  parseSIMStatusResponse,
  parseTelegramUnitResponse,
  parseTelegramUnitsResponse,
  parseTLSSettingsResponse,
  telegramUnitContract,
  telegramUnitDeletePath,
  deviceConfigurationContract,
  lineLabelPath,
  networkSelectionContract,
  networkContracts,
  proxyDeletePath,
  proxyResourceContract,
  tlsSettingsContract
} from './contract'
import {
  parseBootstrap,
  parseCalls,
  parseContactResponse,
  parseContacts,
  parseDeviceResponse,
  parseDevices,
  parseMessages,
  parseThreads
} from './normalize'
import type {
  AboutInfo,
  ApiErrorBody,
  BootstrapResponse,
  CallFilter,
  CallRecording,
  CallRecordingState,
  CallRecord,
  CallSession,
  ChangePasswordInput,
  CommandReceipt,
  ConnectionProfile,
  Contact,
  ContactInput,
  CommunicationCapabilities,
  CommunicationCapabilityName,
  CreateProxyInput,
  CreateDeviceInput,
  DeleteConnectionProfileInput,
  Device,
  DeviceConfiguration,
  DiagnosticActiveCall,
  DiagnosticLogEntry,
  DiagnosticLogLevel,
  DiagnosticLogPage,
  DiagnosticLogQuery,
  DiagnosticLogStreamHandlers,
  DiagnosticStatus,
  DiagnosticsSnapshot,
  GlobalCallSettings,
  IncomingMessageEvent,
  LineLabelResult,
  LineSummary,
  LoginInput,
  Message,
  MessageEventStreamHandlers,
  MessageThread,
  MobileNetworkScan,
  NetworkSelectionPolicy,
  NetworkStatus,
  ProxyDeleteResult,
  ProxyInstance,
  ProxyMutation,
  RecordingEntry,
  RecordingSettings,
  RenameDeviceInput,
  RuntimeEvent,
  RuntimeEventStreamHandlers,
  RuntimeResource,
  SaveConnectionProfileInput,
  SendMessageInput,
  SessionResponse,
  SetupInput,
  SystemLanguage,
  SIMCommandInput,
  SIMStatus,
  TelegramUnit,
  TelegramUnitInput,
  TLSSettings,
  UpdateCheck,
  UpdateStatus,
  UpdateDeviceConfigurationInput,
  UpdateGlobalCallSettingsInput,
  UpdateLineLabelInput,
  UpdateLineSettingsInput,
  UpdateSystemSettingsInput,
  UpdateNetworkSelectionInput,
  UpdateProxyInput,
  UpdateTLSSettingsInput,
  USSDCommandInput,
  USSDResponse,
  USSDStatus
} from './types'
import { ApiError, isLineColorPresetID } from './types'
import { createFixtureGateway } from './fixture'

const API_ROOT = '/api/v1'
const READ_REQUEST_TIMEOUT_MS = 15_000
const WRITE_REQUEST_TIMEOUT_MS = 60_000
const NETWORK_SCAN_REQUEST_TIMEOUT_MS = 130_000
const RUNTIME_EVENT_INACTIVITY_TIMEOUT_MS = 40_000

const runtimeEnvironment = import.meta.env

export const fixtureMode =
  Boolean(runtimeEnvironment?.DEV) && runtimeEnvironment?.VITE_MODEMDECK_FIXTURE === '1'

const fixturePreviewOptions =
  fixtureMode && typeof window !== 'undefined'
    ? {
        initialIncomingCall:
          new URLSearchParams(window.location.search).get('incomingCallFixture') === '1'
      }
    : {}

const REAL_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: true,
  dial: true,
  telegram: true
}

const FIXTURE_INTERACTIONS: GatewayInteractions = {
  contacts: true,
  message: true,
  dial: true,
  telegram: true
}

let currentCSRFToken = ''
let authenticationRequiredHandler: () => void = () => undefined

function requestID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = ((bytes[6] || 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] || 0) & 0x3f) | 0x80
  const encoded = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  return `${encoded.slice(0, 8)}-${encoded.slice(8, 12)}-${encoded.slice(12, 16)}-${encoded.slice(16, 20)}-${encoded.slice(20)}`
}

export function setClientCSRFToken(token?: string): void {
  currentCSRFToken = token?.trim() || ''
}

export function setAuthenticationRequiredHandler(handler: () => void): void {
  authenticationRequiredHandler = handler
}

function queryString(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined
}

function stringProperty(source: Record<string, unknown> | undefined, key: string): string | undefined {
  const value = source?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function errorDetails(value: unknown): ApiErrorBody | undefined {
  const source = recordValue(value)
  if (!source) return undefined
  return {
    code: stringProperty(source, 'code'),
    message: stringProperty(source, 'message'),
    detail: stringProperty(source, 'detail'),
    field: stringProperty(source, 'field')
  }
}

function parseSession(value: unknown): SessionResponse {
  const source = recordValue(value)
  if (
    !source ||
    typeof source.authenticated !== 'boolean' ||
    typeof source.setup_required !== 'boolean'
  ) {
    throw new ApiError('ModemDeck 服务返回了无效的会话状态', 0, 'invalid_response')
  }

  const language = source.language
  if (language !== 'auto' && language !== 'zh-CN' && language !== 'en-US') {
    throw new ApiError('ModemDeck returned an unsupported system language', 0, 'invalid_response')
  }
  const session: SessionResponse = {
    authenticated: source.authenticated,
    setup_required: source.setup_required,
    username: stringProperty(source, 'username'),
    csrf_token: stringProperty(source, 'csrf_token'),
    language: language as SystemLanguage
  }
  if (session.authenticated && (!session.username || !session.csrf_token)) {
    throw new ApiError('ModemDeck 服务返回了不完整的会话状态', 0, 'invalid_response')
  }
  return session
}

function requiredRecord(value: unknown, path: string): Record<string, unknown> {
  const source = recordValue(value)
  if (!source) throw new ApiError(`${path} 必须是对象`, 0, 'invalid_response')
  return source
}

function stringValue(source: Record<string, unknown>, key: string): string {
  const value = source[key]
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

function requiredStringValue(
  source: Record<string, unknown>,
  path: string,
  key: string
): string {
  const value = stringValue(source, key)
  if (!value) throw new ApiError(`${path}.${key} 不能为空`, 0, 'invalid_response')
  return value
}

function requiredBooleanValue(
  source: Record<string, unknown>,
  path: string,
  key: string
): boolean {
  const value = source[key]
  if (typeof value !== 'boolean') {
    throw new ApiError(`${path}.${key} 必须是布尔值`, 0, 'invalid_response')
  }
  return value
}

const UPDATE_STATUSES = new Set<UpdateStatus>([
  'up_to_date',
  'update_available',
  'development',
  'unavailable'
])

function parseAbout(value: unknown): AboutInfo {
  const source = requiredRecord(value, 'about')
  return {
    name: requiredStringValue(source, 'about', 'name'),
    version: requiredStringValue(source, 'about', 'version'),
    commit: requiredStringValue(source, 'about', 'commit'),
    build_date: requiredStringValue(source, 'about', 'build_date'),
    repository_url: requiredStringValue(source, 'about', 'repository_url'),
    license_name: requiredStringValue(source, 'about', 'license_name'),
    license_url: requiredStringValue(source, 'about', 'license_url'),
    notices_url: requiredStringValue(source, 'about', 'notices_url')
  }
}

function parseUpdateCheck(value: unknown): UpdateCheck {
  const source = requiredRecord(value, 'update_check')
  const status = requiredStringValue(source, 'update_check', 'status') as UpdateStatus
  if (!UPDATE_STATUSES.has(status)) {
    throw new ApiError(`update_check.status 未知：${status}`, 0, 'invalid_response')
  }
  return {
    status,
    current_version: requiredStringValue(source, 'update_check', 'current_version'),
    latest_version: stringValue(source, 'latest_version') || undefined,
    release_name: stringValue(source, 'release_name') || undefined,
    release_url: stringValue(source, 'release_url') || undefined,
    published_at: stringValue(source, 'published_at') || undefined,
    checked_at: requiredStringValue(source, 'update_check', 'checked_at'),
    error_code: stringValue(source, 'error_code') || undefined
  }
}

function numberValue(source: Record<string, unknown>, path: string, key: string): number {
  const value = source[key]
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new ApiError(`${path}.${key} 必须是非负数`, 0, 'invalid_response')
  }
  return value
}

const DIAGNOSTIC_CAPABILITIES: Array<[CommunicationCapabilityName, string]> = [
  ['modem', 'modem'],
  ['sim', 'sim'],
  ['voice', 'voice'],
  ['messaging', 'messaging'],
  ['media', 'media'],
  ['dial', 'dial'],
  ['answer', 'answer_call'],
  ['reject', 'reject_call'],
  ['hangup', 'hangup_call'],
  ['dtmf', 'send_dtmf'],
  ['message', 'send_message']
]

function parseDiagnosticCapabilities(value: unknown, path: string): CommunicationCapabilities {
  const source = requiredRecord(value, path)
  const result: CommunicationCapabilities = {}
  for (const [name, wireName] of DIAGNOSTIC_CAPABILITIES) {
    result[name] = requiredBooleanValue(source, path, wireName)
  }
  return result
}

function parseDiagnosticLine(value: unknown, index: number): LineSummary {
  const path = `diagnostics.lines[${index}]`
  const source = requiredRecord(value, path)
  const id = requiredStringValue(source, path, 'id')
  const iccid = stringValue(source, 'iccid')
  const imsi = stringValue(source, 'imsi')
  const deviceIMEI = stringValue(source, 'device_imei')
  const rawSignal = source.signal_quality
  const signalQuality =
    typeof rawSignal === 'number' && Number.isFinite(rawSignal) ? rawSignal : undefined
  const rawLineColor = stringValue(source, 'line_color')
  return {
    id,
    iccid,
    imsi,
    phone_number: stringValue(source, 'phone_number'),
    operator: stringValue(source, 'operator'),
    home_operator_code: stringValue(source, 'home_operator_code'),
    home_operator_name: stringValue(source, 'home_operator_name'),
    serving_operator_code: stringValue(source, 'serving_operator_code'),
    serving_operator_name: stringValue(source, 'serving_operator_name'),
    registration_state_known: requiredBooleanValue(
      source,
      path,
      'registration_state_known'
    ),
    registration_state_code: numberValue(source, path, 'registration_state_code'),
    registration_state: stringValue(source, 'registration_state'),
    roaming: requiredBooleanValue(source, path, 'roaming'),
    emergency_only: requiredBooleanValue(source, path, 'emergency_only'),
    device_imei: deviceIMEI,
    device_name: stringValue(source, 'device_name'),
    line_label: stringValue(source, 'line_label'),
    line_color: isLineColorPresetID(rawLineColor) ? rawLineColor : '',
    model: stringValue(source, 'model') || undefined,
    firmware: stringValue(source, 'firmware') || undefined,
    state: stringValue(source, 'state') || undefined,
    radio_desired_enabled: requiredBooleanValue(
      source,
      path,
      'radio_desired_enabled'
    ),
    radio_desired_enabled_known: requiredBooleanValue(
      source,
      path,
      'radio_desired_enabled_known'
    ),
    signal_quality: signalQuality,
    capabilities: parseDiagnosticCapabilities(source.capabilities, `${path}.capabilities`)
  }
}

function parseDiagnosticAvailability(value: unknown, path: string) {
  const source = requiredRecord(value, path)
  return {
    available: requiredBooleanValue(source, path, 'available'),
    error: stringValue(source, 'error') || undefined
  }
}

function parseCommandReceipt(value: unknown): CommandReceipt {
  const response = requiredRecord(value, 'command_response')
  const receipt = requiredRecord(response.receipt, 'command_response.receipt')
  return {
    request_id: requiredStringValue(receipt, 'command_response.receipt', 'request_id'),
    resource_id: requiredStringValue(receipt, 'command_response.receipt', 'resource_id')
  }
}

function parseConnectionProfile(value: unknown, path: string): ConnectionProfile {
  const source = requiredRecord(value, path)
  return {
    profile_id: numberValue(source, path, 'profile_id'),
    profile_name: stringValue(source, 'profile_name'),
    apn: stringValue(source, 'apn'),
    ip_family: stringValue(source, 'ip_family'),
    ip_type: numberValue(source, path, 'ip_type'),
    apn_type: numberValue(source, path, 'apn_type'),
    allowed_auth: numberValue(source, path, 'allowed_auth'),
    user: stringValue(source, 'user') || undefined,
    access_type_preference: numberValue(source, path, 'access_type_preference'),
    roaming_allowance: numberValue(source, path, 'roaming_allowance'),
    profile_source: numberValue(source, path, 'profile_source')
  }
}

function parseConnectionProfiles(value: unknown): ConnectionProfile[] {
  const response = requiredRecord(value, 'profiles_response')
  if (!Array.isArray(response.profiles)) {
    throw new ApiError('profiles_response.profiles 必须是数组', 0, 'invalid_response')
  }
  return response.profiles.map((profile, index) =>
    parseConnectionProfile(profile, `profiles_response.profiles[${index}]`)
  )
}

function parseSavedConnectionProfile(value: unknown): ConnectionProfile {
  const response = requiredRecord(value, 'profile_response')
  return parseConnectionProfile(response.profile, 'profile_response.profile')
}

function parseUSSDStatus(value: unknown): USSDStatus {
  const response = requiredRecord(value, 'ussd_response')
  const source = requiredRecord(response.ussd, 'ussd_response.ussd')
  return {
    line_id: requiredStringValue(source, 'ussd_response.ussd', 'line_id'),
    state: requiredStringValue(source, 'ussd_response.ussd', 'state'),
    state_code: numberValue(source, 'ussd_response.ussd', 'state_code'),
    network_notification: stringValue(source, 'network_notification') || undefined,
    network_request: stringValue(source, 'network_request') || undefined,
    observed_at: requiredStringValue(source, 'ussd_response.ussd', 'observed_at')
  }
}

function parseUSSDResult(value: unknown): USSDResponse {
  const response = requiredRecord(value, 'ussd_command_response')
  const result = requiredRecord(response.result, 'ussd_command_response.result')
  return { response: stringValue(result, 'response') || undefined }
}

function parseDiagnosticAgentCapabilities(value: unknown) {
  const path = 'diagnostics.host_agent.capabilities'
  const source = requiredRecord(value, path)
  return {
    discovery: requiredBooleanValue(source, path, 'discovery'),
    snapshot: requiredBooleanValue(source, path, 'snapshot'),
    device_configuration: requiredBooleanValue(source, path, 'device_configuration'),
    network: requiredBooleanValue(source, path, 'network'),
    proxy: requiredBooleanValue(source, path, 'proxy'),
    dial: requiredBooleanValue(source, path, 'dial'),
    answer_call: requiredBooleanValue(source, path, 'answer_call'),
    reject_call: requiredBooleanValue(source, path, 'reject_call'),
    hangup_call: requiredBooleanValue(source, path, 'hangup_call'),
    send_dtmf: requiredBooleanValue(source, path, 'send_dtmf'),
    send_message: requiredBooleanValue(source, path, 'send_message'),
    sim_management: requiredBooleanValue(source, path, 'sim_management'),
    connection_profiles: requiredBooleanValue(source, path, 'connection_profiles'),
    ussd: requiredBooleanValue(source, path, 'ussd'),
    media:
      source.media === undefined
        ? false
        : requiredBooleanValue(source, path, 'media')
  }
}

function parseDiagnosticCall(value: unknown, index: number): DiagnosticActiveCall {
  const path = `diagnostics.active_calls[${index}]`
  const source = requiredRecord(value, path)
  const audioRate = source.audio_rate
  return {
    id: requiredStringValue(source, path, 'id'),
    line_id: requiredStringValue(source, path, 'line_id'),
    endpoint_line_id: stringValue(source, 'endpoint_line_id') || undefined,
    direction: requiredStringValue(source, path, 'direction'),
    phase: requiredStringValue(source, path, 'phase'),
    bearer: stringValue(source, 'bearer'),
    media_available: requiredBooleanValue(source, path, 'media_available'),
    audio_encoding: stringValue(source, 'audio_encoding') || undefined,
    audio_resolution: stringValue(source, 'audio_resolution') || undefined,
    audio_rate:
      typeof audioRate === 'number' && Number.isFinite(audioRate) && audioRate > 0
        ? audioRate
        : undefined
  }
}

function parseDiagnostics(value: unknown): DiagnosticsSnapshot {
  const source = requiredRecord(value, 'diagnostics')
  const status = requiredStringValue(source, 'diagnostics', 'status')
  if (status !== 'ok' && status !== 'degraded' && status !== 'unavailable') {
    throw new ApiError(`diagnostics.status 未知：${status}`, 0, 'invalid_response')
  }
  const hostAgentSource = requiredRecord(source.host_agent, 'diagnostics.host_agent')
  if (!Array.isArray(source.lines) || !Array.isArray(source.active_calls)) {
    throw new ApiError('diagnostics 的线路或通话列表无效', 0, 'invalid_response')
  }
  return {
    status: status as DiagnosticStatus,
    observed_at: requiredStringValue(source, 'diagnostics', 'observed_at'),
    database: parseDiagnosticAvailability(source.database, 'diagnostics.database'),
    host_agent: {
      connected: requiredBooleanValue(
        hostAgentSource,
        'diagnostics.host_agent',
        'connected'
      ),
      provider: stringValue(hostAgentSource, 'provider'),
      agent_version: stringValue(hostAgentSource, 'agent_version'),
      runtime_version: stringValue(hostAgentSource, 'runtime_version'),
      boot_epoch: stringValue(hostAgentSource, 'boot_epoch'),
      revision: stringValue(hostAgentSource, 'revision'),
      observed_at: stringValue(hostAgentSource, 'observed_at'),
      last_error: stringValue(hostAgentSource, 'last_error') || undefined,
      capabilities: parseDiagnosticAgentCapabilities(hostAgentSource.capabilities)
    },
    call_runtime: parseDiagnosticAvailability(
      source.call_runtime,
      'diagnostics.call_runtime'
    ),
    lines: source.lines.map(parseDiagnosticLine),
    active_calls: source.active_calls.map(parseDiagnosticCall)
  }
}

function parseDiagnosticLogEntry(value: unknown, path = 'diagnostic_log'): DiagnosticLogEntry {
  const source = requiredRecord(value, path)
  const level = requiredStringValue(source, path, 'level')
  if (level !== 'debug' && level !== 'info' && level !== 'warn' && level !== 'error') {
    throw new ApiError(`${path}.level 未知：${level}`, 0, 'invalid_response')
  }
  const fields = source.fields === undefined ? undefined : requiredRecord(source.fields, `${path}.fields`)
  return {
    id: numberValue(source, path, 'id'),
    timestamp: requiredStringValue(source, path, 'timestamp'),
    level: level as DiagnosticLogLevel,
    component: requiredStringValue(source, path, 'component'),
    caller: stringValue(source, 'caller') || undefined,
    message: requiredStringValue(source, path, 'message'),
    fields: fields ? { ...fields } : undefined
  }
}

function parseIncomingMessageEvent(value: unknown): IncomingMessageEvent {
  const source = requiredRecord(value, 'incoming_message_event')
  return {
    id: numberValue(source, 'incoming_message_event', 'id'),
    event_key: requiredStringValue(source, 'incoming_message_event', 'event_key'),
    message_id: requiredStringValue(source, 'incoming_message_event', 'message_id'),
    thread_key: requiredStringValue(source, 'incoming_message_event', 'thread_key'),
    line_id: requiredStringValue(source, 'incoming_message_event', 'line_id'),
    peer: requiredStringValue(source, 'incoming_message_event', 'peer'),
    content: stringValue(source, 'content'),
    timestamp: requiredStringValue(source, 'incoming_message_event', 'timestamp')
  }
}

const RUNTIME_RESOURCES = new Set<RuntimeResource>([
  'lines',
  'network',
  'calls',
  'messages'
])

function parseRuntimeEvent(value: unknown): RuntimeEvent {
  const source = requiredRecord(value, 'runtime_event')
  if (!Array.isArray(source.resources) || source.resources.length === 0) {
    throw new ApiError('runtime_event.resources 必须是非空数组', 0, 'invalid_response')
  }
  const resources: RuntimeResource[] = []
  for (const [index, value] of source.resources.entries()) {
    if (typeof value !== 'string' || !RUNTIME_RESOURCES.has(value as RuntimeResource)) {
      throw new ApiError(
        `runtime_event.resources[${index}] 是未知资源`,
        0,
        'invalid_response'
      )
    }
    const resource = value as RuntimeResource
    if (!resources.includes(resource)) resources.push(resource)
  }
  const observedAt = requiredStringValue(source, 'runtime_event', 'observed_at')
  if (Number.isNaN(Date.parse(observedAt))) {
    throw new ApiError('runtime_event.observed_at 必须是有效时间', 0, 'invalid_response')
  }
  return {
    id: numberValue(source, 'runtime_event', 'id'),
    resources,
    observed_at: observedAt
  }
}

function parseDiagnosticLogPage(value: unknown): DiagnosticLogPage {
  const source = requiredRecord(value, 'diagnostic_logs')
  if (!Array.isArray(source.entries)) {
    throw new ApiError('diagnostic_logs.entries 必须是数组', 0, 'invalid_response')
  }
  return {
    entries: source.entries.map((entry, index) =>
      parseDiagnosticLogEntry(entry, `diagnostic_logs.entries[${index}]`)
    ),
    oldest_id: numberValue(source, 'diagnostic_logs', 'oldest_id'),
    newest_id: numberValue(source, 'diagnostic_logs', 'newest_id'),
    truncated: requiredBooleanValue(source, 'diagnostic_logs', 'truncated')
  }
}

function diagnosticLogQueryString(query: DiagnosticLogQuery = {}): string {
  return queryString({
    after: query.after === undefined ? undefined : String(query.after),
    limit: query.limit === undefined ? undefined : String(query.limit),
    level: query.level,
    component: query.component?.trim(),
    search: query.search?.trim()
  })
}

async function responseBody(response: Response): Promise<unknown> {
  if (response.status === 204) return undefined

  const text = await response.text()
  if (!text.trim()) return undefined
  if (!response.headers.get('content-type')?.toLocaleLowerCase().includes('application/json')) {
    return text
  }

  try {
    return JSON.parse(text) as unknown
  } catch {
    if (!response.ok) return text
    throw new ApiError('ModemDeck 服务返回了无效的 JSON', response.status, 'invalid_response')
  }
}

async function request(
  path: string,
  init: RequestInit,
  expectedStatus: number,
  timeoutMilliseconds?: number
): Promise<unknown> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS' && currentCSRFToken) {
    headers.set('X-ModemDeck-CSRF', currentCSRFToken)
  }
  const timeoutSignal = AbortSignal.timeout(
    timeoutMilliseconds ??
      (method === 'GET' || method === 'HEAD' || method === 'OPTIONS'
        ? READ_REQUEST_TIMEOUT_MS
        : WRITE_REQUEST_TIMEOUT_MS)
  )
  const signal = init.signal
    ? AbortSignal.any([init.signal, timeoutSignal])
    : timeoutSignal

  let response: Response
  try {
    response = await fetch(path, { ...init, headers, signal, credentials: 'same-origin' })
  } catch (error) {
    if (init.signal?.aborted) throw error
    if (timeoutSignal.aborted) {
      throw new ApiError('ModemDeck 请求超时', 0, 'request_timeout')
    }
    throw new ApiError('无法连接 ModemDeck 服务')
  }

  let body: unknown
  try {
    body = await responseBody(response)
  } catch (error) {
    if (init.signal?.aborted) throw error
    if (timeoutSignal.aborted) {
      throw new ApiError('ModemDeck 请求超时', 0, 'request_timeout')
    }
    if (error instanceof ApiError) throw error
    throw new ApiError('无法读取 ModemDeck 服务响应', response.status, 'invalid_response')
  }

  if (!response.ok) {
    const details = errorDetails(body)
    if (response.status === 401) authenticationRequiredHandler()
    const message =
      details?.message ||
      details?.detail ||
      (typeof body === 'string' && body.trim() ? body.trim() : `请求失败（${response.status}）`)
    throw new ApiError(message, response.status, details?.code, details?.field)
  }
  if (response.status !== expectedStatus) {
    throw new ApiError(
      `ModemDeck 服务返回了意外状态（${response.status}，预期 ${expectedStatus}）`,
      response.status,
      'unexpected_status'
    )
  }
  return body
}

function get(path: string): Promise<unknown> {
  return request(
    path,
    {
      method: 'GET',
      headers: { Accept: 'application/json' }
    },
    200
  )
}

function writeJSON(
  path: string,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  input: unknown,
  expectedStatus: number,
  signal?: AbortSignal,
  timeoutMilliseconds?: number
) {
  return request(
    path,
    {
      method,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(input),
      signal
    },
    expectedStatus,
    timeoutMilliseconds
  )
}

type EventSourceLifecycleHandlers = {
  onOpen: () => void
  onError: (error?: Error) => void
}

function subscribeEventSource(
  path: string,
  handlers: EventSourceLifecycleHandlers,
  bind: (
    source: EventSource,
    restart: (error: Error) => void,
    isActive: () => boolean,
    markActivity: () => void
  ) => void,
  inactivityTimeoutMilliseconds?: number
): () => void {
  let source: EventSource | undefined
  let inactivityTimer: number | undefined
  let stopped = false

  const clearInactivityTimer = () => {
    if (inactivityTimer === undefined) return
    globalThis.clearTimeout(inactivityTimer)
    inactivityTimer = undefined
  }

  const connect = () => {
    if (stopped) return
    const current = new EventSource(path, { withCredentials: true })
    source = current
    const isActive = () => !stopped && source === current
    const restart = (error: Error) => {
      if (!isActive()) return
      clearInactivityTimer()
      current.close()
      handlers.onError(error)
      connect()
    }
    const markActivity = () => {
      if (!isActive() || !inactivityTimeoutMilliseconds) return
      clearInactivityTimer()
      inactivityTimer = globalThis.setTimeout(() => {
        restart(new Error('运行时事件流长时间没有响应'))
      }, inactivityTimeoutMilliseconds)
    }
    current.onopen = () => {
      if (!isActive()) return
      markActivity()
      handlers.onOpen()
    }
    current.onerror = () => {
      if (isActive()) handlers.onError()
    }
    bind(current, restart, isActive, markActivity)
    markActivity()
  }

  connect()
  return () => {
    if (stopped) return
    stopped = true
    clearInactivityTimer()
    source?.close()
  }
}

const realGateway: ConfiguredModemDeckGateway = {
  interactions: REAL_INTERACTIONS,

  async getAbout(): Promise<AboutInfo> {
    return parseAbout(await get(`${API_ROOT}/about`))
  },

  async checkForUpdates(): Promise<UpdateCheck> {
    return parseUpdateCheck(await get(`${API_ROOT}/updates/check`))
  },

  async getSession(): Promise<SessionResponse> {
    return parseSession(await get(`${API_ROOT}/session`))
  },

  async setup(input: SetupInput): Promise<SessionResponse> {
    return parseSession(await writeJSON(`${API_ROOT}/setup`, 'POST', input, 201))
  },

  async login(input: LoginInput): Promise<SessionResponse> {
    return parseSession(await writeJSON(`${API_ROOT}/session`, 'POST', input, 200))
  },

  async changePassword(input: ChangePasswordInput): Promise<void> {
    await writeJSON(`${API_ROOT}/account/password`, 'PUT', input, 204)
  },

  async logout(): Promise<void> {
    await request(
      `${API_ROOT}/session`,
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' }
      },
      204
    )
  },

  async getBootstrap(): Promise<BootstrapResponse> {
    return parseBootstrap(await get(`${API_ROOT}/bootstrap`))
  },

  async getSystemSettings() {
    return parseSystemSettingsResponse(await get(`${API_ROOT}/settings/system`))
  },

  async updateSystemSettings(input: UpdateSystemSettingsInput) {
    return parseSystemSettingsResponse(
      await writeJSON(
        `${API_ROOT}/settings/system`,
        'PATCH',
        createSystemSettingsPayload(input),
        200
      )
    )
  },

  async listContacts(query: ListQuery = {}): Promise<Contact[]> {
    return parseContacts(await get(`${API_ROOT}/contacts${queryString({ q: query.q })}`))
  },

  async createContact(input: ContactInput): Promise<Contact> {
    return parseContactResponse(await writeJSON(`${API_ROOT}/contacts`, 'POST', input, 201))
  },

  async updateContact(id: string, input: ContactInput): Promise<Contact> {
    const contactID = encodeURIComponent(id)
    return parseContactResponse(
      await writeJSON(`${API_ROOT}/contacts/${contactID}`, 'PUT', input, 200)
    )
  },

  async deleteContact(id: string, revision?: number): Promise<void> {
    if (!Number.isSafeInteger(revision) || Number(revision) <= 0) {
      throw new ApiError('删除联系人需要有效的 revision', 0, 'invalid_revision', 'revision')
    }
    const contactID = encodeURIComponent(id)
    await request(
      `${API_ROOT}/contacts/${contactID}${queryString({ revision: String(revision) })}`,
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' }
      },
      204
    )
  },

  async listThreads(query: ListQuery = {}): Promise<MessageThread[]> {
    return parseThreads(await get(`${API_ROOT}/messages/threads${queryString({ q: query.q })}`))
  },

  async listMessages(query): Promise<Message[]> {
    return parseMessages(
      await get(
        `${API_ROOT}/messages${queryString({
          line_id: query.line_id,
          peer: query.peer
        })}`
      )
    )
  },

  async markThreadRead(query): Promise<void> {
    const contract = communicationContracts.markMessageRead
    await writeJSON(
      contract.path,
      contract.method,
      createMessageReadPayload(query),
      contract.successStatus
    )
  },

  async sendMessage(input: SendMessageInput): Promise<Message> {
    const contract = communicationContracts.sendMessage
    return parseMessageResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createMessagePayload({ ...input, request_id: input.request_id || requestID() }),
        contract.successStatus
      )
    )
  },

  async listCalls(filter: CallFilter = 'all', query: ListQuery = {}): Promise<CallRecord[]> {
    return parseCalls(
      await get(`${API_ROOT}/calls${queryString({ kind: filter, q: query.q })}`)
    )
  },

  async markMissedCallsRead(): Promise<void> {
    const contract = communicationContracts.markMissedCallsRead
    await writeJSON(contract.path, contract.method, {}, contract.successStatus)
  },

  async listDevices(): Promise<Device[]> {
    return parseDevices(await get(`${API_ROOT}/devices`))
  },

  async createDevice(input: CreateDeviceInput): Promise<Device> {
    const imei = input.imei.trim()
    if (!imei) throw new Error('IMEI 不能为空')
    return parseDeviceResponse(
      await writeJSON(
        `${API_ROOT}/devices`,
        'POST',
        { imei, name: input.name?.trim() || '' },
        201
      )
    )
  },

  async renameDevice(imei: string, input: RenameDeviceInput): Promise<Device> {
    const normalizedIMEI = imei.trim()
    if (!normalizedIMEI) throw new Error('IMEI 不能为空')
    return parseDeviceResponse(
      await writeJSON(
        `${API_ROOT}/devices/${encodeURIComponent(normalizedIMEI)}`,
        'PATCH',
        { name: input.name.trim() },
        200
      )
    )
  },

  async deleteDevice(imei: string): Promise<void> {
    const normalizedIMEI = imei.trim()
    if (!normalizedIMEI) throw new Error('IMEI 不能为空')
    await request(
      `${API_ROOT}/devices/${encodeURIComponent(normalizedIMEI)}`,
      {
        method: 'DELETE',
        headers: { Accept: 'application/json' }
      },
      204
    )
  },

  async updateLineLabel(lineID: string, input: UpdateLineLabelInput): Promise<LineLabelResult> {
    return parseLineLabelResponse(
      await writeJSON(
        lineLabelPath(lineID),
        'PATCH',
        createLineLabelPayload(input),
        200
      )
    )
  },

  async getNetworkStatus(): Promise<NetworkStatus> {
    return parseNetworkStatusResponse(await get(networkContracts.status.path))
  },

  async getNetworkSelection(lineID: string): Promise<NetworkSelectionPolicy> {
    const contract = networkSelectionContract(lineID).get
    return parseNetworkSelectionResponse(await get(contract.path))
  },

  async updateNetworkSelection(
    lineID: string,
    input: UpdateNetworkSelectionInput
  ): Promise<NetworkSelectionPolicy> {
    const contract = networkSelectionContract(lineID).update
    return parseNetworkSelectionResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createNetworkSelectionPayload(input),
        contract.successStatus
      )
    )
  },

  async scanMobileNetworks(
    lineID: string,
    signal?: AbortSignal
  ): Promise<MobileNetworkScan> {
    const contract = networkSelectionContract(lineID).scan
    return parseMobileNetworkScanResponse(
      await writeJSON(
        contract.path,
        contract.method,
        {},
        contract.successStatus,
        signal,
        NETWORK_SCAN_REQUEST_TIMEOUT_MS
      )
    )
  },

  async listProxies(): Promise<ProxyInstance[]> {
    return parseProxyCollectionResponse(await get(networkContracts.listProxies.path))
  },

  async createProxy(input: CreateProxyInput): Promise<ProxyMutation> {
    const contract = networkContracts.createProxy
    return parseProxyMutationResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createProxyPayload(input),
        contract.successStatus
      )
    )
  },

  async updateProxy(id: string, input: UpdateProxyInput): Promise<ProxyMutation> {
    const contract = proxyResourceContract(id).update
    return parseProxyMutationResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createProxyUpdatePayload(input),
        contract.successStatus
      )
    )
  },

  async deleteProxy(id: string, revision: number): Promise<ProxyDeleteResult> {
    const contract = proxyResourceContract(id).delete
    return parseProxyDeleteResponse(
      await request(
        proxyDeletePath(id, revision),
        {
          method: contract.method,
          headers: { Accept: 'application/json' }
        },
        contract.successStatus
      )
    )
  },

  async getSIMStatus(lineID: string): Promise<SIMStatus> {
    return parseSIMStatusResponse(
      await get(`${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/sim`)
    )
  },

  async commandSIM(lineID: string, input: SIMCommandInput): Promise<CommandReceipt> {
    return parseCommandReceipt(
      await writeJSON(
        `${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/sim/commands`,
        'POST',
        { request_id: requestID(), ...input },
        200
      )
    )
  },

  async listConnectionProfiles(lineID: string): Promise<ConnectionProfile[]> {
    return parseConnectionProfiles(
      await get(`${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/profiles`)
    )
  },

  async saveConnectionProfile(
    lineID: string,
    input: SaveConnectionProfileInput
  ): Promise<ConnectionProfile> {
    return parseSavedConnectionProfile(
      await writeJSON(
        `${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/profiles`,
        'PUT',
        { request_id: requestID(), ...input },
        200
      )
    )
  },

  async deleteConnectionProfile(
    lineID: string,
    input: DeleteConnectionProfileInput
  ): Promise<CommandReceipt> {
    return parseCommandReceipt(
      await writeJSON(
        `${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/profiles`,
        'DELETE',
        { request_id: requestID(), ...input },
        200
      )
    )
  },

  async getUSSDStatus(lineID: string): Promise<USSDStatus> {
    return parseUSSDStatus(
      await get(`${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/ussd`)
    )
  },

  async commandUSSD(lineID: string, input: USSDCommandInput): Promise<USSDResponse> {
    return parseUSSDResult(
      await writeJSON(
        `${API_ROOT}/devices/${encodeURIComponent(lineID.trim())}/ussd`,
        'POST',
        { request_id: requestID(), ...input },
        200
      )
    )
  },

  async getDiagnostics(): Promise<DiagnosticsSnapshot> {
    return parseDiagnostics(await get(`${API_ROOT}/diagnostics`))
  },

  async listDiagnosticLogs(query: DiagnosticLogQuery = {}): Promise<DiagnosticLogPage> {
    return parseDiagnosticLogPage(
      await get(`${API_ROOT}/diagnostics/logs${diagnosticLogQueryString(query)}`)
    )
  },

  subscribeMessageEvents(handlers: MessageEventStreamHandlers): () => void {
    return subscribeEventSource(
      `${API_ROOT}/messages/events`,
      handlers,
      (source, restart, isActive) => {
        source.addEventListener('sms', event => {
          if (!isActive()) return
          try {
            handlers.onMessage(parseIncomingMessageEvent(JSON.parse(event.data) as unknown))
          } catch (error) {
            restart(error instanceof Error ? error : new Error('短信事件格式无效'))
          }
        })
        source.addEventListener('ready', event => {
          if (!isActive()) return
          try {
            const ready = requiredRecord(JSON.parse(event.data) as unknown, 'message_event_ready')
            handlers.onReady(numberValue(ready, 'message_event_ready', 'newest_id'))
          } catch (error) {
            restart(error instanceof Error ? error : new Error('短信事件就绪状态无效'))
          }
        })
        source.addEventListener('reset', event => {
          if (!isActive()) return
          try {
            const reset = requiredRecord(JSON.parse(event.data) as unknown, 'message_event_reset')
            handlers.onReset(
              numberValue(reset, 'message_event_reset', 'oldest_id'),
              numberValue(reset, 'message_event_reset', 'newest_id')
            )
          } catch (error) {
            restart(error instanceof Error ? error : new Error('短信事件重置状态无效'))
          }
        })
      }
    )
  },

  subscribeRuntimeEvents(handlers: RuntimeEventStreamHandlers): () => void {
    return subscribeEventSource(
      `${API_ROOT}/runtime/events`,
      handlers,
      (source, restart, isActive, markActivity) => {
        source.addEventListener('runtime', event => {
          if (!isActive()) return
          markActivity()
          try {
            handlers.onEvent(parseRuntimeEvent(JSON.parse(event.data) as unknown))
          } catch (error) {
            restart(error instanceof Error ? error : new Error('运行时事件格式无效'))
          }
        })
        source.addEventListener('heartbeat', event => {
          if (!isActive()) return
          markActivity()
          try {
            const heartbeat = requiredRecord(
              JSON.parse(event.data) as unknown,
              'runtime_event_heartbeat'
            )
            handlers.onHeartbeat(
              requiredStringValue(heartbeat, 'runtime_event_heartbeat', 'at')
            )
          } catch (error) {
            restart(error instanceof Error ? error : new Error('运行时事件心跳无效'))
          }
        })
        source.addEventListener('ready', event => {
          if (!isActive()) return
          markActivity()
          try {
            const ready = requiredRecord(JSON.parse(event.data) as unknown, 'runtime_event_ready')
            handlers.onReady(numberValue(ready, 'runtime_event_ready', 'newest_id'))
          } catch (error) {
            restart(error instanceof Error ? error : new Error('运行时事件就绪状态无效'))
          }
        })
        source.addEventListener('reset', event => {
          if (!isActive()) return
          markActivity()
          try {
            const reset = requiredRecord(JSON.parse(event.data) as unknown, 'runtime_event_reset')
            handlers.onReset(
              numberValue(reset, 'runtime_event_reset', 'oldest_id'),
              numberValue(reset, 'runtime_event_reset', 'newest_id')
            )
          } catch (error) {
            restart(error instanceof Error ? error : new Error('运行时事件重置状态无效'))
          }
        })
      },
      RUNTIME_EVENT_INACTIVITY_TIMEOUT_MS
    )
  },

  subscribeDiagnosticLogs(
    query: DiagnosticLogQuery,
    handlers: DiagnosticLogStreamHandlers
  ): () => void {
    const source = new EventSource(
      `${API_ROOT}/diagnostics/logs/stream${diagnosticLogQueryString(query)}`,
      { withCredentials: true }
    )
    let closed = false
    const close = () => {
      if (closed) return
      closed = true
      source.close()
    }
    source.onopen = () => {
      if (!closed) handlers.onOpen()
    }
    source.addEventListener('log', event => {
      if (closed) return
      try {
        handlers.onEntry(parseDiagnosticLogEntry(JSON.parse(event.data) as unknown))
      } catch (error) {
        close()
        handlers.onError(error instanceof Error ? error : new Error('实时日志格式无效'))
      }
    })
    source.addEventListener('reset', event => {
      if (closed) return
      try {
        const reset = requiredRecord(JSON.parse(event.data) as unknown, 'diagnostic_log_reset')
        handlers.onReset(
          numberValue(reset, 'diagnostic_log_reset', 'oldest_id'),
          numberValue(reset, 'diagnostic_log_reset', 'newest_id')
        )
      } catch (error) {
        close()
        handlers.onError(error instanceof Error ? error : new Error('日志重置事件格式无效'))
      }
    })
    source.onerror = () => {
      if (closed) return
      close()
      handlers.onError()
    }
    return close
  },

  async downloadDiagnosticLogs(query: DiagnosticLogQuery = {}): Promise<Blob> {
    const body = await request(
      `${API_ROOT}/diagnostics/logs/download${diagnosticLogQueryString(query)}`,
      {
        method: 'GET',
        headers: { Accept: 'application/x-ndjson' }
      },
      200
    )
    if (body !== undefined && typeof body !== 'string') {
      throw new ApiError('日志下载响应格式无效', 0, 'invalid_response')
    }
    return new Blob([body || ''], { type: 'application/x-ndjson' })
  },

  async getGlobalCallSettings(): Promise<GlobalCallSettings> {
    return parseGlobalCallSettings(await get(communicationContracts.getCallSettings.path))
  },

  async updateGlobalCallSettings(
    input: UpdateGlobalCallSettingsInput
  ): Promise<GlobalCallSettings> {
    const contract = communicationContracts.updateCallSettings
    return parseGlobalCallSettings(
      await writeJSON(
        contract.path,
        contract.method,
        createGlobalCallSettingsPayload(input),
        contract.successStatus
      )
    )
  },

  async updateLineSettings(input: UpdateLineSettingsInput) {
    return parseLineSettingsResponse(
      await writeJSON(
        `${API_ROOT}/settings/lines`,
        'PATCH',
        createLineSettingsPayload(input),
        200
      )
    )
  },

  async getDeviceConfiguration(lineID: string): Promise<DeviceConfiguration> {
    const contract = deviceConfigurationContract(lineID).get
    return parseDeviceConfigurationResponse(await get(contract.path))
  },

  async updateDeviceConfiguration(
    lineID: string,
    input: UpdateDeviceConfigurationInput
  ): Promise<DeviceConfiguration> {
    const contract = deviceConfigurationContract(lineID).update
    return parseDeviceConfigurationResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createDeviceConfigurationPayload(input),
        contract.successStatus
      )
    )
  },

  async getActiveCalls(): Promise<CallSession[]> {
    return parseActiveCallsResponse(await get(communicationContracts.activeCalls.path))
  },

  async startCall(
    lineKey: string,
    number: string,
    recordingEnabled?: boolean
  ): Promise<CallSession> {
    const contract = communicationContracts.startCall
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallPayload(lineKey, number, requestID(), recordingEnabled),
        contract.successStatus
      )
    )
  },

  async callAction(id, action): Promise<CallSession> {
    const contract = callActionContract(id, action)
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallActionPayload(action, requestID()),
        contract.successStatus
      )
    )
  },

  async sendDTMF(id: string, digit: string): Promise<CallSession> {
    const contract = callActionContract(id, 'dtmf')
    return parseCallResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createDTMFPayload(digit, requestID()),
        contract.successStatus
      )
    )
  },

  async exchangeCallMedia(id: string, offerSDP: string): Promise<string> {
    const contract = callMediaContract(id)
    return parseCallMediaResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createCallMediaPayload(offerSDP),
        contract.successStatus
      )
    )
  },

  async getRecordingSettings(): Promise<RecordingSettings> {
    const contract = communicationContracts.getRecordingSettings
    return parseRecordingSettingsResponse(await get(contract.path))
  },

  async listRecordings(query: ListQuery = {}): Promise<RecordingEntry[]> {
    const contract = communicationContracts.listRecordings
    return parseRecordingEntriesResponse(
      await get(`${contract.path}${queryString({ q: query.q })}`)
    )
  },

  async updateRecordingSettings(settings: RecordingSettings): Promise<RecordingSettings> {
    const contract = communicationContracts.updateRecordingSettings
    return parseRecordingSettingsResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createRecordingSettingsPayload(settings),
        contract.successStatus
      )
    )
  },

  async getTLSSettings(): Promise<TLSSettings> {
    return parseTLSSettingsResponse(await get(tlsSettingsContract.get.path))
  },

  async updateTLSSettings(input: UpdateTLSSettingsInput): Promise<TLSSettings> {
    const contract = tlsSettingsContract.update
    return parseTLSSettingsResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createTLSSettingsPayload(input),
        contract.successStatus
      )
    )
  },

  async setCallRecording(id: string, enabled: boolean): Promise<CallRecordingState> {
    const contract = callRecordingContract(id).update
    return parseCallRecordingState(
      await writeJSON(
        contract.path,
        contract.method,
        createCallRecordingPayload(enabled),
        contract.successStatus
      )
    )
  },

  async listCallRecordings(id: string): Promise<CallRecording[]> {
    const contract = callRecordingContract(id).list
    return parseCallRecordingsResponse(await get(contract.path))
  },

  async listTelegramUnits(): Promise<TelegramUnit[]> {
    return parseTelegramUnitsResponse(await get(communicationContracts.listTelegram.path))
  },

  async createTelegramUnit(input: TelegramUnitInput): Promise<TelegramUnit> {
    const contract = communicationContracts.createTelegram
    return parseTelegramUnitResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createTelegramUnitPayload(input),
        contract.successStatus
      )
    )
  },

  async updateTelegramUnit(id: string, input: TelegramUnitInput): Promise<TelegramUnit> {
    const contract = telegramUnitContract(id).update
    return parseTelegramUnitResponse(
      await writeJSON(
        contract.path,
        contract.method,
        createTelegramUnitPayload(input),
        contract.successStatus
      )
    )
  },

  async deleteTelegramUnit(id: string, revision: number): Promise<void> {
    const contract = telegramUnitContract(id).delete
    await request(
      telegramUnitDeletePath(id, revision),
      {
        method: contract.method,
        headers: { Accept: 'application/json' }
      },
      contract.successStatus
    )
  }
}

function configureFixture(gateway: ModemDeckGateway): ConfiguredModemDeckGateway {
  const session: SessionResponse = {
    authenticated: true,
    setup_required: false,
    username: 'fixture',
    language: 'auto'
  }
  return {
    ...gateway,
    interactions: FIXTURE_INTERACTIONS,
    async getSession() {
      return session
    },
    async setup() {
      return session
    },
    async login() {
      return session
    },
    async changePassword() {
      return undefined
    },
    async logout() {
      return undefined
    }
  }
}

export const gateway: ConfiguredModemDeckGateway = fixtureMode
  ? configureFixture(createFixtureGateway(fixturePreviewOptions))
  : realGateway
