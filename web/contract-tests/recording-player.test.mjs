import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('recording playback uses one branded accessible player', async () => {
  const [player, recordings, callRecordings] = await Promise.all([
    source('../src/components/RecordingPlayer.vue'),
    source('../src/views/RecordingsView.vue'),
    source('../src/components/RecordingList.vue')
  ])

  assert.match(player, /<audio[\s\S]*?preload="metadata"/)
  assert.doesNotMatch(player, /<audio[\s\S]*?\scontrols(?:\s|>)/)
  assert.match(player, /class="recording-audio-player__toggle"/)
  assert.match(player, /type="range"[\s\S]*?:aria-label="t\('recordings\.seek'\)"/)
  assert.match(player, /applySelectedAudioOutput\(element\)/)
  assert.match(player, /audioState\.recordingPlaybackVolume/)
  assert.match(recordings, /<RecordingPlayer[\s\S]*?:src="selected\.download_url"/)
  assert.match(callRecordings, /<RecordingPlayer[\s\S]*?compact/)
})

test('visible recording surfaces do not expose browser-native audio controls', async () => {
  const files = await Promise.all([
    source('../src/views/RecordingsView.vue'),
    source('../src/components/RecordingList.vue')
  ])

  for (const contents of files) {
    assert.doesNotMatch(contents, /<audio\b/)
    assert.doesNotMatch(contents, /\scontrols(?:\s|>)/)
  }
})

test('recording semantics reserve waveform for recordings and record controls for actions', async () => {
  const [history, surface, dialer] = await Promise.all([
    source('../src/components/CallHistoryListItem.vue'),
    source('../src/components/CallSurface.vue'),
    source('../src/components/DialerPanel.vue')
  ])

  assert.match(history, /AudioLines/)
  assert.match(surface, /AudioLines/)
  assert.match(surface, /Circle/)
  assert.match(surface, /Square/)
  assert.match(dialer, /Circle/)
  for (const contents of [history, surface, dialer]) {
    assert.doesNotMatch(contents, /CassetteTape/)
  }
})
