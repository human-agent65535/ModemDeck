import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  BootstrapResponse,
  CallFilter,
  CallRecord,
  CommunicationCapabilityName,
  Contact,
  ContactInput,
  CreateDeviceInput,
  Device,
  LineSummary,
  Message,
  MessageReadInput,
  MessageThread,
  Resource,
  RenameDeviceInput,
  SendMessageInput,
  TelegramUnit,
  TelegramUnitInput
} from '../api/types'
import { ApiError } from '../api/types'

function resource<T>(data: T): Resource<T> {
  return reactive({ status: 'idle', data, error: '' }) as Resource<T>
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : '请求失败'
}

async function load<T>(target: Resource<T>, loader: () => Promise<T>): Promise<T | null> {
  target.status = 'loading'
  target.error = ''
  try {
    const data = await loader()
    target.data = data
    target.status = 'ready'
    return data
  } catch (error) {
    target.status = error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
    target.error = errorText(error)
    return null
  }
}

export const bootstrapResource = resource<BootstrapResponse | null>(null)
export const contactsResource = resource<Contact[]>([])
export const threadsResource = resource<MessageThread[]>([])
export const callsResource = resource<CallRecord[]>([])
export const devicesResource = resource<Device[]>([])
export const telegramResource = resource<TelegramUnit[]>([])
export const messageResources = reactive<Record<string, Resource<Message[]>>>({})
export const threadReadErrors = reactive<Record<string, string>>({})
export const contactEditingAvailable = gateway.interactions.contacts

let threadsLoad: Promise<MessageThread[] | null> | undefined
const messageLoads = new Map<string, Promise<Message[] | null>>()

export function createMessageReadCoordinator(
  request: (input: MessageReadInput) => Promise<void>
): (input: MessageReadInput) => Promise<void> {
  const requests = new Map<string, Promise<void>>()
  return input => {
    const key = `${input.iccid}\u0000${input.peer}`
    const pending = requests.get(key)
    if (pending) return pending
    const operation = request(input).finally(() => {
      if (requests.get(key) === operation) requests.delete(key)
    })
    requests.set(key, operation)
    return operation
  }
}

const requestThreadRead = createMessageReadCoordinator(input => gateway.markThreadRead(input))

export function capabilityReason(capability: 'dial' | 'message'): string {
  const bootstrap = bootstrapResource.data
  if (!bootstrap) return '通信能力尚未载入'
  const available = bootstrap.capabilities[capability]
  if (!available) {
    return bootstrap.capabilities.unavailable_reasons?.[capability] || 'Host agent 未提供此能力'
  }
  if (!gateway.interactions[capability]) return '当前版本尚未开放此控制接口'
  return ''
}

export function lineKey(line: LineSummary): string {
  return line.id || line.iccid || line.imsi || line.device_imei
}

export function lineLabel(line: LineSummary): string {
  return line.device_alias || line.model || line.phone_number || line.operator || lineKey(line)
}

export function lineName(key: string): string {
  const line = lineForKey(key)
  return line ? lineLabel(line) : key
}

export function lineForKey(key: string): LineSummary | undefined {
  return bootstrapResource.data?.lines.find(
    item => lineKey(item) === key || item.iccid === key || item.device_imei === key
  )
}

export function resolveLine(
  capability: CommunicationCapabilityName,
  options: {
    contextKey?: string
    preferredDeviceIMEI?: string
    number?: string
  } = {}
): LineSummary | undefined {
  const lines = bootstrapResource.data?.lines || []
  const supported = (line: LineSummary | undefined) =>
    line && lineSupports(line, capability) !== false ? line : undefined
  const byKey = (key?: string) =>
    supported(
      key
        ? lines.find(
            line =>
              lineKey(line) === key ||
              line.id === key ||
              line.iccid === key ||
              line.device_imei === key
          )
        : undefined
    )

  const contextLine = byKey(options.contextKey)
  if (contextLine) return contextLine

  const contact =
    options.preferredDeviceIMEI || !options.number
      ? undefined
      : contactForNumber(options.number)
  const preferredLine = byKey(options.preferredDeviceIMEI || contact?.preferred_device_imei)
  if (preferredLine) return preferredLine

  return byKey(bootstrapResource.data?.line_settings.default_device_imei)
}

