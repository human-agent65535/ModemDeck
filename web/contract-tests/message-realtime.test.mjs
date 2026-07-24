import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  incomingMessageRoute,
  shouldDisplayBrowserNotification,
  shouldRunMessageFallback
} from '../src/state/messageRuntime.ts'
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

test('browser SMS notifications require a live event, explicit grant, and background page', () => {
  assert.equal(shouldDisplayBrowserNotification(true, true, 'granted', 'hidden', true), true)
  assert.equal(shouldDisplayBrowserNotification(true, true, 'granted', 'visible', false), true)
  assert.equal(shouldDisplayBrowserNotification(true, true, 'granted', 'visible', true), false)
  assert.equal(shouldDisplayBrowserNotification(false, true, 'granted', 'hidden', false), false)
  assert.equal(shouldDisplayBrowserNotification(true, false, 'granted', 'hidden', false), false)
  assert.equal(shouldDisplayBrowserNotification(true, true, 'default', 'hidden', false), false)
})

test('notification click route preserves the exact line and peer thread identity', () => {
  assert.deepEqual(incomingMessageRoute(event), {
    name: 'messages',
    params: { threadKey: event.thread_key }
  })
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

test('message runtime owns one SSE connection and retains disconnected reconciliation', async () => {
  const runtime = await readFile(
    new URL('../src/state/messageRuntime.ts', import.meta.url),
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

  assert.match(runtime, /gateway\.subscribeMessageEvents\(/)
  assert.match(runtime, /fallbackRefreshMilliseconds = 30_000/)
  assert.match(runtime, /if \(!shouldRunMessageFallback\(state\.connected\)\) return/)
  assert.match(
    runtime,
    /refreshIncomingMessage\(event, activeThreadKey\(router\)\)/
  )
  assert.match(runtime, /Notification\.requestPermission\(\)/)
  assert.match(shell, /@click="toggleMessageNotifications"/)
  assert.match(shell, /短信通知需要 HTTPS/)
  const notificationClick = shell.indexOf('@click="toggleMessageNotifications"')
  const notificationButton = shell.slice(
    shell.lastIndexOf('<button', notificationClick),
    shell.indexOf('</button>', notificationClick)
  )
  assert.match(notificationButton, /:disabled=/)
  assert.match(notificationButton, /!messageNotificationState\.secureContext/)
  assert.doesNotMatch(
    notificationButton,
    /v-if="[^"]*messageNotificationState\.(?:secureContext|supported)/
  )
  assert.doesNotMatch(shell, /onMounted\([^]*requestPermission/)
  assert.match(workspace, /refreshThreads\(\)/)
  assert.match(workspace, /activeThreadKey !== event\.thread_key/)
  assert.match(workspace, /thread\.unread_count > 0\) await markThreadRead\(thread\)/)
  assert.match(messages, /recentIncomingMessageIDs\[message\.id\]/)
  assert.match(messages, /prefers-reduced-motion: reduce/)
})
