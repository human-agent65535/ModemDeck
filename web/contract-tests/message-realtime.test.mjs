import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  incomingMessageRoute,
  shouldAlertIncomingMessage
} from '../src/state/messageRuntime.ts'
import {
  browserNotificationsActive,
  shouldDisplayBrowserNotification
} from '../src/state/browserNotifications.ts'
import {
  claimIncomingCallNotification,
  incomingCallRoute
} from '../src/state/call.ts'
import { gateway } from '../src/api/client.ts'
import { messageThreadKeyFromReference } from '../src/router/messageRoute.ts'
import {
  loadMessages,
  messagesFor,
  refreshMessageWorkspace,
  resetWorkspaceState
} from '../src/state/workspace.ts'

const event = {
  message_id: '42',
  thread_key: 'line-main|+818012345678',
  line_id: 'line-main',
  peer: '+818012345678',
  content: 'hello',
  timestamp: '2026-07-24T07:30:00Z',
  observed_at: '2026-07-24T07:30:05Z'
}

test('browser permission and the local notification preference remain independent', () => {
  assert.equal(browserNotificationsActive(false, true, true, 'granted'), false)
  assert.equal(browserNotificationsActive(true, true, true, 'default'), false)
  assert.equal(browserNotificationsActive(true, true, true, 'denied'), false)
  assert.equal(browserNotificationsActive(true, false, true, 'granted'), false)
  assert.equal(browserNotificationsActive(true, true, false, 'granted'), false)
  assert.equal(browserNotificationsActive(true, true, true, 'granted'), true)
})

test('enabled browser notifications are delivered for every live event', () => {
  assert.equal(shouldDisplayBrowserNotification(true, true), true)
  assert.equal(shouldDisplayBrowserNotification(false, true), false)
  assert.equal(shouldDisplayBrowserNotification(true, false), false)
})

test('notification click route preserves the exact line and peer thread identity', () => {
  const route = incomingMessageRoute(event)
  assert.equal(route.name, 'messages')
  assert.match(route.params.threadRef, /^m_[a-f0-9]{16}$/)
  assert.equal(
    messageThreadKeyFromReference(route.params.threadRef),
    event.thread_key
  )
})

test('incoming call notification is claimed once for one genuinely ringing call', () => {
  const claimed = new Set()
  const incomingCall = {
    id: 'call-incoming-1',
    line_id: 'line-main',
    direction: 'incoming',
    remote_number: '+818012345678',
    phase: 'ringing',
    control_state: 'available',
    media_available: false,
    created_at: '2026-07-24T08:00:00Z'
  }

  assert.equal(claimIncomingCallNotification(incomingCall, claimed), true)
  assert.equal(claimIncomingCallNotification(incomingCall, claimed), false)
  assert.equal(
    claimIncomingCallNotification(
      { ...incomingCall, id: 'call-outgoing-1', direction: 'outgoing' },
      claimed
    ),
    false
  )
  assert.equal(
    claimIncomingCallNotification(
      { ...incomingCall, id: 'call-active-1', phase: 'active' },
      claimed
    ),
    false
  )
  assert.equal(
    claimIncomingCallNotification(
      { ...incomingCall, id: 'call-occupied-1', control_state: 'occupied' },
      claimed
    ),
    false
  )
  assert.equal(
    claimIncomingCallNotification({ ...incomingCall, id: 'call-incoming-2' }, claimed),
    true
  )
  assert.deepEqual(incomingCallRoute(), { name: 'calls' })
})

test('incoming call notification history stays bounded', () => {
  const claimed = new Set()
  for (let index = 0; index < 300; index += 1) {
    assert.equal(
      claimIncomingCallNotification(
        {
          id: `call-${index}`,
          line_id: 'line-main',
          direction: 'incoming',
          remote_number: '+818012345678',
          phase: 'ringing',
          control_state: 'available',
          media_available: false,
          created_at: '2026-07-24T08:00:00Z'
        },
        claimed
      ),
      true
    )
  }
  assert.equal(claimed.size, 256)
  assert.equal(claimed.has('call-0'), false)
  assert.equal(claimed.has('call-299'), true)
})

test('incoming SMS alerts require a fresh server observation', () => {
  const now = Date.parse('2026-07-24T07:31:00Z')

  assert.equal(shouldAlertIncomingMessage(event, now), true)
  assert.equal(
    shouldAlertIncomingMessage(
      { ...event, observed_at: '2026-07-24T07:29:59Z' },
      now
    ),
    false
  )
  assert.equal(
    shouldAlertIncomingMessage({ ...event, observed_at: 'invalid' }, now),
    false
  )
})

