import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  incomingMessageRoute,
  shouldDisplayBrowserNotification,
  shouldRunMessageFallback
} from '../src/state/messageRuntime.ts'

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
    /refreshIncomingMessage\(event, activeThreadKey\(router\), allowNotification\)/
  )
  assert.match(runtime, /Notification\.requestPermission\(\)/)
  assert.match(shell, /@click="toggleMessageNotifications"/)
  assert.match(shell, /短信通知需要 HTTPS/)
  assert.doesNotMatch(shell, /onMounted\([^]*requestPermission/)
  assert.match(workspace, /refreshThreads\(\)/)
  assert.match(workspace, /activeThreadKey !== event\.thread_key/)
  assert.match(messages, /recentIncomingMessageIDs\[message\.id\]/)
  assert.match(messages, /prefers-reduced-motion: reduce/)
})
