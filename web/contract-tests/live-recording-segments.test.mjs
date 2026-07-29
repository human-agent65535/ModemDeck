import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('recording runtime events refresh the active call segment resources', async () => {
  const recording = await source('../src/state/recording.ts')
  const runtime = await source('../src/state/runtimeEvents.ts')

  assert.match(
    recording,
    /if \(callRecordingState\.callID\) \{[\s\S]*loadActiveCallRecordingSegments\(callRecordingState\.callID\)/
  )
  assert.match(
    recording,
    /acceptCallRecording\(callID, state\)[\s\S]*await loadActiveCallRecordingSegments\(callID\)/
  )
  assert.match(
    runtime,
    /case 'recordings':[\s\S]*refreshRecordingWorkspace\(\)/
  )
})

test('the active call surface shows each recording segment and its live duration', async () => {
  const surface = await source('../src/components/CallSurface.vue')

  assert.match(surface, /callRecordingState\.segments/)
  assert.match(surface, /segment\.segment_index/)
  assert.match(surface, /recordingSegmentDuration\(segment\)/)
  assert.match(surface, /segment\.status === 'recording'/)
  assert.match(surface, /class="call-surface__recording-segments"/)
})
