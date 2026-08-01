import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('recording runtime events refresh the active call segment resources', async () => {
  const recording = await source('../src/state/recording.ts')
  const runtime = await source('../src/state/runtimeEvents.ts')

  assert.match(
    recording,
    /if \(callRecordingState\.callID\) \{[\s\S]*requestActiveCallRecordingRefresh\(\)/
  )
  assert.match(
    recording,
    /const snapshot = await gateway\.getCallRecording\(normalizedCallID\)[\s\S]*acceptCallRecording\(normalizedCallID, snapshot\.state\)/
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
  assert.match(surface, /isActiveRecordingSegment\(segment\)/)
  assert.match(surface, /callRecordingState\.recordingStatus === 'recording'/)
  assert.match(surface, /t\('calls\.recordingPending'\)/)
  assert.match(surface, /t\('calls\.recordingFailed'\)/)
  assert.match(surface, /class="call-surface__recording-segments"/)
})
