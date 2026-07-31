import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { useListSelection } from '../src/composables/useListSelection.ts'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('list selection keeps a stable explicit set and reconciles removed rows', () => {
  const selection = useListSelection(item => item.id)
  const rows = [{ id: 'one' }, { id: 'two' }, { id: 'three' }]

  selection.enter()
  selection.selectAll(rows)
  assert.equal(selection.active.value, true)
  assert.equal(selection.count.value, 3)

  selection.toggle(rows[1])
  assert.deepEqual(Array.from(selection.keys.value), ['one', 'three'])

  selection.reconcile([{ id: 'three' }])
  assert.deepEqual(Array.from(selection.keys.value), ['three'])

  selection.exit()
  assert.equal(selection.active.value, false)
  assert.equal(selection.count.value, 0)
})

test('all communication list panes expose selection and their eligible batch actions', async () => {
  const [
    dashboard,
    messages,
    calls,
    contacts,
    recordings,
    batchBar,
    selectionToggle,
    selectableRow,
    callRow,
    messageRow,
    statusRail,
    avatarStatus
  ] = await Promise.all([
    source('../src/views/DashboardView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/components/BatchActionBar.vue'),
    source('../src/components/ListSelectionToggle.vue'),
    source('../src/components/SelectableListRow.vue'),
    source('../src/components/CallHistoryListItem.vue'),
    source('../src/components/MessageThreadListItem.vue'),
    source('../src/components/ListItemStatusRail.vue'),
    source('../src/components/ListItemAvatarStatus.vue')
  ])

  for (const view of [dashboard, messages, calls, contacts, recordings]) {
    assert.match(view, /import BatchActionBar from/)
    assert.match(view, /import ListSelectionToggle from/)
    assert.match(view, /import SelectableListRow from/)
    assert.match(view, /useListSelection</)
    assert.match(view, /@select-all="selection\.selectAll\(/)
    assert.match(view, /@done="selection\.exit"/)
    assert.match(view, /event\.key === 'Escape'/)
  }
  for (const view of [messages, calls, contacts, recordings]) {
    assert.match(
      view,
      /class="pane-search-row"[\s\S]*?<ListSelectionToggle[\s\S]*?<SearchField/
    )
    assert.match(view, /<SearchField[\s\S]*?:placeholder="t\('common\.search'\)"/)
    assert.doesNotMatch(view, /:placeholder="t\('contacts\.searchNameOrNumber'\)"/)
  }

  assert.match(messages, /const batchHasUnread = computed/)
  assert.match(messages, /@click="batchSetRead\(batchHasUnread\)"/)
  assert.match(messages, /const batchAllFavorite = computed/)
  assert.match(messages, /@click="batchSetFavorite\(!batchAllFavorite\)"/)
  assert.match(
    messages,
    /<Star[\s\S]*?:fill="batchAllFavorite \? 'currentColor' : 'none'"/
  )
  assert.match(messages, /v-if="batchThreads\.length > 0"/)
  assert.match(messages, /@click="batchDelete"/)
  assert.match(messages, /:favorite-interactive="false"/)
  assert.doesNotMatch(messages, /@favorite="toggleFavorite"/)

  assert.match(calls, /const batchMissedCalls = computed/)
  assert.match(calls, /const batchHasUnread = computed/)
  assert.match(calls, /@click="batchSetRead\(batchHasUnread\)"/)
  assert.match(calls, /const batchAllFavorite = computed/)
  assert.match(calls, /@click="batchSetFavorite\(!batchAllFavorite\)"/)
  assert.match(calls, /<FavoriteFilterButton/)
  assert.match(calls, /@click="batchDelete"/)

  assert.doesNotMatch(contacts, /batchSetRead|batchSetFavorite/)
  assert.match(contacts, /await deleteContacts\(contacts\)/)
  assert.doesNotMatch(recordings, /batchSetRead/)
  assert.match(recordings, /const batchAllFavorite = computed/)
  assert.match(recordings, /@click="batchSetFavorite\(!batchAllFavorite\)"/)
  assert.match(recordings, /await setRecordingsFavorite\(recordings, favorite\)/)
  assert.match(recordings, /<FavoriteFilterButton/)
  assert.match(
    recordings,
    /<ListItemStatusRail[\s\S]*?<template #favorite>[\s\S]*?<Star/
  )
  assert.match(recordings, /await deleteRecordings\(recordings\)/)
  assert.match(contacts, /import ListItemStatusRail from/)
  assert.match(recordings, /import ListItemStatusRail from/)
  assert.match(contacts, /import ListItemAvatarStatus from/)
  assert.match(recordings, /import ListItemAvatarStatus from/)
  assert.match(callRow, /import ListItemStatusRail from/)
  assert.match(messageRow, /import ListItemStatusRail from/)
  assert.match(callRow, /import ListItemAvatarStatus from/)
  assert.match(messageRow, /import ListItemAvatarStatus from/)
  assert.match(
    callRow,
    /<ListItemStatusRail[\s\S]*?class="call-list-item__recording"[\s\S]*?<template #favorite>[\s\S]*?<Star/
  )
  assert.match(
    messageRow,
    /<ListItemStatusRail[\s\S]*?<template #favorite>[\s\S]*?class="message-thread-favorite"/
  )
  assert.match(contacts, /<ListItemStatusRail>[\s\S]*?<template #favorite>/)
  assert.match(avatarStatus, /class="list-item-avatar-status__unread"/)
  assert.match(avatarStatus, /<UnreadDot :label="unreadLabel" compact/)
  assert.match(avatarStatus, /top: -2px;[\s\S]*?left: -2px;/)
  assert.match(statusRail, /class="list-item-status-rail"/)
  assert.match(statusRail, /<time v-if="date"/)
  assert.match(statusRail, /class="list-item-status-rail__icons"/)
  assert.match(statusRail, /<slot \/>[\s\S]*?<slot name="favorite" \/>/)
  assert.match(statusRail, /\.has-date[\s\S]*?justify-content: space-between/)
  assert.match(
    calls,
    /<header class="detail-header">[\s\S]*?class="icon-button icon-button--danger desktop-delete-action"[\s\S]*?class="icon-button call-favorite-button"[\s\S]*?<\/header>/
  )
  assert.match(
    messages,
    /<header class="conversation-header">[\s\S]*?class="icon-button icon-button--danger desktop-delete-action"[\s\S]*?class="icon-button conversation-favorite-button"[\s\S]*?<\/header>/
  )
  assert.match(
    recordings,
    /<header class="detail-header">[\s\S]*?class="icon-button icon-button--danger desktop-delete-action"[\s\S]*?class="icon-button recording-favorite-button"[\s\S]*?<\/header>/
  )
  assert.match(
    contacts,
    /<header class="detail-header">[\s\S]*?class="icon-button icon-button--danger desktop-delete-action"[\s\S]*?class="icon-button contact-favorite-button"[\s\S]*?<\/header>/
  )

  assert.match(dashboard, /const batchMessageActivities = computed/)
  assert.match(dashboard, /const batchMissedCallActivities = computed/)
  assert.match(dashboard, /@click="batchSetRead\(batchHasUnread\)"/)
  assert.match(dashboard, /const batchAllFavorite = computed/)
  assert.match(dashboard, /@click="batchSetFavorite\(!batchAllFavorite\)"/)
  assert.match(dashboard, /await Promise\.all\(\[[\s\S]*deleteMessageThreads\(threads\),[\s\S]*deleteCalls\(calls\)/)
  assert.match(dashboard, /:favorite-interactive="false"/)
  assert.match(batchBar, /<Square v-else/)
  assert.match(batchBar, /<CheckSquare2/)
  assert.match(selectionToggle, /import \{ ListChecks \}/)
  assert.match(selectionToggle, /<ListChecks :size="18"/)
  assert.doesNotMatch(selectionToggle, /<X |<CheckSquare2/)
  assert.match(
    batchBar,
    /<strong :title="selectedLabel" :aria-label="selectedLabel">[\s\S]*?\{\{ selectedLabel \}\}/
  )
  assert.match(batchBar, /text-overflow: ellipsis/)
  assert.match(
    batchBar,
    /\.batch-action-bar__select span,[\s\S]*?position: absolute;[\s\S]*?clip: rect/
  )
  assert.match(selectableRow, /border-radius: 5px/)
})

test('message conversation read and favorite state are independent and persistent in the gateway', async () => {
  const gateway = createFixtureGateway()
  const thread = (await gateway.listThreads()).items.find(item => !item.favorite)
  assert.ok(thread)

  const identity = { line_id: thread.line_id, peer: thread.peer }
  await gateway.updateMessageThreads('favorite', [identity])
  await gateway.updateMessageThreads('unread', [identity])

  let updated = (await gateway.listThreads()).items.find(item => item.key === thread.key)
  assert.equal(updated?.favorite, true)
  assert.equal(updated?.marked_unread, true)

  await gateway.updateMessageThreads('read', [identity])
  updated = (await gateway.listThreads()).items.find(item => item.key === thread.key)
  assert.equal(updated?.unread_count, 0)
  assert.equal(updated?.marked_unread, false)
  assert.equal(updated?.favorite, true)

  await gateway.updateMessageThreads('unfavorite', [identity])
  updated = (await gateway.listThreads()).items.find(item => item.key === thread.key)
  assert.equal(updated?.favorite, false)
})

test('call and recording entry favorites persist independently in the gateway', async () => {
  const gateway = createFixtureGateway()
  const call = (await gateway.listCalls()).items.find(item => !item.favorite)
  const recording = (await gateway.listRecordings()).items.find(item => !item.favorite)
  assert.ok(call)
  assert.ok(recording)

  await gateway.updateCalls('favorite', [call.id])
  await gateway.updateRecordings('favorite', [{
    call_id: recording.call_id,
    id: recording.id
  }])

  let updatedCall = (await gateway.listCalls()).items.find(item => item.id === call.id)
  let updatedRecording = (await gateway.listRecordings()).items.find(
    item => item.id === recording.id
  )
  assert.equal(updatedCall?.favorite, true)
  assert.equal(updatedRecording?.favorite, true)

  await gateway.updateCalls('unfavorite', [call.id])
  await gateway.updateRecordings('unfavorite', [{
    call_id: recording.call_id,
    id: recording.id
  }])
  updatedCall = (await gateway.listCalls()).items.find(item => item.id === call.id)
  updatedRecording = (await gateway.listRecordings()).items.find(
    item => item.id === recording.id
  )
  assert.equal(updatedCall?.favorite, false)
  assert.equal(updatedRecording?.favorite, false)
})
