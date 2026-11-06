import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('runtime state applies active recording truth without another request', async () => {
  const recording = await source('../src/state/recording.ts')
  const runtime = await source('../src/state/runtimeEvents.ts')

  assert.match(
    recording,
    /export function acceptRuntimeCallRecordings[\s\S]*?acceptCallRecording\(callID, snapshot\.state\)[\s\S]*?callRecordingState\.segments = snapshot\.segments/
  )
  assert.match(
    recording,
    /const snapshot = await gateway\.getCallRecording\(normalizedCallID\)[\s\S]*acceptCallRecording\(normalizedCallID, snapshot\.state\)/
  )
  assert.match(
    runtime,
    /acceptRuntimeCallRecordings\(runtime\.recordings\)/
  )
  assert.doesNotMatch(runtime, /refreshRecordingWorkspace\(false\)/)
})

test('outgoing calls can toggle recording before answer and active calls keep recording controls without live segments', async () => {
  const [surface, dialer, history, recordings] = await Promise.all([
    source('../src/components/CallSurface.vue'),
    source('../src/components/DialerPanel.vue'),
    source('../src/components/CallHistoryListItem.vue'),
    source('../src/components/RecordingList.vue')
  ])

  assert.doesNotMatch(surface, /recordingSegments|call-surface__recording-segments|CallRecordingSegment|AudioLines/)
  assert.match(surface, /!isTestCall\.value && \(active\.value \|\| outgoingBeforeAnswer\.value\) && owned\.value && !occupied\.value/)
  assert.match(surface, /direction === 'outgoing' &&\s*callState\.pendingAction !== 'dial'/)
  assert.match(surface, /callState\.outgoingRecordingEnabled/)
  assert.match(surface, /<p v-if="recordingLabel" class="call-surface__recording" role="status">/)
  assert.match(surface, /void setCallRecordingEnabled\(!callRecordingState\.enabled\)/)
  assert.match(dialer, /dialer-recording-note/)
  assert.match(history, /AudioLines/)
  assert.match(recordings, /<RecordingPlayer/)
})
