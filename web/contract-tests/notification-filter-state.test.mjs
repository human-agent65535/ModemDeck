import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [
  dashboard,
  messages,
  calls,
  callRow,
  messageRow,
  unreadDot,
  workspace,
  chinese,
  english
] = await Promise.all([
  readFile(new URL('../src/views/DashboardView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/CallsView.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/components/CallHistoryListItem.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/components/MessageThreadListItem.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/components/UnreadDot.vue', import.meta.url), 'utf8'),
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
  assert.match(messages, /!threadIsUnread\(thread\)/)
  assert.match(messages, /threadIsUnread\(thread\)/)
  assert.match(messages, /\(\) => route\.query\.filter/)
  assert.match(messages, /@click="setMessageFilter\(item\.value\)"/)
  assert.match(messages, /favoriteOnly\.value && !thread\.favorite/)
  assert.match(messages, /<FavoriteFilterButton/)
  assert.match(messages, /@toggle="setFavoriteFilter\(!favoriteOnly\)"/)
})

test('read threads remain in the active unread view until its filter changes', () => {
  assert.match(messages, /const retainedUnreadThreadKeys = ref\(new Set<string>\(\)\)/)
  assert.match(
    messages,
    /messageFilter\.value === 'unread'[\s\S]*?!threadIsUnread\(thread\)[\s\S]*?!retainedUnreadThreadKeys\.value\.has\(thread\.key\)/
  )
  assert.match(
    messages,
    /function markThreadReadInView[\s\S]*?messageFilter\.value === 'unread'[\s\S]*?retainedUnreadThreadKeys\.value\.add\(thread\.key\)[\s\S]*?return markThreadRead\(thread\)/
  )
  assert.match(
    messages,
    /watch\(\[messageFilter, favoriteOnly\],[\s\S]*?retainedUnreadThreadKeys\.value\.clear\(\)/
  )
  assert.match(messages, /await markThreadReadInView\(current\)/)
  assert.match(messages, /@read="toggleThreadRead\(thread\)"/)
})

test('missed call filter acknowledges unread missed calls persistently', () => {
  assert.match(calls, /\(\) => route\.query\.filter/)
  assert.match(calls, /activeFilter === 'missed'/)
  assert.match(calls, /await updateMissedCallsReadState\(/)
  assert.match(calls, /@click="setFilter\(item\.value\)"/)
  assert.match(workspace, /gateway\.updateCalls\(read \? 'read' : 'unread', ids\)/)
  assert.match(workspace, /if \(selected\.has\(call\.id\) && call\.missed\) call\.read = read/)
})

test('unread missed calls have a visible and accessible unread mark', () => {
  assert.match(callRow, /'is-unread': call\.missed && !call\.read/)
  assert.match(callRow, /t\('calls\.viewUnreadDetails', \{ name \}\)/)
  assert.match(
    callRow,
    /<ListItemAvatarStatus[\s\S]*?:unread-label="[\s\S]*?call\.missed && !call\.read[\s\S]*?t\('calls\.unreadMissed'\)/
  )
  assert.match(
    callRow,
    /<ListItemStatusRail[\s\S]*?<Star[\s\S]*?class="call-list-item__recording"/
  )
  assert.match(
    messageRow,
    /<ListItemAvatarStatus[\s\S]*?:unread-label="[\s\S]*?thread\.unread_count > 0[\s\S]*?t\('messages\.unreadCount'/
  )
  assert.match(unreadDot, /background: var\(--accent\)/)
  assert.match(unreadDot, /0 0 0 1px rgb\(12 98 79 \/ 52%\)/)
})
