import type {
  BootstrapResponse,
  CallDirection,
  CallRecord,
  CommunicationCapabilities,
  CommunicationCapabilityName,
  Contact,
  ContactPhone,
  Device,
  DeviceSIM,
  LineSummary,
  Message,
  MessageThread,
  Page,
  PageMeta,
  SystemLanguage
} from './types'
import { isLineColorPresetID } from './types'

type JsonRecord = Record<string, unknown>

function objectValue(value: unknown, path: string): JsonRecord {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${path} 必须是对象`)
  }
  return value as JsonRecord
}

function requiredString(source: JsonRecord, path: string, key: string): string {
  const value = source[key]
  if (typeof value === 'string' && value.trim()) return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  throw new Error(`${path} 缺少 ${key}`)
}

function stringValue(source: JsonRecord, key: string): string {
  const value = source[key]
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

function numberValue(source: JsonRecord, key: string, fallback = 0): number {
  const value = Number(source[key])
  return Number.isFinite(value) ? value : fallback
}

function requiredBoolean(source: JsonRecord, path: string, key: string): boolean {
  const value = source[key]
  if (typeof value === 'boolean') return value
  throw new Error(`${path}.${key} 必须是布尔值`)
}

function nullableNumber(source: JsonRecord, key: string, zeroIsUnknown = false): number | null {
  const raw = source[key]
  if (raw === undefined || raw === null || raw === '') return null
  const value = Number(raw)
  if (!Number.isFinite(value) || (zeroIsUnknown && value === 0)) return null
  return value
}

function timestamp(source: JsonRecord, path: string, key: string): string {
  const value = requiredString(source, path, key)
  if (!Number.isFinite(Date.parse(value))) throw new Error(`${path}.${key} 不是有效时间`)
  return value
}

function optionalTimestamp(source: JsonRecord, key: string): string | undefined {
  const value = stringValue(source, key)
  return value && Number.isFinite(Date.parse(value)) ? value : undefined
}

function listValue(value: unknown, key: string): unknown[] {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source[key])) throw new Error(`response.${key} 必须是数组`)
  return source[key] as unknown[]
}

export function parsePageMeta(value: unknown, path = 'response.meta'): PageMeta {
  const source = objectValue(value, path)
  const limit = Number(source.limit)
  const nextCursor = source.next_cursor
  const hasMore = source.has_more
  if (!Number.isSafeInteger(limit) || limit <= 0) {
    throw new Error(`${path}.limit 必须是正整数`)
  }
  if (typeof nextCursor !== 'string') {
    throw new Error(`${path}.next_cursor 必须是字符串`)
  }
  if (typeof hasMore !== 'boolean') {
    throw new Error(`${path}.has_more 必须是布尔值`)
  }
  if (hasMore !== Boolean(nextCursor.trim())) {
    throw new Error(`${path}.has_more 与 next_cursor 不一致`)
  }
  return {
    limit,
    next_cursor: nextCursor.trim(),
    has_more: hasMore
  }
}

function parsePage<T>(
  value: unknown,
  key: string,
  parser: (item: unknown) => T
): Page<T> {
  const source = objectValue(value, 'response')
  if (!Array.isArray(source[key])) throw new Error(`response.${key} 必须是数组`)
  return {
    items: source[key].map(parser),
    meta: parsePageMeta(source.meta)
  }
}

const COMMUNICATION_CAPABILITIES: CommunicationCapabilityName[] = [
  'modem',
  'sim',
  'voice',
  'messaging',
  'media',
  'dial',
  'answer',
  'reject',
  'hangup',
  'dtmf',
  'message'
]

function parseCommunicationCapabilities(
  value: unknown,
  path: string
): CommunicationCapabilities | undefined {
  if (value === undefined || value === null) return undefined
  const source = objectValue(value, path)
  const capabilities: CommunicationCapabilities = {}
  for (const key of COMMUNICATION_CAPABILITIES) {
    const wireKey =
      key === 'answer'
        ? 'answer_call'
        : key === 'hangup'
          ? 'hangup_call'
          : key === 'reject'
            ? 'reject_call'
            : key === 'dtmf'
              ? 'send_dtmf'
              : key === 'message'
                ? 'send_message'
                : key
    if (source[wireKey] === undefined) continue
    if (typeof source[wireKey] !== 'boolean') {
      throw new Error(`${path}.${wireKey} 必须是布尔值`)
    }
    capabilities[key] = source[wireKey]
  }
  return capabilities
}

function parseModemPorts(value: unknown, path: string) {
  if (value === undefined || value === null) return undefined
  if (!Array.isArray(value)) throw new Error(`${path} 必须是数组`)
  return value.map((entry, index) => {
    const source = objectValue(entry, `${path}[${index}]`)
    const typeCode = Number(source.type_code)
    if (!Number.isSafeInteger(typeCode) || typeCode < 0) {
      throw new Error(`${path}[${index}].type_code 必须是非负整数`)
    }
    return {
      name: requiredString(source, `${path}[${index}]`, 'name'),
      type: requiredString(source, `${path}[${index}]`, 'type'),
      type_code: typeCode
    }
  })
}

function normalizePhone(value: unknown, contactId: string, index: number): ContactPhone {
  const source = objectValue(value, `contact.phones[${index}]`)
  return {
    id: stringValue(source, 'id') || `${contactId}-phone-${index + 1}`,
    label: stringValue(source, 'label') || '电话',
    number: requiredString(source, `contact.phones[${index}]`, 'original_number'),
    normalized_number: stringValue(source, 'canonical_e164') || undefined,
    region: stringValue(source, 'region') || undefined,
    primary: typeof source.primary === 'boolean' ? source.primary : index === 0
  }
}

export function parseContact(value: unknown): Contact {
  const source = objectValue(value, 'contact')
  const id = requiredString(source, 'contact', 'id')
  const rawPhones = Array.isArray(source.phones) ? source.phones : []
  return {
    id,
    display_name: requiredString(source, 'contact', 'display_name'),
    avatar: stringValue(source, 'avatar') || undefined,
    phones: rawPhones.map((phone, index) => normalizePhone(phone, id, index)),
    favorite: requiredBoolean(source, 'contact', 'favorite'),
    notes: stringValue(source, 'notes') || undefined,
    preferred_line_id: stringValue(source, 'preferred_line_id') || undefined,
    revision: Number.isFinite(Number(source.revision)) ? Number(source.revision) : undefined,
    created_at: optionalTimestamp(source, 'created_at'),
    updated_at: optionalTimestamp(source, 'updated_at')
  }
}

export function parseContacts(value: unknown): Page<Contact> {
  return parsePage(value, 'contacts', parseContact)
}

export function parseContactResponse(value: unknown): Contact {
  const source = objectValue(value, 'response')
  return parseContact(source.contact)
}

export function parseThread(value: unknown): MessageThread {
  const source = objectValue(value, 'thread')
  const peer = requiredString(source, 'thread', 'peer')
  return {
    key: requiredString(source, 'thread', 'key'),
    line_id: requiredString(source, 'thread', 'line_id'),
    peer,
    contact_name: stringValue(source, 'contact_name') || undefined,
    last_timestamp: stringValue(source, 'last_timestamp'),
    last_content: stringValue(source, 'last_content') || undefined,
    unread_count: Math.max(0, numberValue(source, 'unread_count')),
    marked_unread: requiredBoolean(source, 'thread', 'marked_unread'),
    favorite: requiredBoolean(source, 'thread', 'favorite')
  }
}

export function parseThreads(value: unknown): Page<MessageThread> {
  return parsePage(value, 'threads', parseThread)
}

export function parseMessage(value: unknown): Message {
  const source = objectValue(value, 'message')
  const type = numberValue(source, 'type')
  if (type !== 1 && type !== 2) throw new Error(`message.type 未知：${type}`)
  const reportedDeliveryStatus = stringValue(source, 'delivery_status')
  const deliveryStatus =
    reportedDeliveryStatus || (type === 2 ? 'submitted' : '')
  if (
    deliveryStatus !== '' &&
    deliveryStatus !== 'submitted' &&
    deliveryStatus !== 'delivered' &&
    deliveryStatus !== 'failed'
  ) {
    throw new Error(`message.delivery_status 未知：${deliveryStatus}`)
  }
  return {
    id: requiredString(source, 'message', 'id'),
    line_id: requiredString(source, 'message', 'line_id'),
    peer: requiredString(source, 'message', 'peer'),
    direction: type === 1 ? 'incoming' : 'outgoing',
    content: stringValue(source, 'content'),
    timestamp: stringValue(source, 'timestamp'),
    type,
    status: numberValue(source, 'status'),
    delivery_status: deliveryStatus
  }
}

export function parseMessages(value: unknown): Page<Message> {
  return parsePage(value, 'messages', parseMessage)
}

function directionValue(source: JsonRecord, path: string): CallDirection {
  const value = requiredString(source, path, 'direction')
  if (value !== 'incoming' && value !== 'outgoing') throw new Error(`${path}.direction 未知：${value}`)
  return value
}

export function parseCallRecord(value: unknown): CallRecord {
  const source = objectValue(value, 'call')
  return {
    id: requiredString(source, 'call', 'id'),
    line_id: requiredString(source, 'call', 'line_id'),
    direction: directionValue(source, 'call'),
    remote_number: requiredString(source, 'call', 'remote_number'),
    display_name: stringValue(source, 'contact_name') || undefined,
    contact_id: stringValue(source, 'contact_id') || undefined,
    started_at: timestamp(source, 'call', 'started_at'),
    ended_at: optionalTimestamp(source, 'ended_at'),
    duration_seconds: Math.max(0, numberValue(source, 'duration_seconds')),
    missed: source.missed === true,
    read: source.read === true,
    favorite: source.favorite === true,
    failure_reason: stringValue(source, 'failure_code') || undefined
  }
}

export function parseCalls(value: unknown): Page<CallRecord> {
  return parsePage(value, 'calls', parseCallRecord)
}

function parseSIM(value: unknown): DeviceSIM {
  const source = objectValue(value, 'device.sim')
  return {
    iccid: stringValue(source, 'iccid'),
    imsi: stringValue(source, 'imsi'),
    phone_number: stringValue(source, 'phone_number'),
    operator: stringValue(source, 'operator'),
    home_operator_code: stringValue(source, 'home_operator_code'),
    home_operator_name: stringValue(source, 'home_operator_name'),
    home_country_iso: stringValue(source, 'home_country_iso'),
    serving_operator_code: stringValue(source, 'serving_operator_code'),
    serving_operator_name: stringValue(source, 'serving_operator_name'),
    serving_country_iso: stringValue(source, 'serving_country_iso'),
    registration_state_known: source.registration_state_known === true,
    registration_state_code: numberValue(source, 'registration_state_code'),
    registration_state: stringValue(source, 'registration_state'),
    roaming: source.roaming === true,
    current_imei: stringValue(source, 'current_imei'),
    reg_status: numberValue(source, 'reg_status'),
    reg_status_text: stringValue(source, 'reg_status_text'),
    lac: stringValue(source, 'lac'),
    cell_id: stringValue(source, 'cell_id'),
    apn: stringValue(source, 'apn'),
    ims_status: numberValue(source, 'ims_status'),
    last_seen: stringValue(source, 'last_seen')
  }
}

export function parseDevice(value: unknown): Device {
  const source = objectValue(value, 'device')
  return {
    imei: requiredString(source, 'device', 'imei'),
    endpoint_id: stringValue(source, 'endpoint_id'),
    name: stringValue(source, 'name'),
    model: stringValue(source, 'model'),
    firmware: stringValue(source, 'firmware'),
    port: stringValue(source, 'port'),
    public_ip: stringValue(source, 'public_ip'),
    private_ip: stringValue(source, 'private_ip'),
    public_ipv6: stringValue(source, 'public_ipv6'),
    private_ipv6: stringValue(source, 'private_ipv6'),
    state: stringValue(source, 'state') || undefined,
    current_iccid: stringValue(source, 'current_iccid'),
    sim_inserted: source.sim_inserted === true,
    signal_quality: nullableNumber(source, 'signal_quality'),
    signal_dbm: nullableNumber(source, 'signal_dbm', true),
    signal_rsrq: nullableNumber(source, 'signal_rsrq', true),
    signal_rsrp: nullableNumber(source, 'signal_rsrp', true),
    last_seen: optionalTimestamp(source, 'last_seen'),
    present: source.present === true,
    sim: source.sim ? parseSIM(source.sim) : undefined,
    capabilities: parseCommunicationCapabilities(source.capabilities, 'device.capabilities')
  }
}

export function parseDevices(value: unknown): Device[] {
  return listValue(value, 'devices').map(parseDevice)
}

export function parseDeviceResponse(value: unknown): Device {
  const source = objectValue(value, 'device_response')
  return parseDevice(source.device)
}

export function parseLine(value: unknown): LineSummary {
  const source = objectValue(value, 'line')
  const rawAccessTechnologies = nullableNumber(source, 'access_technologies')
  const rawFailureReasonCode = nullableNumber(source, 'failure_reason_code')
  const rawSignalSNR = nullableNumber(source, 'signal_snr', true)
  const rawLineColor = stringValue(source, 'line_color')
  const line: LineSummary = {
    id: requiredString(source, 'line', 'id'),
    iccid: stringValue(source, 'iccid'),
    imsi: stringValue(source, 'imsi'),
    phone_number: stringValue(source, 'phone_number'),
    operator: stringValue(source, 'operator'),
    home_operator_code: stringValue(source, 'home_operator_code'),
    home_operator_name: stringValue(source, 'home_operator_name'),
    home_country_iso: stringValue(source, 'home_country_iso'),
    serving_operator_code: stringValue(source, 'serving_operator_code'),
    serving_operator_name: stringValue(source, 'serving_operator_name'),
    serving_country_iso: stringValue(source, 'serving_country_iso'),
    registration_state_known: source.registration_state_known === true,
    registration_state_code: numberValue(source, 'registration_state_code'),
    registration_state: stringValue(source, 'registration_state'),
    roaming: source.roaming === true,
    emergency_only: source.emergency_only === true,
    device_imei: stringValue(source, 'device_imei'),
    device_name: stringValue(source, 'device_name'),
    line_label: stringValue(source, 'line_label'),
    line_color: isLineColorPresetID(rawLineColor) ? rawLineColor : '',
    model: stringValue(source, 'model') || undefined,
    firmware: stringValue(source, 'firmware') || undefined,
    hardware_revision: stringValue(source, 'hardware_revision') || undefined,
    primary_port: stringValue(source, 'primary_port') || undefined,
    ports: parseModemPorts(source.ports, 'line.ports'),
    access_technologies:
      rawAccessTechnologies != null && rawAccessTechnologies > 0
        ? rawAccessTechnologies
        : undefined,
    state: stringValue(source, 'state') || undefined,
    failure_reason: stringValue(source, 'failure_reason') || undefined,
    failure_reason_code:
      rawFailureReasonCode != null && rawFailureReasonCode > 0
        ? rawFailureReasonCode
        : undefined,
    radio_desired_enabled: requiredBoolean(source, 'line', 'radio_desired_enabled'),
    radio_desired_enabled_known: requiredBoolean(
      source,
      'line',
      'radio_desired_enabled_known'
    ),
    signal_quality: nullableNumber(source, 'signal_quality') ?? undefined,
    signal_snr: rawSignalSNR ?? undefined,
    capabilities: parseCommunicationCapabilities(source.capabilities, 'line.capabilities')
  }
  return line
}

export function parseLineResponse(value: unknown): LineSummary {
  const source = objectValue(value, 'line_response')
  return parseLine(source.line ?? source)
}

export function parseBootstrap(value: unknown): BootstrapResponse {
  const source = objectValue(value, 'bootstrap')
  const capabilities = objectValue(source.capabilities, 'bootstrap.capabilities')
  const booleanKeys = [
    'agent_connected',
    'dial',
    'message',
    'webrtc_audio',
    'device_control',
    'volte_control',
    'vowifi_control'
  ] as const
  for (const key of booleanKeys) {
    if (typeof capabilities[key] !== 'boolean') {
      throw new Error(`bootstrap.capabilities.${key} 必须是布尔值`)
    }
  }
  const reasons =
    capabilities.unavailable_reasons && typeof capabilities.unavailable_reasons === 'object'
      ? (capabilities.unavailable_reasons as JsonRecord)
      : {}
  const lines = Array.isArray(source.lines) ? source.lines.map(parseLine) : []
  const lineCatalog = Array.isArray(source.line_catalog)
    ? source.line_catalog.map(parseLine)
    : lines
  const lineSettings = objectValue(source.line_settings, 'bootstrap.line_settings')
  const lineSettingsRevision = numberValue(lineSettings, 'revision')
  if (!Number.isSafeInteger(lineSettingsRevision) || lineSettingsRevision < 1) {
    throw new Error('bootstrap.line_settings.revision 必须是正整数')
  }
  const systemSettings = objectValue(source.system_settings, 'bootstrap.system_settings')
  const language = stringValue(systemSettings, 'language')
  if (
    language !== 'auto' &&
    language !== 'zh-CN' &&
    language !== 'zh-TW' &&
    language !== 'en-US' &&
    language !== 'ja-JP' &&
    language !== 'vi-VN' &&
    language !== 'es-ES' &&
    language !== 'de-DE' &&
    language !== 'fr-FR' &&
    language !== 'pt-BR'
  ) {
    throw new Error('bootstrap.system_settings.language is not supported')
  }
  const systemSettingsRevision = numberValue(systemSettings, 'revision')
  if (!Number.isSafeInteger(systemSettingsRevision) || systemSettingsRevision < 1) {
    throw new Error('bootstrap.system_settings.revision must be a positive integer')
  }
  return {
    capabilities: {
      agent_connected: capabilities.agent_connected as boolean,
      dial: capabilities.dial as boolean,
      message: capabilities.message as boolean,
      webrtc_audio: capabilities.webrtc_audio as boolean,
      device_control: capabilities.device_control as boolean,
      volte_control: capabilities.volte_control as boolean,
      vowifi_control: capabilities.vowifi_control as boolean,
      unavailable_reasons: {
        dial: stringValue(reasons, 'dial') || undefined,
        message: stringValue(reasons, 'message') || undefined
      }
    },
    lines,
    line_catalog: lineCatalog,
    line_settings: {
      default_line_id: stringValue(lineSettings, 'default_line_id'),
      revision: lineSettingsRevision
    },
    system_settings: {
      language: language as SystemLanguage,
      revision: systemSettingsRevision
    }
  }
}
