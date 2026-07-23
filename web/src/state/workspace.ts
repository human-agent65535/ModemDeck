import { reactive, shallowReactive } from 'vue'
import { gateway } from '../api/client'
import type {
  BootstrapResponse,
  CallFilter,
  CallRecord,
  Contact,
  ContactInput,
  Device,
  LineSummary,
  Message,
  MessageThread,
  Resource,
  SendMessageInput
} from '../api/types'

function resource<T>(data: T): Resource<T> {
  return shallowReactive({ status: 'idle', data, error: '' })
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
    target.status = 'error'
    target.error = errorText(error)
    return null
  }
}

export const bootstrapResource = resource<BootstrapResponse | null>(null)
export const contactsResource = resource<Contact[]>([])
export const threadsResource = resource<MessageThread[]>([])
export const callsResource = resource<CallRecord[]>([])
export const devicesResource = resource<Device[]>([])
export const messageResources = reactive<Record<string, Resource<Message[]>>>({})
export const interactiveMode = gateway.interactive

export function capabilityReason(capability: 'dial' | 'message'): string {
  const bootstrap = bootstrapResource.data
  if (!bootstrap) return '通信能力尚未载入'
  const available = bootstrap.capabilities[capability]
  if (!available) {
    return bootstrap.capabilities.unavailable_reasons?.[capability] || 'Host agent 未提供此能力'
  }
  if (!gateway.interactive) return '当前版本为只读，控制接口尚未开放'
  return ''
}

export function lineKey(line: LineSummary): string {
  return line.iccid || line.imsi || line.device_imei
}

export function lineLabel(line: LineSummary): string {
  return line.device_alias || line.phone_number || line.operator || lineKey(line)
}

export function lineName(key: string): string {
  const line = bootstrapResource.data?.lines.find(item => lineKey(item) === key || item.iccid === key)
  return line ? lineLabel(line) : key
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
  return load(threadsResource, () => gateway.listThreads())
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

export function messagesFor(threadKey: string): Resource<Message[]> {
  if (!messageResources[threadKey]) messageResources[threadKey] = resource<Message[]>([])
  return messageResources[threadKey]
}

export function loadMessages(thread: MessageThread, force = false): Promise<Message[] | null> {
  const target = messagesFor(thread.key)
  if (!force && target.status === 'ready') return Promise.resolve(target.data)
  return load(target, () => gateway.listMessages({ iccid: thread.iccid, peer: thread.peer }))
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
  const index = contactsResource.data.findIndex(contact => contact.id === saved.id)
  if (index >= 0) contactsResource.data.splice(index, 1, saved)
  else contactsResource.data.push(saved)
  contactsResource.data.sort((a, b) => a.display_name.localeCompare(b.display_name))
  contactsResource.status = 'ready'
  return saved
}

export async function deleteContact(contact: Contact): Promise<void> {
  if (!gateway.deleteContact) throw new Error('当前版本不支持联系人写入')
  await gateway.deleteContact(contact.id, contact.revision)
  const index = contactsResource.data.findIndex(item => item.id === contact.id)
  if (index >= 0) contactsResource.data.splice(index, 1)
}

export async function sendMessage(input: SendMessageInput): Promise<Message> {
  if (!gateway.sendMessage) throw new Error('当前版本不支持发送消息')
  const sent = await gateway.sendMessage(input)
  const key = input.thread_key || `${input.iccid}|${input.to}`
  const target = messagesFor(key)
  target.data.push(sent)
  target.status = 'ready'
  await loadThreads(true)
  return sent
}

export async function ensureSearchData(): Promise<void> {
  await Promise.all([loadContacts(), loadThreads(), loadCalls()])
}
