import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callsView = new URL('../src/views/CallsView.vue', import.meta.url)
const callHistoryListItem = new URL(
  '../src/components/CallHistoryListItem.vue',
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
  assert.match(row, /<BaseAvatar :name="name" :src="avatar"/)
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

test('call detail keeps communication and contact actions in one compact header', async () => {
  const calls = await source()

  assert.match(
    calls,
    /class="detail-header__actions call-detail__header-actions"[\s\S]*?class="call-detail__command call-detail__command--primary"[\s\S]*?@click="callBack\(selected\)"[\s\S]*?class="call-detail__command"[\s\S]*?@click="sendMessage\(selected\)"/
  )
  assert.match(
    calls,
    /<ContactHeaderIdentity[\s\S]*?:number="callDisplayNumber\(selected\)"[\s\S]*?\/>[\s\S]*?<div class="detail-header__actions call-detail__header-actions">[\s\S]*?<ContactNumberActions[\s\S]*?:contact="selectedContact"[\s\S]*?compact/
  )
  assert.doesNotMatch(calls, /<template #actions>/)
  assert.doesNotMatch(calls, /<div class="call-detail__actions">/)
  assert.doesNotMatch(calls, /class="call-detail__contact-actions"/)
  assert.match(
    calls,
    /\.calls-workspace \.detail-header\s*\{[^}]*container-type: inline-size;[^}]*\}[\s\S]*?@container \(max-width: 760px\)[\s\S]*?\.call-detail__command span\s*\{[^}]*display: none;/s
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
