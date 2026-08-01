import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  BootstrapResponse,
  CallFilter,
  CallRecord,
  CommunicationCapabilityName,
  Contact,
  ContactInput,
  ContactPhone,
  CreateDeviceInput,
  Device,
  IncomingMessageEvent,
  LineSummary,
  Message,
  MessageReadInput,
  MessageThread,
  Page,
  Resource,
  ResourceStatus,
  RenameDeviceInput,
  RuntimeCommunicationState,
  SendMessageInput,
  TelegramUnit,
  TelegramUnitInput,
  UpdateLineLabelInput
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import { formatPhoneNumber } from '../utils/format'
import { playOutgoingMessageSound } from './browserSounds'
import { normalizedPhoneIdentity } from '../utils/lineIdentity'
import {
  acceptFirstPage,
  acceptNextPage,
  mergeUnique,
  paginationState,
  resetPagination,
  type PaginationState
} from './pagination'

function resource<T>(data: T): Resource<T> {
  return reactive({ status: 'idle', data, error: '' }) as Resource<T>
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
}

let workspaceGeneration = 0

async function load<T>(target: Resource<T>, loader: () => Promise<T>): Promise<T | null> {
  const generation = workspaceGeneration
  target.status = 'loading'
  target.error = ''
  try {
    const data = await loader()
    if (generation !== workspaceGeneration) return null
    target.data = data
    target.status = 'ready'
    return data
  } catch (error) {
    if (generation !== workspaceGeneration) return null
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
export const contactsPagination = reactive(paginationState())
export const threadsPagination = reactive(paginationState())
export const callsPagination = reactive(paginationState())
export const messagePaginationStates = reactive<Record<string, PaginationState>>({})
export const threadReadErrors = reactive<Record<string, string>>({})
export const recentIncomingMessageIDs = reactive<Record<string, boolean>>({})
export const recentIncomingThreadKeys = reactive<Record<string, boolean>>({})
export const contactEditingAvailable = gateway.interactions.contacts

let threadsLoad: Promise<MessageThread[] | null> | undefined
let bootstrapLoad: Promise<BootstrapResponse | null> | undefined
let contactsLoad: Promise<Contact[] | null> | undefined
let devicesLoad: Promise<Device[] | null> | undefined
let telegramLoad: Promise<TelegramUnit[] | null> | undefined
let threadsRefresh: Promise<MessageThread[] | null> | undefined
let contactsRefresh: Promise<Contact[] | null> | undefined
let bootstrapRefresh: Promise<BootstrapResponse | null> | undefined
let devicesRefresh: Promise<Device[] | null> | undefined
let callsRefreshRequest: Promise<CallRecord[] | null> | undefined
let missedCallsReadRequest: Promise<void> | undefined
const messageLoads = new Map<string, Promise<Message[] | null>>()
const messageRefreshes = new Map<string, Promise<Message[] | null>>()
const arrivalTimers = new Map<string, ReturnType<typeof setTimeout>>()
let callsQueryFilter: CallFilter = 'all'

function pageErrorStatus(error: unknown): ResourceStatus {
  return error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
}

async function loadFirstPage<T>(
  target: Resource<T[]>,
  pagination: PaginationState,
  loader: () => Promise<Page<T>>
): Promise<T[] | null> {
  resetPagination(pagination)
  const generation = workspaceGeneration
  const pageGeneration = pagination.generation
  target.status = 'loading'
  target.error = ''
  try {
    const page = await loader()
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation
    ) return null
    target.data = page.items
    target.status = 'ready'
    acceptFirstPage(pagination, page.meta)
    return target.data
  } catch (error) {
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation
    ) return null
    target.status = pageErrorStatus(error)
    target.error = errorText(error)
    pagination.error = target.error
    return null
  }
}

