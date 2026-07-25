import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  incomingMessageRoute,
  shouldRunMessageFallback
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
import {
  messageResources,
  messagesFor,
  refreshIncomingMessage,
  threadsResource
} from '../src/state/workspace.ts'

const event = {
  id: 7,
  event_key: 'sms:42',
  message_id: '42',
  thread_key: '8986010000000000001|+818012345678',
  line_id: 'line-main',
  iccid: '8986010000000000001',
  peer: '+818012345678',
  content: 'hello',
  timestamp: '2026-07-24T07:30:00Z'
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
  assert.deepEqual(incomingMessageRoute(event), {
    name: 'messages',
    params: { threadKey: event.thread_key }
  })
})

test('incoming call notification is claimed once for one genuinely ringing call', () => {
  const claimed = new Set()
  const incomingCall = {
    id: 'call-incoming-1',
    line_key: 'line-main',
    direction: 'incoming',
    remote_number: '+818012345678',
    phase: 'ringing',
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
          line_key: 'line-main',
          direction: 'incoming',
          remote_number: '+818012345678',
          phase: 'ringing',
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

test('message reconciliation polling only runs while SSE is disconnected', () => {
  assert.equal(shouldRunMessageFallback(false), true)
  assert.equal(shouldRunMessageFallback(true), false)
})

test('active-thread SMS invalidation refreshes messages and persists the read state', async () => {
  const key = event.thread_key
  const initialThread = {
    key,
    imsi: 'imsi-main',
    iccid: event.iccid,
    line_id: event.line_id,
    peer: event.peer,
    last_timestamp: '2026-07-24T07:00:00Z',
    last_content: 'before',
    unread_count: 0
  }
  const incomingThread = {
    ...initialThread,
    last_timestamp: event.timestamp,
    last_content: event.content,
    unread_count: 1
  }
  const initialMessage = {
    id: '41',
    iccid: event.iccid,
    peer: event.peer,
    direction: 'incoming',
    state: 'received',
    content: 'before',
    timestamp: initialThread.last_timestamp
  }
  const incomingMessage = {
    ...initialMessage,
    id: event.message_id,
    content: event.content,
    timestamp: event.timestamp
  }
  const previousThreads = {
    status: threadsResource.status,
    data: threadsResource.data,
    error: threadsResource.error
  }
  const hadMessages = Object.hasOwn(messageResources, key)
  const previousMessages = hadMessages
    ? {
        status: messageResources[key].status,
        data: messageResources[key].data,
        error: messageResources[key].error
      }
    : undefined
  const originalListThreads = gateway.listThreads
  const originalListMessages = gateway.listMessages
  const originalMarkThreadRead = gateway.markThreadRead
  const reads = []

  threadsResource.status = 'ready'
  threadsResource.data = [initialThread]
  threadsResource.error = ''
  const messages = messagesFor(key)
  messages.status = 'ready'
  messages.data = [initialMessage]
  messages.error = ''
  gateway.listThreads = async () => [incomingThread]
  gateway.listMessages = async () => [initialMessage, incomingMessage]
  gateway.markThreadRead = async input => {
    reads.push(input)
  }

  try {
    await refreshIncomingMessage(event, key, false)

    assert.deepEqual(reads, [{ iccid: event.iccid, peer: event.peer }])
    assert.deepEqual(messagesFor(key).data.map(message => message.id), ['41', event.message_id])
    assert.equal(threadsResource.data[0].unread_count, 0)
  } finally {
    gateway.listThreads = originalListThreads
    gateway.listMessages = originalListMessages
    gateway.markThreadRead = originalMarkThreadRead
    threadsResource.status = previousThreads.status
    threadsResource.data = previousThreads.data
    threadsResource.error = previousThreads.error
    if (previousMessages) {
      messageResources[key].status = previousMessages.status
      messageResources[key].data = previousMessages.data
      messageResources[key].error = previousMessages.error
    } else {
      delete messageResources[key]
    }
  }
})

test('communication notifications share one explicit browser preference', async () => {
  const runtime = await readFile(
    new URL('../src/state/messageRuntime.ts', import.meta.url),
    'utf8'
  )
  const browserNotifications = await readFile(
    new URL('../src/state/browserNotifications.ts', import.meta.url),
    'utf8'
  )
  const calls = await readFile(
    new URL('../src/state/call.ts', import.meta.url),
    'utf8'
  )
  const shell = await readFile(
    new URL('../src/components/AppShell.vue', import.meta.url),
    'utf8'
  )
  const workspace = await readFile(
    new URL('../src/state/workspace.ts', import.meta.url),
    'utf8'
  )
  const messages = await readFile(
    new URL('../src/views/MessagesView.vue', import.meta.url),
    'utf8'
  )
  const styles = await readFile(new URL('../src/style.css', import.meta.url), 'utf8')

  assert.match(runtime, /gateway\.subscribeMessageEvents\(/)
  assert.match(runtime, /fallbackRefreshMilliseconds = 30_000/)
  assert.match(runtime, /if \(!shouldRunMessageFallback\(state\.connected\)\) return/)
  assert.match(
    runtime,
    /refreshIncomingMessage\(event, activeThreadKey\(router\)\)/
  )
  assert.match(runtime, /showBrowserNotification\(/)
  assert.match(calls, /showBrowserNotification\(/)
  assert.match(calls, /claimIncomingCallNotification\(session, notifiedIncomingCallIDs\)/)
  assert.match(browserNotifications, /modemdeck\.browserNotifications/)
  assert.doesNotMatch(browserNotifications, /modemdeck\.messageNotifications/)
  assert.match(browserNotifications, /Notification\.requestPermission\(\)/)
  assert.match(browserNotifications, /window\.addEventListener\('storage'/)
  assert.match(shell, /@click="toggleBrowserNotifications"/)
  assert.match(shell, /浏览器通知需要 HTTPS/)
  assert.match(shell, /关闭短信与来电通知/)
  assert.match(shell, /启用短信与来电通知/)
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
  assert.match(workspace, /refreshThreads\(\)/)
  assert.match(workspace, /activeThreadKey !== event\.thread_key/)
  assert.match(workspace, /thread\.unread_count > 0\) await markThreadRead\(thread\)/)
  assert.match(messages, /recentIncomingMessageIDs\[message\.id\]/)
  assert.match(messages, /`message-row--\$\{message\.direction\}`/)
  assert.match(styles, /\.message-row--incoming\s*\{[^}]*justify-content: flex-start/s)
  assert.match(styles, /\.message-row--outgoing\s*\{[^}]*justify-content: flex-end/s)
  assert.match(messages, /prefers-reduced-motion: reduce/)
})
