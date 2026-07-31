import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callsView = new URL('../src/views/CallsView.vue', import.meta.url)
const callHistoryListItem = new URL(
  '../src/components/CallHistoryListItem.vue',
  import.meta.url
)
const workspaceDetailHeader = new URL(
  '../src/components/workspace/WorkspaceDetailHeader.vue',
  import.meta.url
)

async function source() {
  return readFile(callsView, 'utf8')
}

test('call history marks only calls with playable recordings', async () => {
  const [calls, row] = await Promise.all([
    source(),
    readFile(callHistoryListItem, 'utf8')
  ])

  assert.match(calls, /import CallHistoryListItem from/)
  assert.match(
    row,
    /<CommunicationAvatar[\s\S]*channel="call"[\s\S]*:address="number"[\s\S]*:src="avatar"/
  )
  assert.match(row, /class="call-list-item__avatar"/)
  assert.match(row, /CassetteTape/)
  assert.match(
    calls,
    /recordingCatalogState\.data[\s\S]*?\.filter\(recording => recording\.playable\)[\s\S]*?\.map\(recording => recording\.call_id\)/
  )
  assert.match(calls, /function hasPlayableRecording\(call: CallRecord\)/)
  assert.match(
    calls,
    /<CallHistoryListItem[\s\S]*?:has-recording="hasPlayableRecording\(call\)"/
  )
  assert.match(
    row,
    /v-if="hasRecording"[\s\S]*?class="call-list-item__recording"[\s\S]*?:aria-label="t\('calls\.hasRecording'\)"[\s\S]*?<CassetteTape/
  )
  assert.match(calls, /loadRecordingEntries\(\)/)
})

test('call detail keeps communication and contact actions in one shared compact header', async () => {
  const [calls, detailHeader, detailActions] = await Promise.all([
    source(),
    readFile(workspaceDetailHeader, 'utf8'),
    readFile(
      new URL('../src/components/workspace/WorkspaceDetailActions.vue', import.meta.url),
      'utf8'
    )
  ])

  assert.match(
    calls,
    /<WorkspaceDetailHeader>[\s\S]*?<template #actions>[\s\S]*?class="workspace-detail-command workspace-detail-command--primary"[\s\S]*?@click="callBack\(selected\)"[\s\S]*?class="workspace-detail-command"[\s\S]*?@click="sendMessage\(selected\)"/
  )
  assert.match(
    calls,
    /<template #identity>[\s\S]*?<ContactHeaderIdentity[\s\S]*?:number="callDisplayNumber\(selected\)"[\s\S]*?\/>[\s\S]*?<template #actions>[\s\S]*?<ContactNumberActions[\s\S]*?:contact="selectedContact"[\s\S]*?compact/
  )
  assert.match(calls, /<FavoriteActionButton/)
  assert.doesNotMatch(calls, /<div class="call-detail__actions">/)
  assert.doesNotMatch(calls, /class="call-detail__contact-actions"/)
  assert.doesNotMatch(calls, /\.call-detail__command/)
  assert.match(
    detailActions,
    /@container \(max-width: 760px\)[\s\S]*?\.workspace-detail-command span\s*\{[^}]*display: none;/s
  )
  assert.match(detailHeader, /container-type: inline-size;/)
  assert.match(
    detailHeader,
    /\.workspace-detail-header__actions \{[\s\S]*gap: var\(--detail-action-gap\);/
  )
})

test('outgoing call detail uses call-again copy', async () => {
  const calls = await source()

  assert.match(
    calls,
    /function callActionLabel\(call: CallRecord\)[\s\S]*?call\.direction === 'outgoing'[\s\S]*?t\('calls\.callAgain'\)/
  )
  assert.match(calls, /:title="dialUnavailable \|\| callActionLabel\(selected\)"/)
  assert.match(calls, /<span>\{\{ callActionLabel\(selected\) \}\}<\/span>/)
})