async function refreshFirstPage<T>(
  target: Resource<T[]>,
  pagination: PaginationState,
  loader: () => Promise<Page<T>>,
  identity: (item: T) => string,
  sort?: (left: T, right: T) => number
): Promise<T[] | null> {
  const generation = workspaceGeneration
  const pageGeneration = pagination.generation
  try {
    const page = await loader()
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation
    ) return null
    if (pagination.pages > 1) {
      const merged = mergeUnique(page.items, target.data, identity)
      target.data = sort ? merged.sort(sort) : merged
    } else {
      target.data = page.items
      acceptFirstPage(pagination, page.meta)
    }
    target.status = 'ready'
    target.error = ''
    return target.data
  } catch (error) {
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation
    ) return null
    target.error = errorText(error)
    if (target.status === 'idle') target.status = pageErrorStatus(error)
    return null
  }
}

async function loadNextPage<T>(
  target: Resource<T[]>,
  pagination: PaginationState,
  loader: (cursor: string) => Promise<Page<T>>,
  restart: () => Promise<Page<T>>,
  identity: (item: T) => string,
  prepend = false
): Promise<T[] | null> {
  const cursor = pagination.nextCursor
  if (
    target.status !== 'ready' ||
    !pagination.hasMore ||
    !cursor ||
    pagination.loadingMore
  ) return target.data

  const generation = workspaceGeneration
  const pageGeneration = pagination.generation
  pagination.loadingMore = true
  pagination.error = ''
  try {
    const page = await loader(cursor)
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation ||
      pagination.nextCursor !== cursor
    ) return null
    target.data = prepend
      ? mergeUnique(page.items, target.data, identity)
      : mergeUnique(target.data, page.items, identity)
    acceptNextPage(pagination, page.meta)
    return target.data
  } catch (error) {
    if (
      generation !== workspaceGeneration ||
      pageGeneration !== pagination.generation
    ) return null
    if (
      error instanceof ApiError &&
      error.code === 'invalid_argument' &&
      error.field === 'cursor'
    ) {
      return loadFirstPage(target, pagination, restart)
    }
    pagination.error = errorText(error)
    return null
  } finally {
    if (
      generation === workspaceGeneration &&
      pageGeneration === pagination.generation
    ) {
      pagination.loadingMore = false
    }
  }
}

function messageOrder(left: Message, right: Message): number {
  return Date.parse(left.timestamp) - Date.parse(right.timestamp) ||
    Number(left.id) - Number(right.id) ||
    left.id.localeCompare(right.id)
}