export function lineSupports(
  line: LineSummary | undefined,
  capability: CommunicationCapabilityName
): boolean | null {
  const value = line?.capabilities?.[capability]
  return typeof value === 'boolean' ? value : null
}

export function deviceName(id: string): string {
  const device = devicesResource.data.find(item => item.imei === id)
  return device?.alias || device?.model || id
}

export function contactForNumber(number: string): Contact | undefined {
  const compact = number.replace(/\D/g, '')
  return contactsResource.data.find(contact =>
    contact.phones.some(phone => phone.number.replace(/\D/g, '') === compact)
  )
}

export function loadBootstrap(force = false): Promise<BootstrapResponse | null> {
  if (!force && bootstrapResource.status === 'ready') return Promise.resolve(bootstrapResource.data)
  return load(bootstrapResource, () => gateway.getBootstrap())
}

export function loadContacts(force = false): Promise<Contact[] | null> {
  if (!force && contactsResource.status === 'ready') return Promise.resolve(contactsResource.data)
  return load(contactsResource, () => gateway.listContacts())
}

export function loadThreads(force = false): Promise<MessageThread[] | null> {
  if (!force && threadsResource.status === 'ready') return Promise.resolve(threadsResource.data)
  if (threadsLoad) return threadsLoad
  threadsLoad = load(threadsResource, () => gateway.listThreads()).finally(() => {
    threadsLoad = undefined
  })
  return threadsLoad
}

export function loadCalls(force = false, filter: CallFilter = 'all'): Promise<CallRecord[] | null> {
  if (!force && filter === 'all' && callsResource.status === 'ready') {
    return Promise.resolve(callsResource.data)
  }
  return load(callsResource, () => gateway.listCalls(filter))
}

export function loadDevices(force = false): Promise<Device[] | null> {
  if (!force && devicesResource.status === 'ready') return Promise.resolve(devicesResource.data)
  return load(devicesResource, () => gateway.listDevices())
}

export async function createDevice(input: CreateDeviceInput): Promise<Device> {
  const saved = await gateway.createDevice(input)
  devicesResource.data = devicesResource.data
    .filter(device => device.imei !== saved.imei)
    .concat(saved)
    .sort((a, b) => (a.alias || a.model || a.imei).localeCompare(b.alias || b.model || b.imei))
  devicesResource.status = 'ready'
  devicesResource.error = ''
  return saved
}

export async function renameDevice(imei: string, input: RenameDeviceInput): Promise<Device> {
  const saved = await gateway.renameDevice(imei, input)
  devicesResource.data = devicesResource.data
    .map(device => (device.imei === saved.imei ? saved : device))
    .sort((a, b) => (a.alias || a.model || a.imei).localeCompare(b.alias || b.model || b.imei))
  devicesResource.status = 'ready'
  devicesResource.error = ''
  const line = bootstrapResource.data?.lines.find(item => item.device_imei === saved.imei)
  if (line) line.device_alias = saved.alias
  return saved
}

export function loadTelegramUnits(force = false): Promise<TelegramUnit[] | null> {
  if (!force && telegramResource.status === 'ready') {
    return Promise.resolve(telegramResource.data)
  }
  return load(telegramResource, () => gateway.listTelegramUnits())
}

export function messagesFor(threadKey: string): Resource<Message[]> {
  if (!messageResources[threadKey]) messageResources[threadKey] = resource<Message[]>([])
  return messageResources[threadKey]
}

export async function loadMessages(
  thread: MessageThread,
  force = false
): Promise<Message[] | null> {
  const target = messagesFor(thread.key)
  if (!force && target.status === 'ready') return target.data
  const pending = messageLoads.get(thread.key)
  if (pending) return pending
  const operation = load(target, () =>
    gateway.listMessages({ iccid: thread.iccid, peer: thread.peer })
  ).finally(() => {
    if (messageLoads.get(thread.key) === operation) messageLoads.delete(thread.key)
  })
  messageLoads.set(thread.key, operation)
  return operation
}

