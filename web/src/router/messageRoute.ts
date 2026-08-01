import type { LocationQueryRaw } from 'vue-router'

const storageKey = 'modemdeck:message-route-references:v1'
const referencePattern = /^m_[a-f0-9]{16}$/
const maximumStoredReferences = 128

type ThreadReference = {
  kind: 'thread'
  reference: string
  threadKey: string
}

type ComposeReference = {
  kind: 'compose'
  reference: string
  recipient: string
  name: string
  lineKey: string
}

type MessageReference = ThreadReference | ComposeReference
type NewMessageReference =
  | Omit<ThreadReference, 'reference'>
  | Omit<ComposeReference, 'reference'>

export type MessageComposeContext = Pick<
  ComposeReference,
  'recipient' | 'name' | 'lineKey'
>

export type MessageRouteLocation = {
  name: 'messages'
  params?: { threadRef: string }
  query?: LocationQueryRaw
}

const referenceRecords = new Map<string, MessageReference>()
const identityReferences = new Map<string, string>()
let storageLoaded = false
let fallbackSequence = 0

function browserStorage(): Storage | undefined {
  if (typeof window === 'undefined') return undefined
  try {
    return window.sessionStorage
  } catch {
    return undefined
  }
}

function identity(record: NewMessageReference): string {
  return record.kind === 'thread'
    ? `thread:${record.threadKey}`
    : `compose:${JSON.stringify([
        record.recipient,
        record.name,
        record.lineKey
      ])}`
}

function validRecord(value: unknown): value is MessageReference {
  if (!value || typeof value !== 'object') return false
  const record = value as Partial<MessageReference>
  if (
    typeof record.reference !== 'string' ||
    !referencePattern.test(record.reference)
  ) return false
  if (record.kind === 'thread') return typeof record.threadKey === 'string'
  return (
    record.kind === 'compose' &&
    typeof record.recipient === 'string' &&
    typeof record.name === 'string' &&
    typeof record.lineKey === 'string'
  )
}

function loadStoredReferences(): void {
  if (storageLoaded) return
  storageLoaded = true
  const storage = browserStorage()
  if (!storage) return
  try {
    const value = JSON.parse(storage.getItem(storageKey) || '[]') as unknown
    if (!Array.isArray(value)) return
    for (const record of value) {
      if (!validRecord(record)) continue
      referenceRecords.set(record.reference, record)
      const storedIdentity = identity(record)
      identityReferences.set(storedIdentity, record.reference)
    }
  } catch {
    // A damaged or unavailable session cache must not block navigation.
  }
}

function saveReferences(): void {
  const storage = browserStorage()
  if (!storage) return
  try {
    const records = [...referenceRecords.values()].slice(
      -maximumStoredReferences
    )
    storage.setItem(storageKey, JSON.stringify(records))
  } catch {
    // In-memory references still keep navigation working for this page.
  }
}

function randomReference(): string {
  const bytes = new Uint8Array(8)
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes)
  } else {
    fallbackSequence += 1
    const seed = `${Date.now().toString(16)}${fallbackSequence.toString(16)}`
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = seed.charCodeAt(index % seed.length) ^ (index * 29)
    }
  }
  return `m_${[...bytes]
    .map(value => value.toString(16).padStart(2, '0'))
    .join('')}`
}

function remember(record: NewMessageReference): string {
  loadStoredReferences()
  const recordIdentity = identity(record)
  const existing = identityReferences.get(recordIdentity)
  if (existing) return existing

  let reference = randomReference()
  while (referenceRecords.has(reference)) reference = randomReference()
  const stored = { ...record, reference } as MessageReference
  referenceRecords.set(reference, stored)
  identityReferences.set(recordIdentity, reference)

  while (referenceRecords.size > maximumStoredReferences) {
    const oldestReference = referenceRecords.keys().next().value
    if (typeof oldestReference !== 'string') break
    const oldest = referenceRecords.get(oldestReference)
    referenceRecords.delete(oldestReference)
    if (oldest) identityReferences.delete(identity(oldest))
  }
  saveReferences()
  return reference
}

function routeValue(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function routeLocation(
  params?: { threadRef: string },
  query?: LocationQueryRaw
): MessageRouteLocation {
  return {
    name: 'messages',
    ...(params && Object.keys(params).length > 0 ? { params } : {}),
    ...(query && Object.keys(query).length > 0 ? { query } : {})
  }
}

export function messageThreadReference(threadKey: string): string {
  const normalized = threadKey.trim()
  return normalized
    ? remember({ kind: 'thread', threadKey: normalized })
    : ''
}

export function messageThreadKeyFromReference(value: unknown): string {
  const reference = routeValue(value)
  if (!referencePattern.test(reference)) return ''
  loadStoredReferences()
  const record = referenceRecords.get(reference)
  return record?.kind === 'thread' ? record.threadKey : ''
}

export function messageThreadRoute(
  threadKey: string,
  query?: LocationQueryRaw
): MessageRouteLocation {
  const reference = messageThreadReference(threadKey)
  return routeLocation(reference ? { threadRef: reference } : undefined, query)
}

export function messageComposeRoute(
  context: MessageComposeContext,
  query?: LocationQueryRaw
): MessageRouteLocation {
  const normalized = {
    recipient: context.recipient.trim(),
    name: context.name.trim(),
    lineKey: context.lineKey.trim()
  }
  const compose =
    normalized.recipient || normalized.name || normalized.lineKey
      ? remember({ kind: 'compose', ...normalized })
      : ''
  return routeLocation(undefined, { ...query, compose })
}

export function messageComposeContextFromReference(
  value: unknown
): MessageComposeContext | undefined {
  const reference = routeValue(value)
  const isExplicitlyBlank =
    value === '' ||
    (Array.isArray(value) && value.length > 0 && value[0] === '')
  if (isExplicitlyBlank) return { recipient: '', name: '', lineKey: '' }
  if (!referencePattern.test(reference)) return undefined
  loadStoredReferences()
  const record = referenceRecords.get(reference)
  if (record?.kind !== 'compose') return undefined
  return {
    recipient: record.recipient,
    name: record.name,
    lineKey: record.lineKey
  }
}