export function createMessageReadCoordinator(
  request: (input: MessageReadInput) => Promise<void>
): (input: MessageReadInput) => Promise<void> {
  type PendingRead = {
    dirty: boolean
    input: MessageReadInput
    operation: Promise<void>
  }
  const requests = new Map<string, PendingRead>()
  return input => {
    const key = `${input.line_id.trim()}\u0000${input.peer.trim()}`
    const pending = requests.get(key)
    if (pending) {
      pending.input = input
      pending.dirty = true
      return pending.operation
    }
    const entry = {
      dirty: false,
      input,
      operation: Promise.resolve()
    } satisfies PendingRead
    entry.operation = (async () => {
      do {
        const nextInput = entry.input
        entry.dirty = false
        await request(nextInput)
      } while (entry.dirty)
    })().finally(() => {
      if (requests.get(key) === entry) requests.delete(key)
    })
    requests.set(key, entry)
    return entry.operation
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
  if (
    capability === 'dial' &&
    (!bootstrap.capabilities.webrtc_audio ||
      !bootstrap.lines.some(lineCanPlaceVoiceCall))
  ) {
    return translate(
      bootstrap.lines.some(lineHasCallControl)
        ? 'runtime.callControlOnly'
        : 'runtime.voiceCallingUnavailable'
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
  const bootstrap = bootstrapResource.data
  const normalizedKey = key.trim()
  if (!normalizedKey) return undefined
  return (
    bootstrap?.lines.find(line => lineKey(line) === normalizedKey) ||
    bootstrap?.line_catalog.find(line => lineKey(line) === normalizedKey)
  )
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

  const explicitlyPreferredLine = byID(options.preferredLineID)
  if (explicitlyPreferredLine) return explicitlyPreferredLine

  const contact =
    options.preferredLineID || !options.number
      ? undefined
      : contactForNumber(options.number)
  const contextLine = byID(options.contextKey)
  if (contextLine) return contextLine

  const preferredLine = byID(contact?.preferred_line_id)
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

export function lineCanPlaceVoiceCall(line: LineSummary | undefined): boolean {
  return (
    lineSupports(line, 'dial') === true &&
    lineSupports(line, 'media') === true
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
      home_country_iso: device.sim_inserted ? sim?.home_country_iso || '' : '',
      serving_operator_code: device.sim_inserted ? sim?.serving_operator_code || '' : '',
      serving_operator_name: device.sim_inserted ? sim?.serving_operator_name || '' : '',
      serving_country_iso: device.sim_inserted ? sim?.serving_country_iso || '' : '',
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
  const identity = normalizedPhoneIdentity(number)
  if (!identity.startsWith('+')) return undefined
  const matches = contactsResource.data.filter(contact =>
    contact.phones.some(phone => contactPhoneIdentity(phone) === identity)
  )
  return matches.length === 1 ? matches[0] : undefined
}

export function contactPhoneForNumber(number: string): ContactPhone | undefined {
  const identity = normalizedPhoneIdentity(number)
  const contact = contactForNumber(number)
  if (!contact) return undefined
  return contact.phones.find(phone => contactPhoneIdentity(phone) === identity)
}

export function displayPhoneNumber(number: string, lineReference = ''): string {
  const contactNumber = contactPhoneForNumber(number)?.number.trim()
  if (contactNumber) return contactNumber
  return formatPhoneNumber(number, lineForKey(lineReference)?.home_country_iso)
}

function contactPhoneIdentity(phone: ContactPhone): string {
  const canonical = normalizedPhoneIdentity(phone.normalized_number)
  if (canonical.startsWith('+')) return canonical
  const explicitNumber = normalizedPhoneIdentity(phone.number)
  return explicitNumber.startsWith('+') ? explicitNumber : ''
}

export function loadBootstrap(force = false): Promise<BootstrapResponse | null> {
  if (!force && bootstrapResource.status === 'ready') return Promise.resolve(bootstrapResource.data)
  if (bootstrapLoad) return bootstrapLoad
  bootstrapLoad = load(bootstrapResource, () => gateway.getBootstrap()).finally(() => {
    bootstrapLoad = undefined
  })
  return bootstrapLoad
}

export function acceptRuntimeCommunicationState(
  state: RuntimeCommunicationState
): void {
  const bootstrap = bootstrapResource.data
  if (bootstrap) {
    bootstrap.capabilities = state.capabilities
    bootstrap.lines = state.lines
    bootstrap.line_catalog = state.line_catalog
    bootstrapResource.status = 'ready'
    bootstrapResource.error = ''
  }
  if (state.devices) {
    devicesResource.data = state.devices
    devicesResource.status = 'ready'
    devicesResource.error = ''
  }
}

export function loadContacts(force = false): Promise<Contact[] | null> {
  if (!force && contactsResource.status === 'ready') return Promise.resolve(contactsResource.data)
  if (contactsLoad) return contactsLoad
  contactsLoad = loadFirstPage(contactsResource, contactsPagination, () =>
    gateway.listContacts()
  ).finally(() => {
    contactsLoad = undefined
  })
  return contactsLoad
}

export function loadMoreContacts(): Promise<Contact[] | null> {
  return loadNextPage(
    contactsResource,
    contactsPagination,
    cursor => gateway.listContacts({ cursor }),
    () => gateway.listContacts(),
    contact => contact.id
  )
}

export function refreshContacts(): Promise<Contact[] | null> {
  if (contactsRefresh) return contactsRefresh
  contactsRefresh = refreshFirstPage(
    contactsResource,
    contactsPagination,
    () => gateway.listContacts(),
    contact => contact.id
  ).finally(() => {
    contactsRefresh = undefined
  })
  return contactsRefresh
}

export function loadThreads(force = false): Promise<MessageThread[] | null> {
  if (!force && threadsResource.status === 'ready') return Promise.resolve(threadsResource.data)
  if (threadsLoad) return threadsLoad
  threadsLoad = loadFirstPage(
    threadsResource,
    threadsPagination,
    () => gateway.listThreads()
  ).finally(() => {
    threadsLoad = undefined
  })
  return threadsLoad
}

export function loadMoreThreads(): Promise<MessageThread[] | null> {
  return loadNextPage(
    threadsResource,
    threadsPagination,
    cursor => gateway.listThreads({ cursor }),
    () => gateway.listThreads(),
    thread => thread.key
  )
}

export function refreshThreads(): Promise<MessageThread[] | null> {
  if (threadsRefresh) return threadsRefresh
  threadsRefresh = refreshFirstPage(
    threadsResource,
    threadsPagination,
    () => gateway.listThreads(),
    thread => thread.key
  ).finally(() => {
    threadsRefresh = undefined
  })
  return threadsRefresh
}

export function loadCalls(force = false, filter: CallFilter = 'all'): Promise<CallRecord[] | null> {
  if (!force && filter === callsQueryFilter && callsResource.status === 'ready') {
    return Promise.resolve(callsResource.data)
  }
  callsQueryFilter = filter
  return loadFirstPage(callsResource, callsPagination, () =>
    gateway.listCalls(filter)
  )
}

export function loadMoreCalls(): Promise<CallRecord[] | null> {
  return loadNextPage(
    callsResource,
    callsPagination,
    cursor => gateway.listCalls(callsQueryFilter, { cursor }),
    () => gateway.listCalls(callsQueryFilter),
    call => call.id
  )
}

export function refreshCalls(): Promise<CallRecord[] | null> {
  if (callsRefreshRequest) return callsRefreshRequest
  callsRefreshRequest = refreshFirstPage(
    callsResource,
    callsPagination,
    () => gateway.listCalls(callsQueryFilter),
    call => call.id
  ).finally(() => {
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

export function callCanHaveReadState(call: CallRecord): boolean {
  return call.missed
}

export async function markMissedCallRead(call: CallRecord): Promise<void> {
  if (!call.missed || call.read) return
  await updateMissedCallsReadState([call], true)
}

export async function markMissedCallUnread(call: CallRecord): Promise<void> {
  if (!call.missed || !call.read) return
  await updateMissedCallsReadState([call], false)
}

export async function updateMissedCallsReadState(
  calls: CallRecord[],
  read: boolean
): Promise<void> {
  const ids = Array.from(
    new Set(calls.filter(callCanHaveReadState).map(call => call.id))
  )
  if (ids.length === 0) return
  await gateway.updateCalls(read ? 'read' : 'unread', ids)
  const selected = new Set(ids)
  for (const call of callsResource.data) {
    if (selected.has(call.id) && call.missed) call.read = read
  }
}

export async function setCallsFavorite(
  calls: CallRecord[],
  favorite: boolean
): Promise<void> {
  const ids = Array.from(new Set(calls.map(call => call.id)))
  if (ids.length === 0) return
  await gateway.updateCalls(favorite ? 'favorite' : 'unfavorite', ids)
  const selected = new Set(ids)
  for (const call of callsResource.data) {
    if (selected.has(call.id)) call.favorite = favorite
  }
}

export async function deleteCall(call: CallRecord): Promise<void> {
  await deleteCalls([call])
}

export async function deleteCalls(calls: CallRecord[]): Promise<void> {
  const ids = Array.from(new Set(calls.map(call => call.id)))
  if (ids.length === 0) return
  await gateway.updateCalls('delete', ids)
  const deleted = new Set(ids)
  callsResource.data = callsResource.data.filter(item => !deleted.has(item.id))
}

export function loadDevices(force = false): Promise<Device[] | null> {
  if (!force && devicesResource.status === 'ready') return Promise.resolve(devicesResource.data)
  if (devicesLoad) return devicesLoad
  devicesLoad = load(devicesResource, () => gateway.listDevices()).finally(() => {
    devicesLoad = undefined
  })
  return devicesLoad
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
  if (telegramLoad) return telegramLoad
  telegramLoad = load(telegramResource, () => gateway.listTelegramUnits()).finally(() => {
    telegramLoad = undefined
  })
  return telegramLoad
}

export function messagesFor(threadKey: string): Resource<Message[]> {
  if (!messageResources[threadKey]) messageResources[threadKey] = resource<Message[]>([])
  return messageResources[threadKey]
}

export function messagePaginationFor(threadKey: string): PaginationState {
  if (!messagePaginationStates[threadKey]) {
    messagePaginationStates[threadKey] = paginationState()
  }
  return messagePaginationStates[threadKey]
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
  const operation = loadFirstPage(
    target,
    messagePaginationFor(thread.key),
    () => gateway.listMessages(messageQueryForThread(thread))
  ).finally(() => {
    if (messageLoads.get(thread.key) === operation) messageLoads.delete(thread.key)
  })
  messageLoads.set(thread.key, operation)
  return operation
}

export function loadMoreMessages(thread: MessageThread): Promise<Message[] | null> {
  const target = messagesFor(thread.key)
  return loadNextPage(
    target,
    messagePaginationFor(thread.key),
    cursor => gateway.listMessages({
      ...messageQueryForThread(thread),
      cursor
    }),
    () => gateway.listMessages(messageQueryForThread(thread)),
    message => message.id,
    true
  )
}

export function refreshMessages(thread: MessageThread): Promise<Message[] | null> {
  const pending = messageRefreshes.get(thread.key)
  if (pending) return pending
  const target = messagesFor(thread.key)
  const operation = refreshFirstPage(
    target,
    messagePaginationFor(thread.key),
    () => gateway.listMessages(messageQueryForThread(thread)),
    message => message.id,
    messageOrder
  ).finally(() => {
    if (messageRefreshes.get(thread.key) === operation) messageRefreshes.delete(thread.key)
  })
  messageRefreshes.set(thread.key, operation)
  return operation
}

export function noteIncomingMessageArrival(event: IncomingMessageEvent): void {
  markArrival(recentIncomingThreadKeys, `thread:${event.thread_key}`, event.thread_key)
  markArrival(recentIncomingMessageIDs, `message:${event.message_id}`, event.message_id)
}

export async function refreshMessageWorkspace(activeThreadKey = ''): Promise<void> {
  const normalizedActiveKey = activeThreadKey.trim()
  const threads = await refreshThreads()
  if (!normalizedActiveKey) return

  const currentThreads = threads || threadsResource.data
  const thread = currentThreads.find(item => item.key === normalizedActiveKey)
  if (thread) await refreshMessages(thread)
}

async function refreshResource<T>(
  target: Resource<T>,
  loader: () => Promise<T>
): Promise<T | null> {
  const generation = workspaceGeneration
  try {
    const data = await loader()
    if (generation !== workspaceGeneration) return null
    target.data = data
    target.status = 'ready'
    target.error = ''
    return data
  } catch (error) {
    if (generation !== workspaceGeneration) return null
    target.error = errorText(error)
    if (target.status === 'idle') target.status = 'error'
    return null
  }
}

export function resetWorkspaceState(): void {
  workspaceGeneration += 1
  bootstrapResource.status = 'idle'
  bootstrapResource.data = null
  bootstrapResource.error = ''
  for (const target of [
    contactsResource,
    threadsResource,
    callsResource,
    devicesResource,
    telegramResource
  ]) {
    target.status = 'idle'
    target.data = []
    target.error = ''
  }
  for (const key of Object.keys(messageResources)) delete messageResources[key]
  for (const key of Object.keys(messagePaginationStates)) {
    delete messagePaginationStates[key]
  }
  resetPagination(contactsPagination)
  resetPagination(threadsPagination)
  resetPagination(callsPagination)
  callsQueryFilter = 'all'
  for (const key of Object.keys(threadReadErrors)) delete threadReadErrors[key]
  for (const key of Object.keys(recentIncomingMessageIDs)) {
    delete recentIncomingMessageIDs[key]
  }
  for (const key of Object.keys(recentIncomingThreadKeys)) {
    delete recentIncomingThreadKeys[key]
  }
  for (const timer of arrivalTimers.values()) clearTimeout(timer)
  arrivalTimers.clear()
  messageLoads.clear()
  messageRefreshes.clear()
  threadsLoad = undefined
  bootstrapLoad = undefined
  contactsLoad = undefined
  devicesLoad = undefined
  telegramLoad = undefined
  threadsRefresh = undefined
  contactsRefresh = undefined
  bootstrapRefresh = undefined
  devicesRefresh = undefined
  callsRefreshRequest = undefined
  missedCallsReadRequest = undefined
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
  if (!threadIsUnread(thread)) return true
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
  if (current) {
    current.unread_count = 0
    current.marked_unread = false
  }
  delete threadReadErrors[key]
  return true
}

export function threadIsUnread(thread: MessageThread): boolean {
  return thread.unread_count > 0 || thread.marked_unread
}

export async function markThreadsRead(threads: MessageThread[]): Promise<void> {
  await updateMessageThreadsState(threads.filter(threadIsUnread), 'read')
}

export async function markThreadsUnread(threads: MessageThread[]): Promise<void> {
  await updateMessageThreadsState(
    threads.filter(thread => !threadIsUnread(thread)),
    'unread'
  )
}

export async function setThreadsFavorite(
  threads: MessageThread[],
  favorite: boolean
): Promise<void> {
  await updateMessageThreadsState(
    threads.filter(thread => thread.favorite !== favorite),
    favorite ? 'favorite' : 'unfavorite'
  )
}

async function updateMessageThreadsState(
  threads: MessageThread[],
  action: 'read' | 'unread' | 'favorite' | 'unfavorite'
): Promise<void> {
  const unique = Array.from(
    new Map(threads.map(thread => [thread.key, thread])).values()
  )
  if (unique.length === 0) return
  await gateway.updateMessageThreads(action, unique.map(messageQueryForThread))
  const keys = new Set(unique.map(thread => thread.key))
  for (const thread of threadsResource.data) {
    if (!keys.has(thread.key)) continue
    if (action === 'read') {
      thread.unread_count = 0
      thread.marked_unread = false
    } else if (action === 'unread') {
      thread.marked_unread = true
    } else {
      thread.favorite = action === 'favorite'
    }
  }
}

export async function deleteMessageThread(thread: MessageThread): Promise<void> {
  await deleteMessageThreads([thread])
}

export async function deleteMessageThreads(threads: MessageThread[]): Promise<void> {
  const unique = Array.from(
    new Map(threads.map(thread => [thread.key, thread])).values()
  )
  if (unique.length === 0) return
  await gateway.updateMessageThreads('delete', unique.map(messageQueryForThread))
  const deleted = new Set(unique.map(thread => thread.key))
  threadsResource.data = threadsResource.data.filter(item => !deleted.has(item.key))
  for (const key of deleted) {
    delete messageResources[key]
    delete threadReadErrors[key]
  }
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
  await deleteContacts([contact])
}

export async function deleteContacts(contacts: Contact[]): Promise<void> {
  if (!gateway.deleteContacts) throw new Error(translate('runtime.contactWriteUnsupported'))
  const unique = Array.from(
    new Map(contacts.map(contact => [contact.id, contact])).values()
  )
  if (unique.length === 0) return
  await gateway.deleteContacts(
    unique.map(contact => ({
      id: contact.id,
      revision: contact.revision || 0
    }))
  )
  const deleted = new Set(unique.map(contact => contact.id))
  contactsResource.data = contactsResource.data.filter(item => !deleted.has(item.id))
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
