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
  IncomingMessageEvent,
  LineSummary,
  Message,
  MessageReadInput,
  MessageThread,
  Resource,
  RenameDeviceInput,
  SendMessageInput,
  TelegramUnit,
  TelegramUnitInput,
  UpdateLineLabelInput
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import { playOutgoingMessageSound } from './browserSounds'
import {
  normalizedPhoneIdentity,
  phoneIdentitiesMatch
} from '../utils/lineIdentity'

function resource<T>(data: T): Resource<T> {
  return reactive({ status: 'idle', data, error: '' }) as Resource<T>
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
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
export const recentIncomingMessageIDs = reactive<Record<string, boolean>>({})
export const recentIncomingThreadKeys = reactive<Record<string, boolean>>({})
export const contactEditingAvailable = gateway.interactions.contacts

let threadsLoad: Promise<MessageThread[] | null> | undefined
let threadsRefresh: Promise<MessageThread[] | null> | undefined
let bootstrapRefresh: Promise<BootstrapResponse | null> | undefined
let devicesRefresh: Promise<Device[] | null> | undefined
let callsRefreshRequest: Promise<CallRecord[] | null> | undefined
let missedCallsReadRequest: Promise<void> | undefined
const messageLoads = new Map<string, Promise<Message[] | null>>()
const messageRefreshes = new Map<string, Promise<Message[] | null>>()
const arrivalTimers = new Map<string, ReturnType<typeof setTimeout>>()

export function createMessageReadCoordinator(
  request: (input: MessageReadInput) => Promise<void>
): (input: MessageReadInput) => Promise<void> {
  const requests = new Map<string, Promise<void>>()
  return input => {
    const key = `${input.line_id.trim()}\u0000${input.peer.trim()}`
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
  if (!bootstrap) return translate('runtime.capabilityNotLoaded')
  const available = bootstrap.capabilities[capability]
  if (!available) {
    return (
      bootstrap.capabilities.unavailable_reasons?.[capability] ||
      translate('runtime.hostCapabilityMissing')
    )
  }
  if (!gateway.interactions[capability]) return translate('runtime.controlUnavailable')
  return ''
}

export function lineKey(line: LineSummary): string {
  return line.id.trim()
}

export function lineLabel(line: LineSummary): string {
  const explicit = line.line_label.trim()
  if (explicit) return explicit

  const moduleName = line.device_name.trim() || line.model?.trim()
  if (moduleName) return moduleName

  const identifier = (line.iccid || line.id || line.device_imei).trim()
  return identifier
    ? translate('runtime.lineSuffix', { suffix: identifier.slice(-4) })
    : translate('runtime.unnamedLine')
}

export function lineName(key: string): string {
  const line = lineForKey(key)
  return line ? lineLabel(line) : key
}

export function lineForKey(key: string): LineSummary | undefined {
  const lines = bootstrapResource.data?.lines || []
  const normalizedKey = key.trim()
  if (!normalizedKey) return undefined
  return lines.find(line => lineKey(line) === normalizedKey)
}

export function resolveLine(
  capability: CommunicationCapabilityName,
  options: {
    contextKey?: string
    preferredLineID?: string
    number?: string
  } = {}
): LineSummary | undefined {
  const lines = bootstrapResource.data?.lines || []
  const supported = (line: LineSummary | undefined) =>
    line && lineSupports(line, capability) !== false ? line : undefined
  const byID = (id?: string) =>
    supported(id ? lines.find(line => lineKey(line) === id.trim()) : undefined)

  const contextLine = byID(options.contextKey)
  if (contextLine) return contextLine

  const contact =
    options.preferredLineID || !options.number
      ? undefined
      : contactForNumber(options.number)
  const preferredLine = byID(options.preferredLineID || contact?.preferred_line_id)
  if (preferredLine) return preferredLine

  return byID(bootstrapResource.data?.line_settings.default_line_id)
}

export function lineSupports(
  line: LineSummary | undefined,
  capability: CommunicationCapabilityName
): boolean | null {
  const value = line?.capabilities?.[capability]
  return typeof value === 'boolean' ? value : null
}

export function lineHasCallControl(line: LineSummary | undefined): boolean {
  return (
    lineSupports(line, 'dial') === true ||
    line?.capabilities?.answer === true ||
    line?.capabilities?.reject === true ||
    line?.capabilities?.hangup === true
  )
}

export function displayModuleLines(lines: LineSummary[], devices: Device[]): LineSummary[] {
  const result = lines.slice()
  const boundIMEIs = new Set(
    lines.map(line => line.device_imei.trim()).filter(identifier => Boolean(identifier))
  )
  for (const device of devices) {
    const imei = device.imei.trim()
    if (!imei || boundIMEIs.has(imei)) continue
    const sim = device.sim
    result.push({
      id: `module_${imei}`,
      iccid: device.sim_inserted ? device.current_iccid : '',
      imsi: device.sim_inserted ? sim?.imsi || '' : '',
      phone_number: device.sim_inserted ? sim?.phone_number || '' : '',
      operator: device.sim_inserted ? sim?.operator || '' : '',
      home_operator_code: device.sim_inserted ? sim?.home_operator_code || '' : '',
      home_operator_name: device.sim_inserted ? sim?.home_operator_name || '' : '',
      serving_operator_code: device.sim_inserted ? sim?.serving_operator_code || '' : '',
      serving_operator_name: device.sim_inserted ? sim?.serving_operator_name || '' : '',
      registration_state_known:
        device.sim_inserted && sim?.registration_state_known === true,
      registration_state_code: device.sim_inserted ? sim?.registration_state_code || 0 : 0,
      registration_state: device.sim_inserted ? sim?.registration_state || '' : '',
      roaming: device.sim_inserted && sim?.roaming === true,
      emergency_only: false,
      device_imei: imei,
      device_name: device.name,
      line_label: '',
      line_color: '',
      model: device.model || undefined,
      firmware: device.firmware || undefined,
      primary_port: device.port || undefined,
      state: !device.present
        ? 'disconnected'
        : device.sim_inserted
          ? device.state || 'unknown'
          : 'sim-missing',
      radio_desired_enabled: false,
      radio_desired_enabled_known: false,
      signal_quality: device.present ? device.signal_quality ?? undefined : undefined,
      capabilities: { modem: device.present },
      module_only: true
    })
  }
  return result
}

export function presentModuleLines(lines: LineSummary[], devices: Device[]): LineSummary[] {
  const devicesByIMEI = new Map(
    devices
      .map(device => [device.imei.trim(), device] as const)
      .filter(([imei]) => Boolean(imei))
  )
  const seen = new Set<string>()

  return displayModuleLines(lines, devices).filter(line => {
    const imei = line.device_imei.trim()
    const device = imei ? devicesByIMEI.get(imei) : undefined
    if (device?.present === false) return false
    if (line.module_only && device?.present !== true) return false

    const identity = imei ? `imei:${imei}` : `line:${lineKey(line)}`
    if (seen.has(identity)) return false
    seen.add(identity)
    return true
  })
}

export function deviceName(id: string): string {
  const device = devicesResource.data.find(item => item.imei === id)
  return device?.name || device?.model || id
}

export function contactForNumber(number: string): Contact | undefined {
  return contactsResource.data.find(contact =>
    contact.phones.some(
      phone =>
        phoneIdentitiesMatch(phone.normalized_number, number) ||
        phoneIdentitiesMatch(phone.number, number)
    )
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

export function refreshThreads(): Promise<MessageThread[] | null> {
  if (threadsRefresh) return threadsRefresh
  threadsRefresh = refreshResource(threadsResource, () => gateway.listThreads()).finally(() => {
    threadsRefresh = undefined
  })
  return threadsRefresh
}

export function loadCalls(force = false, filter: CallFilter = 'all'): Promise<CallRecord[] | null> {
  if (!force && filter === 'all' && callsResource.status === 'ready') {
    return Promise.resolve(callsResource.data)
  }
  return load(callsResource, () => gateway.listCalls(filter))
}

export function refreshCalls(): Promise<CallRecord[] | null> {
  if (callsRefreshRequest) return callsRefreshRequest
  callsRefreshRequest = refreshResource(callsResource, () => gateway.listCalls('all')).finally(() => {
    callsRefreshRequest = undefined
  })
  return callsRefreshRequest
}

export function markMissedCallsRead(): Promise<void> {
  if (!callsResource.data.some(call => call.missed && !call.read)) {
    return Promise.resolve()
  }
  if (missedCallsReadRequest) return missedCallsReadRequest
  missedCallsReadRequest = gateway
    .markMissedCallsRead()
    .then(() => {
      for (const call of callsResource.data) {
        if (call.missed) call.read = true
      }
    })
    .finally(() => {
      missedCallsReadRequest = undefined
    })
  return missedCallsReadRequest
}

export function loadDevices(force = false): Promise<Device[] | null> {
  if (!force && devicesResource.status === 'ready') return Promise.resolve(devicesResource.data)
  return load(devicesResource, () => gateway.listDevices())
}

export async function refreshDeviceWorkspace(): Promise<void> {
  if (!bootstrapRefresh) {
    bootstrapRefresh = refreshResource(bootstrapResource, () => gateway.getBootstrap()).finally(
      () => {
        bootstrapRefresh = undefined
      }
    )
  }
  if (!devicesRefresh) {
    devicesRefresh = refreshResource(devicesResource, () => gateway.listDevices()).finally(() => {
      devicesRefresh = undefined
    })
  }
  await Promise.all([bootstrapRefresh, devicesRefresh])
}

export async function createDevice(input: CreateDeviceInput): Promise<Device> {
  const saved = await gateway.createDevice(input)
  devicesResource.data = devicesResource.data
    .filter(device => device.imei !== saved.imei)
    .concat(saved)
    .sort((a, b) => (a.name || a.model || a.imei).localeCompare(b.name || b.model || b.imei))
  devicesResource.status = 'ready'
  devicesResource.error = ''
  return saved
}

export async function renameDevice(imei: string, input: RenameDeviceInput): Promise<Device> {
  const saved = await gateway.renameDevice(imei, input)
  devicesResource.data = devicesResource.data
    .map(device => (device.imei === saved.imei ? saved : device))
    .sort((a, b) => (a.name || a.model || a.imei).localeCompare(b.name || b.model || b.imei))
  devicesResource.status = 'ready'
  devicesResource.error = ''
  for (const line of bootstrapResource.data?.lines ?? []) {
    if (line.device_imei === saved.imei) line.device_name = saved.name
  }
  return saved
}

export async function deleteDevice(imei: string): Promise<void> {
  const normalizedIMEI = imei.trim()
  if (!normalizedIMEI) return
  await gateway.deleteDevice(normalizedIMEI)
  devicesResource.data = devicesResource.data.filter(
    device => device.imei !== normalizedIMEI
  )
  devicesResource.status = 'ready'
  devicesResource.error = ''
}

export async function updateLineLabel(
  lineID: string,
  input: UpdateLineLabelInput
): Promise<void> {
  const saved = await gateway.updateLineLabel(lineID, input)
  const normalizedLineID = lineID.trim()
  if (saved.line_id !== normalizedLineID) {
    throw new ApiError(translate('runtime.lineLabelMismatch'), 0, 'invalid_response')
  }
  const line = bootstrapResource.data?.lines.find(item => lineKey(item) === normalizedLineID)
  if (line) {
    line.line_label = saved.line_label
    line.line_color = saved.line_color
  }
  bootstrapResource.error = ''
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

export function messageQueryForThread(thread: MessageThread): MessageReadInput {
  return {
    line_id: thread.line_id,
    peer: thread.peer
  }
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
    gateway.listMessages(messageQueryForThread(thread))
  ).finally(() => {
    if (messageLoads.get(thread.key) === operation) messageLoads.delete(thread.key)
  })
  messageLoads.set(thread.key, operation)
  return operation
}

export function refreshMessages(thread: MessageThread): Promise<Message[] | null> {
  const pending = messageRefreshes.get(thread.key)
  if (pending) return pending
  const target = messagesFor(thread.key)
  const operation = refreshResource(target, () =>
    gateway.listMessages(messageQueryForThread(thread))
  ).finally(() => {
    if (messageRefreshes.get(thread.key) === operation) messageRefreshes.delete(thread.key)
  })
  messageRefreshes.set(thread.key, operation)
  return operation
}

export async function refreshIncomingMessage(
  event: IncomingMessageEvent,
  activeThreadKey = '',
  animate = true
): Promise<void> {
  const previousThread = threadsResource.data.find(thread => thread.key === event.thread_key)
  const threadWasPresent = Boolean(previousThread)
  const activeResource = activeThreadKey === event.thread_key
    ? messageResources[event.thread_key]
    : undefined
  const messagesWereReady = activeResource?.status === 'ready'
  const previousMessageIDs = new Set(activeResource?.data.map(message => message.id) || [])

  const threads = await refreshThreads()
  const thread = threads?.find(item => item.key === event.thread_key)
  if (
    animate &&
    thread &&
    (!threadWasPresent || thread.last_timestamp !== previousThread?.last_timestamp)
  ) {
    markArrival(recentIncomingThreadKeys, `thread:${event.thread_key}`, event.thread_key)
  }
  if (!thread || activeThreadKey !== event.thread_key) return

  const messages = await refreshMessages(thread)
  if (!messages) return
  if (thread.unread_count > 0) await markThreadRead(thread)
  if (!messagesWereReady || !animate) return
  const inserted = messages.filter(message => !previousMessageIDs.has(message.id))
  const eventMessage = inserted.find(message => message.id === event.message_id)
  if (eventMessage) {
    markArrival(recentIncomingMessageIDs, `message:${event.message_id}`, event.message_id)
    return
  }
  for (const message of inserted) {
    markArrival(recentIncomingMessageIDs, `message:${message.id}`, message.id)
  }
}

export async function refreshMessageWorkspace(activeThreadKey = ''): Promise<void> {
  const threads = await refreshThreads()
  if (!activeThreadKey) return
  const thread = threads?.find(item => item.key === activeThreadKey)
  if (thread) await refreshMessages(thread)
}

async function refreshResource<T>(
  target: Resource<T>,
  loader: () => Promise<T>
): Promise<T | null> {
  try {
    const data = await loader()
    target.data = data
    target.status = 'ready'
    target.error = ''
    return data
  } catch (error) {
    target.error = errorText(error)
    if (target.status === 'idle') target.status = 'error'
    return null
  }
}

function markArrival(
  target: Record<string, boolean>,
  timerKey: string,
  resourceKey: string
): void {
  target[resourceKey] = true
  const existing = arrivalTimers.get(timerKey)
  if (existing) clearTimeout(existing)
  arrivalTimers.set(timerKey, setTimeout(() => {
    delete target[resourceKey]
    arrivalTimers.delete(timerKey)
  }, 1400))
}

function messageReadError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'authentication_required' || error.code === 'csrf_failed') {
      return translate('runtime.markReadUnauthorized')
    }
    if (error.code === 'message_thread_not_found') {
      return translate('runtime.markReadMissing')
    }
    if (error.code === 'message_thread_identity_invalid') {
      return translate('runtime.markReadAmbiguousLine')
    }
    if (error.code === 'internal_error') {
      return translate('runtime.markReadSaveFailed')
    }
  }
  return translate('runtime.markReadFailed', { error: errorText(error) })
}

export async function markThreadRead(thread: MessageThread): Promise<boolean> {
  if (thread.unread_count <= 0) return true
  const key = thread.key
  threadReadErrors[key] = ''
  try {
    await requestThreadRead(messageQueryForThread(thread))
  } catch (error) {
    threadReadErrors[key] = messageReadError(error)
    return false
  }

  const current = threadsResource.data.find(
    item => item.key === key && item.peer === thread.peer
  )
  if (current) current.unread_count = 0
  delete threadReadErrors[key]
  return true
}

export async function updateDefaultLine(lineID: string): Promise<void> {
  const bootstrap = bootstrapResource.data
  if (!bootstrap) throw new Error(translate('runtime.lineSettingsNotLoaded'))
  const settings = await gateway.updateLineSettings({
    default_line_id: lineID,
    expected_revision: bootstrap.line_settings.revision
  })
  bootstrap.line_settings = settings
}

export async function saveContact(input: ContactInput, id?: string): Promise<Contact> {
  let saved: Contact
  if (id) {
    if (!gateway.updateContact) throw new Error(translate('runtime.contactWriteUnsupported'))
    saved = await gateway.updateContact(id, input)
  } else {
    if (!gateway.createContact) throw new Error(translate('runtime.contactWriteUnsupported'))
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
  if (!gateway.deleteContact) throw new Error(translate('runtime.contactWriteUnsupported'))
  await gateway.deleteContact(contact.id, contact.revision)
  contactsResource.data = contactsResource.data.filter(item => item.id !== contact.id)
}

function normalizedAddress(value: string): string {
  return normalizedPhoneIdentity(value) || value.trim().toLocaleLowerCase()
}

function findThreadForSentMessage(
  threads: MessageThread[],
  input: SendMessageInput,
  sent: Message
): MessageThread | undefined {
  const lineID = sent.line_id || input.line_id
  return threads.find(
    thread =>
      thread.line_id === lineID &&
      normalizedAddress(thread.peer) === normalizedAddress(sent.peer)
  )
}

export async function sendMessage(
  input: SendMessageInput
): Promise<{ message: Message; thread?: MessageThread }> {
  const sent = await gateway.sendMessage(input)
  playOutgoingMessageSound()
  const threads = await refreshThreads()
  const thread = findThreadForSentMessage(threads || threadsResource.data, input, sent)
  if (thread) {
    const target = messagesFor(thread.key)
    if (!target.data.some(message => message.id === sent.id)) {
      target.data = [...target.data, sent]
    }
    target.status = 'ready'
    thread.contact_name =
      thread.contact_name || contactForNumber(sent.peer)?.display_name
    thread.last_timestamp = sent.timestamp
    thread.last_content = sent.content
  }
  return { message: sent, thread }
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
