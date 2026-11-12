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
    selectionIndicator,
    callRow,
    messageRow,
    statusRail,
    avatarStatus,
    styles,
    listSelection
  ] = await Promise.all([
    source('../src/views/DashboardView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/views/CallsView.vue'),
    source('../src/views/ContactsView.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/components/BatchActionBar.vue'),
    source('../src/components/ListSelectionToggle.vue'),
    source('../src/components/SelectableListRow.vue'),
    source('../src/components/SelectionCheckIndicator.vue'),
    source('../src/components/CallHistoryListItem.vue'),
    source('../src/components/MessageThreadListItem.vue'),
    source('../src/components/ListItemStatusRail.vue'),
    source('../src/components/ListItemAvatarStatus.vue'),
    source('../src/style.css'),
    source('../src/composables/useListSelection.ts')
  ])

  for (const view of [dashboard, messages, calls, contacts, recordings]) {
    assert.match(view, /import BatchActionBar from/)
    assert.match(view, /import ListSelectionToggle from/)
    assert.match(view, /import SelectableListRow from/)
    assert.match(view, /useListSelection</)
    assert.match(view, /@select-all="selection\.selectAll\(/)
    assert.match(view, /@done="selection\.exit"/)
  }
  // Escape is wired once, by useListSelection, for every view above.
  assert.match(listSelection, /event\.key === 'Escape' && active\.value/)
  for (const view of [messages, calls, contacts, recordings]) {
    assert.match(
      view,
      /<CommunicationListToolbar[\s\S]*?<template #primary>[\s\S]*?<ListSelectionToggle[\s\S]*?<SearchField/
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
  for (const view of [calls, messages, recordings, contacts]) {
    assert.match(view, /import WorkspaceDetailHeader from/)
    assert.match(view, /import WorkspaceDetailActions from/)
    assert.match(view, /import FavoriteActionButton from/)
    assert.match(view, /<WorkspaceDetailHeader[\s\S]*?<template[^>]*#actions>/)
    assert.match(view, /<WorkspaceDetailActions>/)
    assert.match(view, /class="icon-button icon-button--danger[^"]*"/)
    assert.match(view, /<span class="workspace-detail-action__label">\{\{ t\('[a-z]+\.delete'\) \}\}<\/span>/)
    assert.match(view, /<template[^>]*#trailing>\s*<FavoriteActionButton/)
  }

  assert.match(dashboard, /const batchMessageActivities = computed/)
  assert.match(dashboard, /const batchMissedCallActivities = computed/)
  assert.match(dashboard, /@click="batchSetRead\(batchHasUnread\)"/)
  assert.match(dashboard, /const batchAllFavorite = computed/)
  assert.match(dashboard, /@click="batchSetFavorite\(!batchAllFavorite\)"/)
  assert.match(dashboard, /await Promise\.allSettled\(\[\s*deleteMessageThreads\(threads\),\s*deleteCalls\(calls\)/)
  assert.match(dashboard, /t\('dashboard\.deleteSelectedThreads', \{ count: threads\.length \}, threads\.length\)/)
  assert.match(dashboard, /t\('dashboard\.deleteSelectedCalls', \{ count: calls\.length \}, calls\.length\)/)
  assert.match(dashboard, /'dashboard\.deleteThreadsFailed'/)
  assert.match(dashboard, /'dashboard\.deleteCallsFailed'/)
  assert.match(dashboard, /if \(!failures\.length\) selection\.exit\(\)/)
  assert.match(dashboard, /:favorite-interactive="false"/)
  assert.match(batchBar, /import SelectionCheckIndicator from/)
  assert.match(
    batchBar,
    /<SelectionCheckIndicator[\s\S]*:checked="selected === total && total > 0"/
  )
  assert.match(selectableRow, /import SelectionCheckIndicator from/)
  assert.match(selectableRow, /<SelectionCheckIndicator[\s\S]*:checked="selected"/)
  assert.match(selectionIndicator, /width: 22px;/)
  assert.match(selectionIndicator, /height: 22px;/)
  assert.match(selectionIndicator, /border-radius: var\(--radius-xs\);/)
  assert.match(selectionToggle, /import \{ ListChecks \}/)
  assert.match(selectionToggle, /<ListChecks :size="18"/)
  assert.doesNotMatch(selectionToggle, /<X |<CheckSquare2/)
  assert.match(
    batchBar,
    /<strong :title="selectedLabel" :aria-label="selectedLabel">[\s\S]*?\{\{ selectedLabel \}\}/
  )
  assert.match(batchBar, /text-overflow: ellipsis/)
  assert.match(batchBar, /--batch-action-bar-height: 52px;/)
  assert.match(batchBar, /max-height: var\(--batch-action-bar-height\);/)
  assert.doesNotMatch(batchBar, /\.batch-action-bar\s*\{[^}]*position: fixed;/)
  assert.doesNotMatch(styles, /\.workspace\.is-batch-selecting \.item-list\s*\{/)
  assert.match(
    batchBar,
    /\.batch-action-bar__select-label,[\s\S]*?position: absolute;[\s\S]*?clip: rect/
  )
  assert.doesNotMatch(batchBar, /\.batch-action-bar__select span/)
  assert.doesNotMatch(selectableRow, /border-radius: 5px/)
})

// The body of `async function name(...) { ... }`, found by matching braces.
function functionBody(text, name) {
  const start = text.indexOf(`async function ${name}(`)
  assert.ok(start >= 0, `${name} exists`)
  const open = text.indexOf('{', text.indexOf(')', start))
  let depth = 0
  for (let index = open; index < text.length; index += 1) {
    if (text[index] === '{') depth += 1
    if (text[index] === '}' && --depth === 0) return text.slice(open + 1, index)
  }
  assert.fail(`${name} has a closing brace`)
}

test('every batch delete asks for confirmation and stops on Cancel before deleting', async () => {
  const views = {
    Messages: ['../src/views/MessagesView.vue', ['deleteMessageThreads']],
    Calls: ['../src/views/CallsView.vue', ['deleteCalls']],
    Recordings: ['../src/views/RecordingsView.vue', ['deleteRecordings']],
    Dashboard: ['../src/views/DashboardView.vue', ['deleteMessageThreads', 'deleteCalls']],
    Contacts: ['../src/views/ContactsView.vue', ['deleteContacts']]
  }
  for (const [name, [path, deletes]] of Object.entries(views)) {
    const view = await source(path)
    assert.match(view, /import \{ requestConfirmation \} from '\.\.\/state\/confirmation'/, name)
    assert.match(view, /@click="batchDelete"/, name)
    const body = functionBody(view, 'batchDelete')
    const ask = body.search(/const confirmed = await requestConfirmation\(\{/)
    const stop = body.search(/if \(!confirmed\) return\b/)
    assert.ok(ask >= 0, `${name} batch delete asks first`)
    assert.ok(stop > ask, `${name} batch delete stops when the answer is Cancel`)
    assert.match(body.slice(ask, stop), /tone: 'danger'/, `${name} confirmation is destructive`)
    // Nothing is removed (state or server) before the answer.
    assert.doesNotMatch(body.slice(0, stop), /\bdelete[A-Z]\w*\(|gateway\./, `${name} deletes only after confirming`)
    for (const call of deletes) {
      const at = body.search(new RegExp(`\\b${call}\\(`))
      assert.ok(at > stop, `${name} calls ${call} after confirmation`)
    }
  }
})

test('Messages batch delete deletes nothing on Cancel and the selection on Delete', async t => {
  const { gateway } = await import('../src/api/client.ts')
  const { answerConfirmation, confirmationState } = await import('../src/state/confirmation.ts')
  const { flushView, mountMessagesView } = await import('./helpers/messages-view.mjs')
  const view = await mountMessagesView(t)
  const deletions = []
  const original = gateway.updateMessageThreads
  gateway.updateMessageThreads = async (action, queries) => {
    deletions.push([action, queries.map(query => query.peer)])
  }
  t.after(() => {
    gateway.updateMessageThreads = original
    while (confirmationState.request) answerConfirmation(false)
  })

  view.state.selection.enter()
  view.state.selection.toggle(view.threads[1])
  const cancelled = view.state.batchDelete()
  await flushView()
  assert.equal(confirmationState.request?.tone, 'danger')
  assert.deepEqual(deletions, [])
  answerConfirmation(false)
  await cancelled
  assert.deepEqual(deletions, [])
  assert.equal(view.state.selection.count.value, 1)

  const confirmed = view.state.batchDelete()
  await flushView()
  answerConfirmation(true)
  await confirmed
  assert.deepEqual(deletions, [['delete', [view.threads[1].peer]]])
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