test('message SSE observes heartbeats and reconnects without replay state', () => {
  const originalEventSource = globalThis.EventSource
  const originalSetTimeout = globalThis.setTimeout
  const originalClearTimeout = globalThis.clearTimeout
  const timers = new Map()
  let nextTimerID = 0

  class FakeEventSource {
    static instances = []

    constructor(url, options) {
      this.closed = false
      this.listeners = new Map()
      this.url = url
      this.withCredentials = options?.withCredentials
      FakeEventSource.instances.push(this)
    }

    addEventListener(type, handler) {
      this.listeners.set(type, handler)
    }

    close() {
      this.closed = true
    }

    emit(type, data) {
      this.listeners.get(type)?.({ data })
    }
  }

  try {
    globalThis.EventSource = FakeEventSource
    globalThis.setTimeout = callback => {
      nextTimerID += 1
      timers.set(nextTimerID, callback)
      return nextTimerID
    }
    globalThis.clearTimeout = timerID => {
      timers.delete(timerID)
    }

    const messages = []
    const close = gateway.subscribeMessageEvents({
      onMessage(message) {
        messages.push(message)
      }
    })

    assert.equal(FakeEventSource.instances[0].url, '/api/v1/messages/events')
    assert.equal(FakeEventSource.instances[0].withCredentials, true)
    assert.equal(FakeEventSource.instances[0].listeners.has('ready'), false)
    assert.equal(FakeEventSource.instances[0].listeners.has('reset'), false)
    FakeEventSource.instances[0].emit(
      'heartbeat',
      '{"at":"2026-07-30T08:00:00Z"}'
    )

    const [firstTimerID, expireFirst] = timers.entries().next().value
    timers.delete(firstTimerID)
    expireFirst()

    assert.equal(FakeEventSource.instances[0].closed, true)
    assert.equal(FakeEventSource.instances[1].url, '/api/v1/messages/events')

    FakeEventSource.instances[1].emit(
      'sms',
      JSON.stringify({ ...event, message_id: '43' })
    )
    assert.equal(messages.length, 1)
    assert.equal(messages[0].message_id, '43')

    const [secondTimerID, expireSecond] = timers.entries().next().value
    timers.delete(secondTimerID)
    expireSecond()

    assert.equal(FakeEventSource.instances[2].url, '/api/v1/messages/events')
    close()
    assert.equal(FakeEventSource.instances[2].closed, true)
    assert.equal(timers.size, 0)
  } finally {
    globalThis.setTimeout = originalSetTimeout
    globalThis.clearTimeout = originalClearTimeout
    if (originalEventSource === undefined) {
      delete globalThis.EventSource
    } else {
      globalThis.EventSource = originalEventSource
    }
  }
})

test('message invalidation reloads only the visible thread without mutating hidden caches', async () => {
  const originalListThreads = gateway.listThreads
  const originalListMessages = gateway.listMessages
  const threads = Array.from({ length: 20 }, (_, index) => ({
    key: `line-main|+81800000${String(index).padStart(3, '0')}`,
    line_id: 'line-main',
    peer: `+81800000${String(index).padStart(3, '0')}`,
    last_timestamp: `2026-07-24T07:${String(index).padStart(2, '0')}:00Z`,
    unread_count: 0,
    marked_unread: false,
    favorite: false
  }))
  const page = items => ({
    items,
    meta: { next_cursor: '', has_more: false }
  })
  const messageRequests = []

  try {
    resetWorkspaceState()
    gateway.listThreads = async () => page(threads)
    gateway.listMessages = async query => {
      messageRequests.push(query.peer)
      return page([])
    }

    for (const thread of threads) await loadMessages(thread, true)
    messageRequests.length = 0

    const visible = threads[7]
    await refreshMessageWorkspace(visible.key)

    assert.deepEqual(messageRequests, [visible.peer])
    assert.equal(messagesFor(visible.key).status, 'ready')
    for (const thread of threads) {
      if (thread.key !== visible.key) {
        assert.equal(messagesFor(thread.key).status, 'ready')
      }
    }

    const reopened = threads[12]
    messageRequests.length = 0
    await loadMessages(reopened, true)
    assert.deepEqual(messageRequests, [reopened.peer])

    messageRequests.length = 0
    await refreshMessageWorkspace('')
    assert.deepEqual(messageRequests, [])
    assert.equal(messagesFor(visible.key).status, 'ready')
  } finally {
    gateway.listThreads = originalListThreads
    gateway.listMessages = originalListMessages
    resetWorkspaceState()
  }
})

