import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callsView = new URL('../src/views/CallsView.vue', import.meta.url)

async function source() {
  return readFile(callsView, 'utf8')
}

test('call history marks only calls with playable recordings', async () => {
  const calls = await source()

  assert.match(calls, /<BaseAvatar :name="displayName\(call\)" :src="avatarForCall\(call\)"/)
  assert.match(calls, /class="call-list-item__avatar"/)
  assert.match(calls, /CassetteTape/)
  assert.match(
    calls,
    /recordingCatalogState\.data[\s\S]*?\.filter\(recording => recording\.playable\)[\s\S]*?\.map\(recording => recording\.call_id\)/
  )
  assert.match(calls, /function hasPlayableRecording\(call: CallRecord\)/)
  assert.match(
    calls,
    /v-if="hasPlayableRecording\(call\)"[\s\S]*?class="call-list-item__recording"[\s\S]*?:aria-label="t\('calls\.hasRecording'\)"[\s\S]*?<CassetteTape/
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
    /<ContactHeaderIdentity[\s\S]*?:number="selected\.remote_number"[\s\S]*?<div class="detail-header__actions call-detail__header-actions">[\s\S]*?<ContactNumberActions[\s\S]*?:contact="selectedContact"[\s\S]*?compact/
  )
  assert.doesNotMatch(calls, /<div class="call-detail__actions">/)
  assert.doesNotMatch(calls, /class="call-detail__contact-actions"/)
})
