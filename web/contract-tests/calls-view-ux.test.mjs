import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callsView = new URL('../src/views/CallsView.vue', import.meta.url)

async function source() {
  return readFile(callsView, 'utf8')
}

test('call history marks only calls with playable recordings', async () => {
  const calls = await source()

  assert.match(calls, /CassetteTape/)
  assert.match(
    calls,
    /recordingCatalogState\.data[\s\S]*?\.filter\(recording => recording\.playable\)[\s\S]*?\.map\(recording => recording\.call_id\)/
  )
  assert.match(calls, /function hasPlayableRecording\(call: CallRecord\)/)
  assert.match(
    calls,
    /v-if="hasPlayableRecording\(call\)"[\s\S]*?class="call-list-item__recording"[\s\S]*?aria-label="有通话录音"[\s\S]*?<CassetteTape/
  )
  assert.match(calls, /loadRecordingEntries\(\)/)
})

test('call detail keeps communication commands primary and contact management secondary', async () => {
  const calls = await source()

  assert.match(
    calls,
    /class="detail-header__actions call-detail__header-actions"[\s\S]*?class="call-detail__command call-detail__command--primary"[\s\S]*?@click="callBack\(selected\)"[\s\S]*?class="call-detail__command"[\s\S]*?@click="sendMessage\(selected\)"/
  )
  assert.match(
    calls,
    /<\/header>[\s\S]*?<div class="call-detail">[\s\S]*?<div class="call-detail__contact-actions">[\s\S]*?<ContactNumberActions[\s\S]*?:contact="selectedContact"[\s\S]*?<section class="detail-section detail-facts">/
  )
  assert.doesNotMatch(calls, /<div class="call-detail__actions">/)
  assert.doesNotMatch(calls, /<ContactNumberActions[\s\S]*?class="call-detail__contact-actions"/)
})