test('communication notifications share one explicit browser preference', async () => {
  const [
    runtime,
    runtimeEvents,
    client,
    browserNotifications,
    calls,
    shell,
    workspace,
    messages,
    styles
  ] = await Promise.all([
    readFile(new URL('../src/state/messageRuntime.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/runtimeEvents.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/state/browserNotifications.ts', import.meta.url),
      'utf8'
    ),
    readFile(new URL('../src/state/call.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/AppShell.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/style.css', import.meta.url), 'utf8')
  ])

  assert.match(runtime, /gateway\.subscribeMessageEvents\(/)
  assert.match(client, /MESSAGE_EVENT_INACTIVITY_TIMEOUT_MS = 40_000/)
  assert.match(client, /source\.addEventListener\('heartbeat'/)
  assert.doesNotMatch(runtime, /setInterval|refreshIncomingMessage|refreshMessageWorkspace/)
  assert.match(runtime, /noteIncomingMessageArrival\(event\)/)
  assert.match(
    runtimeEvents,
    /case 'messages':[\s\S]*?refreshMessageWorkspace\([\s\S]*?visibleMessageThreadKey\(router\.currentRoute\.value\)/
  )
  const messageSubscriptionStart = client.indexOf('subscribeMessageEvents(')
  const runtimeSubscriptionStart = client.indexOf(
    'subscribeRuntimeEvents(',
    messageSubscriptionStart
  )
  const messageSubscription = client.slice(messageSubscriptionStart, runtimeSubscriptionStart)
  assert.doesNotMatch(
    messageSubscription,
    /setCursor|addEventListener\('ready'|addEventListener\('reset'/
  )
  assert.match(runtime, /showBrowserNotification\(/)
  assert.match(calls, /showBrowserNotification\(/)
  assert.match(calls, /claimIncomingCallNotification\(session, notifiedIncomingCallIDs\)/)
  assert.match(browserNotifications, /modemdeck\.browserNotifications/)
  assert.doesNotMatch(browserNotifications, /modemdeck\.messageNotifications/)
  assert.match(browserNotifications, /Notification\.requestPermission\(\)/)
  assert.match(browserNotifications, /window\.addEventListener\('storage'/)
  assert.match(shell, /@click="toggleBrowserNotifications"/)
  assert.match(shell, /t\('shell\.notificationsRequireHTTPS'\)/)
  assert.match(shell, /t\('shell\.notificationsDisable'\)/)
  assert.match(shell, /t\('shell\.notificationsEnable'\)/)
  const notificationClick = shell.indexOf('@click="toggleBrowserNotifications"')
  const notificationButton = shell.slice(
    shell.lastIndexOf('<button', notificationClick),
    shell.indexOf('</button>', notificationClick)
  )
  assert.match(notificationButton, /:disabled=/)
  assert.match(notificationButton, /!browserNotificationState\.secureContext/)
  assert.match(notificationButton, /<BellRing v-if="browserNotificationState\.active"/)
  assert.match(notificationButton, /<BellOff v-else/)
  assert.doesNotMatch(
    notificationButton,
    /v-if="[^"]*browserNotificationState\.(?:secureContext|supported)/
  )
  assert.doesNotMatch(shell, /onMounted\([^]*requestPermission/)
  const arrivalStart = workspace.indexOf('export function noteIncomingMessageArrival(')
  const refreshWorkspaceStart = workspace.indexOf(
    'export async function refreshMessageWorkspace(',
    arrivalStart
  )
  assert.match(workspace.slice(arrivalStart, refreshWorkspaceStart), /markArrival\(/)
  assert.doesNotMatch(
    workspace.slice(arrivalStart, refreshWorkspaceStart),
    /refreshThreads\(|refreshMessages\(|markThreadRead\(/
  )
  assert.match(messages, /canAcknowledgeMessageThread\(/)
  assert.match(
    messages,
    /async function openThread\([\s\S]*?loadMessages\(thread, true\)/
  )
  assert.match(messages, /document\.visibilityState === 'visible'/)
  assert.match(messages, /document\.hasFocus\(\)/)
  assert.match(messages, /viewportAtBottom\.value/)
  assert.match(messages, /recentIncomingMessageIDs\[message\.id\]/)
  assert.match(messages, /`message-row--\$\{message\.direction\}`/)
  assert.match(styles, /\.message-row--incoming\s*\{[^}]*justify-content: flex-start/s)
  assert.match(styles, /\.message-row--outgoing\s*\{[^}]*justify-content: flex-end/s)
  assert.match(messages, /prefers-reduced-motion: reduce/)
})
