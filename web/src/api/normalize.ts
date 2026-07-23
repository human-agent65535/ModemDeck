import type {
  BootstrapResponse,
  CallDirection,
  CallRecord,
  Contact,
  ContactPhone,
  Device,
  DeviceSIM,
  LineSummary,
  Message,
  MessageThread
} from './types'

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

function normalizePhone(value: unknown, contactId: string, index: number): ContactPhone {
  const source = objectValue(value, `contact.phones[${index}]`)
  return {
    id: stringValue(source, 'id') || `${contactId}-phone-${index + 1}`,
    label: stringValue(source, 'label') || '电话',
    number: requiredString(source, `contact.phones[${index}]`, 'original_number'),
    normalized_number: stringValue(source, 'canonical_e164') || undefined,
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
    phones: rawPhones.map((phone, index) => normalizePhone(phone, id, index)),
    notes: stringValue(source, 'notes') || undefined,
    revision: Number.isFinite(Number(source.revision)) ? Number(source.revision) : undefined,
    created_at: optionalTimestamp(source, 'created_at'),
    updated_at: optionalTimestamp(source, 'updated_at')
  }
}

export function parseContacts(value: unknown): Contact[] {
  return listValue(value, 'contacts').map(parseContact)
}

export function threadKey(iccid: string, peer: string): string {
  return `${iccid}|${peer}`
}

export function parseThread(value: unknown): MessageThread {
  const source = objectValue(value, 'thread')
  const iccid = requiredString(source, 'thread', 'iccid')
  const peer = requiredString(source, 'thread', 'peer')
  return {
    key: threadKey(iccid, peer),
    imsi: stringValue(source, 'imsi'),
    iccid,
    peer,
    contact_name: stringValue(source, 'contact_name') || undefined,
    last_timestamp: stringValue(source, 'last_timestamp'),
    last_content: stringValue(source, 'last_content') || undefined,
    unread_count: Math.max(0, numberValue(source, 'unread_count'))
  }
}

export function parseThreads(value: unknown): MessageThread[] {
  return listValue(value, 'threads').map(parseThread)
}

export function parseMessage(value: unknown): Message {
  const source = objectValue(value, 'message')
  const type = numberValue(source, 'type')
  if (type !== 1 && type !== 2) throw new Error(`message.type 未知：${type}`)
  return {
    id: requiredString(source, 'message', 'id'),
    imsi: stringValue(source, 'imsi'),
    iccid: requiredString(source, 'message', 'iccid'),
    peer: requiredString(source, 'message', 'peer'),
    direction: type === 1 ? 'incoming' : 'outgoing',
    content: stringValue(source, 'content'),
    timestamp: stringValue(source, 'timestamp'),
    type,
    status: numberValue(source, 'status')
  }
}

export function parseMessages(value: unknown): Message[] {
  return listValue(value, 'messages').map(parseMessage)
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
    device_id: requiredString(source, 'call', 'device_id'),
    direction: directionValue(source, 'call'),
    remote_number: requiredString(source, 'call', 'remote_number'),
    display_name: stringValue(source, 'contact_name') || undefined,
    contact_id: stringValue(source, 'contact_id') || undefined,
    started_at: timestamp(source, 'call', 'started_at'),
    ended_at: optionalTimestamp(source, 'ended_at'),
    duration_seconds: Math.max(0, numberValue(source, 'duration_seconds')),
    missed: source.missed === true,
    failure_reason: stringValue(source, 'failure_code') || undefined
  }
}

export function parseCalls(value: unknown): CallRecord[] {
  return listValue(value, 'calls').map(parseCallRecord)
}

function parseSIM(value: unknown): DeviceSIM {
  const source = objectValue(value, 'device.sim')
  return {
    iccid: stringValue(source, 'iccid'),
    imsi: stringValue(source, 'imsi'),
    phone_number: stringValue(source, 'phone_number'),
    operator: stringValue(source, 'operator'),
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
  const signal = Number(source.signal_dbm)
  return {
    imei: requiredString(source, 'device', 'imei'),
    alias: stringValue(source, 'alias'),
    model: stringValue(source, 'model'),
    firmware: stringValue(source, 'firmware'),
    current_iccid: stringValue(source, 'current_iccid'),
    sim_inserted: source.sim_inserted === true,
    signal_dbm: Number.isFinite(signal) ? signal : null,
    sim: source.sim ? parseSIM(source.sim) : undefined
  }
}

export function parseDevices(value: unknown): Device[] {
  return listValue(value, 'devices').map(parseDevice)
}

function parseLine(value: unknown): LineSummary {
  const source = objectValue(value, 'line')
  const line: LineSummary = {
    iccid: stringValue(source, 'iccid'),
    imsi: stringValue(source, 'imsi'),
    phone_number: stringValue(source, 'phone_number'),
    operator: stringValue(source, 'operator'),
    device_imei: stringValue(source, 'device_imei'),
    device_alias: stringValue(source, 'device_alias')
  }
  if (!line.iccid && !line.imsi && !line.device_imei) throw new Error('line 缺少稳定标识')
  return line
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
    lines
  }
}
