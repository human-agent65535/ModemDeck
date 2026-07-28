import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [dashboard, messages, calls, callRow, workspace, chinese, english] = await Promise.all([
  readFile(new URL('../src/views/DashboardView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/CallsView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/components/CallHistoryListItem.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8'),
  readFile(new URL('../src/i18n/locales/zh-CN.ts', import.meta.url), 'utf8'),
  readFile(new URL('../src/i18n/locales/en-US.ts', import.meta.url), 'utf8')
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
  assert.match(chinese, /missedCalls: '新未接来电'/)
  assert.match(english, /missedCalls: 'New missed calls'/)
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

test('unread missed calls have a visible and accessible unread mark', () => {
  assert.match(callRow, /'is-unread': call\.missed && !call\.read/)
  assert.match(callRow, /t\('calls\.viewUnreadDetails', \{ name \}\)/)
  assert.match(
    callRow,
    /v-if="call\.missed && !call\.read"[\s\S]*?class="call-list-item__unread"[\s\S]*?t\('calls\.unread'\)/
  )
})
