import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [dashboard, messages, calls, workspace] = await Promise.all([
  readFile(new URL('../src/views/DashboardView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/CallsView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8')
])

test('dashboard notification cards activate their corresponding filters', () => {
  assert.match(
    dashboard,
    /name: 'messages', query: \{ filter: 'unread' \}/
  )
  assert.match(
    dashboard,
    /name: 'calls', query: \{ filter: 'missed' \}/
  )
  assert.match(
    dashboard,
    /callsResource\.data\.filter\(call => call\.missed && !call\.read\)\.length/
  )
})

test('message list exposes all, unread, and read route-backed filters', () => {
  assert.match(messages, /type MessageReadFilter = 'all' \| 'unread' \| 'read'/)
  assert.match(messages, /value: 'unread', label: t\('messages\.unread'\)/)
  assert.match(messages, /value: 'read', label: t\('messages\.read'\)/)
  assert.match(messages, /thread\.unread_count <= 0/)
  assert.match(messages, /thread\.unread_count > 0/)
  assert.match(messages, /\(\) => route\.query\.filter/)
  assert.match(messages, /@click="setMessageFilter\(item\.value\)"/)
})

test('missed call filter acknowledges unread missed calls persistently', () => {
  assert.match(calls, /\(\) => route\.query\.filter/)
  assert.match(calls, /activeFilter === 'missed'/)
  assert.match(calls, /await markMissedCallsRead\(\)/)
  assert.match(calls, /@click="setFilter\(item\.value\)"/)
  assert.match(workspace, /gateway[\s\S]*?\.markMissedCallsRead\(\)/)
  assert.match(workspace, /if \(call\.missed\) call\.read = true/)
})