function messageReadError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'authentication_required' || error.code === 'csrf_failed') {
      return '无法标记已读：登录校验已失效，请刷新页面后重试'
    }
    if (error.code === 'message_thread_not_found') {
      return '无法标记已读：这段会话已不存在，请刷新消息列表'
    }
    if (error.code === 'message_thread_identity_invalid') {
      return '无法标记已读：线路身份不明确，请刷新消息列表'
    }
    if (error.code === 'internal_error') {
      return '无法标记已读：服务端未能保存状态，请稍后重试'
    }
  }
  return `无法标记已读：${errorText(error)}`
}

export async function markThreadRead(thread: MessageThread): Promise<boolean> {
  if (thread.unread_count <= 0) return true
  const key = thread.key
  threadReadErrors[key] = ''
  try {
    await requestThreadRead({ iccid: thread.iccid, peer: thread.peer })
  } catch (error) {
    threadReadErrors[key] = messageReadError(error)
    return false
  }

  const current = threadsResource.data.find(
    item => item.key === key && item.iccid === thread.iccid && item.peer === thread.peer
  )
  if (current) current.unread_count = 0
  delete threadReadErrors[key]
  return true
}

export async function updateDefaultLine(deviceIMEI: string): Promise<void> {
  const bootstrap = bootstrapResource.data
  if (!bootstrap) throw new Error('线路设置尚未载入')
  const settings = await gateway.updateLineSettings({
    default_device_imei: deviceIMEI,
    expected_revision: bootstrap.line_settings.revision
  })
  bootstrap.line_settings = settings
}

export async function saveContact(input: ContactInput, id?: string): Promise<Contact> {
  let saved: Contact
  if (id) {
    if (!gateway.updateContact) throw new Error('当前版本不支持联系人写入')
    saved = await gateway.updateContact(id, input)
  } else {
    if (!gateway.createContact) throw new Error('当前版本不支持联系人写入')
    saved = await gateway.createContact(input)
  }
  contactsResource.data = contactsResource.data
    .filter(contact => contact.id !== saved.id)
    .concat(saved)
    .sort((a, b) => a.display_name.localeCompare(b.display_name))
  contactsResource.status = 'ready'
  return saved
}

export async function deleteContact(contact: Contact): Promise<void> {
  if (!gateway.deleteContact) throw new Error('当前版本不支持联系人写入')
  await gateway.deleteContact(contact.id, contact.revision)
  contactsResource.data = contactsResource.data.filter(item => item.id !== contact.id)
}

export async function sendMessage(input: SendMessageInput): Promise<Message> {
  const sent = await gateway.sendMessage(input)
  const key = `${sent.iccid}|${sent.peer}`
  const target = messagesFor(key)
  target.data = [...target.data, sent]
  target.status = 'ready'

  const existing = threadsResource.data.find(thread => thread.key === key)
  const updated: MessageThread = {
    key,
    imsi: sent.imsi,
    iccid: sent.iccid,
    line_id: sent.line_id || input.line_id || existing?.line_id,
    peer: sent.peer,
    contact_name: existing?.contact_name || contactForNumber(sent.peer)?.display_name,
    last_timestamp: sent.timestamp,
    last_content: sent.content,
    unread_count: existing?.unread_count || 0
  }
  threadsResource.data = threadsResource.data
    .filter(thread => thread.key !== key)
    .concat(updated)
    .sort((a, b) => Date.parse(b.last_timestamp) - Date.parse(a.last_timestamp))
  threadsResource.status = 'ready'
  return sent
}

export async function saveTelegramUnit(
  input: TelegramUnitInput,
  id?: string
): Promise<TelegramUnit> {
  const saved = id
    ? await gateway.updateTelegramUnit(id, input)
    : await gateway.createTelegramUnit(input)
  telegramResource.data = telegramResource.data
    .filter(unit => unit.id !== saved.id)
    .concat(saved)
    .sort((a, b) => a.display_name.localeCompare(b.display_name))
  telegramResource.status = 'ready'
  telegramResource.error = ''
  return saved
}

export async function deleteTelegramUnit(unit: TelegramUnit): Promise<void> {
  await gateway.deleteTelegramUnit(unit.id, unit.revision)
  telegramResource.data = telegramResource.data.filter(item => item.id !== unit.id)
}

export async function ensureSearchData(): Promise<void> {
  await Promise.all([loadContacts(), loadThreads(), loadCalls()])
}
