import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import {
  answerCall,
  callState,
  dial,
  initializeCallRuntime,
  requestActiveCallRefresh,
  shutdownCallRuntime
} from '../src/state/call.ts'
import {
  callRecordingState,
  recordingSettingsState,
  refreshRecordingWorkspace,
  rememberCallRecordingPreference,
  setCallRecordingEnabled,
  syncCallRecording
} from '../src/state/recording.ts'
import { bootstrapResource } from '../src/state/workspace.ts'

function callResponse(id, lineKey, number) {
  return {
    call: {
      id,
      line_id: lineKey,
      direction: 'outgoing',
      remote_number: number,
      phase: 'dialing',
      control_state: 'owned',
      media_available: false,
      created_at: '2026-07-23T12:00:00Z'
    }
  }
}

test('real call requests keep recording preferences local to each request', async () => {
  const originalFetch = globalThis.fetch
  const bodies = []
  let failNext = false

  globalThis.fetch = async (_input, init) => {
    bodies.push(JSON.parse(String(init?.body)))
    if (failNext) {
      failNext = false
      return new Response(
        JSON.stringify({ code: 'forced_failure', message: 'forced failure' }),
        {
          status: 500,
          headers: { 'Content-Type': 'application/json' }
        }
      )
    }
    const body = bodies.at(-1)
    return new Response(
      JSON.stringify(callResponse(`call-${bodies.length}`, body.line_id, body.number)),
      {
        status: 201,
        headers: { 'Content-Type': 'application/json' }
      }
    )
  }

  try {
    await gateway.startCall('line-main', '+818000000001', true)
    await gateway.startCall('line-main', '+818000000002', false)
    failNext = true
    await assert.rejects(
      () => gateway.startCall('line-main', '+818000000003', true),
      /forced failure/
    )
    await gateway.startCall('line-main', '+818000000004')
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(
    bodies.map(body => body.recording_enabled),
    [true, false, true, undefined]
  )
  assert.equal('recording_enabled' in bodies[3], false)
})

test('dial forwards the current recording preference and keeps one call in flight', async () => {
  const originalStartCall = gateway.startCall
  const originalBootstrap = bootstrapResource.data
  const originalBootstrapStatus = bootstrapResource.status
  const originalBootstrapError = bootstrapResource.error
  const received = []

  bootstrapResource.data = {
    capabilities: {
      agent_connected: true,
      dial: true,
      message: true,
      webrtc_audio: true,
      device_control: false,
      volte_control: false,
      vowifi_control: false
    },
    lines: [
      {
        id: 'line-main',
        capabilities: {
          dial: true,
          media: true
        }
      }
    ]
  }
  bootstrapResource.status = 'ready'
  bootstrapResource.error = ''
  gateway.startCall = async (lineKey, number, recordingEnabled) => {
    received.push({ lineKey, number, recordingEnabled })
    return callResponse(`state-call-${received.length}`, lineKey, number).call
  }

  try {
    assert.equal(await dial('+818000000011', 'line-main', true), true)
    shutdownCallRuntime()
    assert.equal(await dial('+818000000012', 'line-main', false), true)
    shutdownCallRuntime()

    let releasePending
    gateway.startCall = (lineKey, number, recordingEnabled) => {
      received.push({ lineKey, number, recordingEnabled })
      return new Promise(resolve => {
        releasePending = () =>
          resolve(callResponse(`state-call-${received.length}`, lineKey, number).call)
      })
    }
    const pending = dial('+818000000013', 'line-main', true)
    assert.equal(callState.busy, true)
    assert.equal(await dial('+818000000014', 'line-main', false), false)
    assert.equal(callState.errorStatus, 409)
    assert.equal(typeof releasePending, 'function')
    releasePending()
    assert.equal(await pending, true)
  } finally {
    shutdownCallRuntime()
    gateway.startCall = originalStartCall
    bootstrapResource.data = originalBootstrap
    bootstrapResource.status = originalBootstrapStatus
    bootstrapResource.error = originalBootstrapError
    callState.session = null
  }

  assert.deepEqual(
    received.map(request => request.recordingEnabled),
    [true, false, true]
  )
})

test('a pre-connect recording choice wins over a pending default lookup', async () => {
  const originalGetRecordingSettings = gateway.getRecordingSettings
  let resolveSettings

  recordingSettingsState.status = 'idle'
  recordingSettingsState.data = null
  recordingSettingsState.error = ''
  gateway.getRecordingSettings = () =>
    new Promise(resolve => {
      resolveSettings = resolve
    })

  const session = {
    ...callResponse(
      'call-recording-preconnect',
      'line-main',
      '+818000000015'
    ).call,
    direction: 'incoming',
    phase: 'ringing',
    control_state: 'available'
  }

  try {
    syncCallRecording(session)
    assert.equal(callRecordingState.status, 'initializing')

    rememberCallRecordingPreference(session.id, true)
    assert.equal(callRecordingState.status, 'ready')
    assert.equal(callRecordingState.enabled, true)

    resolveSettings({ default_enabled: false, revision: 1 })
    await Promise.resolve()
    await Promise.resolve()

    assert.equal(callRecordingState.enabled, true)
  } finally {
    syncCallRecording(null)
    gateway.getRecordingSettings = originalGetRecordingSettings
  }
})

test('runtime recording refresh leaves an unclaimed incoming draft alone', async () => {
  const originalGetRecordingSettings = gateway.getRecordingSettings
  const originalGetCallRecording = gateway.getCallRecording
  const originalListRecordings = gateway.listRecordings
  let snapshotReads = 0
  const session = {
    ...callResponse(
      'call-recording-unclaimed-refresh',
      'line-main',
      '+818000000019'
    ).call,
    direction: 'incoming',
    phase: 'ringing',
    control_state: 'available'
  }

  recordingSettingsState.status = 'idle'
  recordingSettingsState.data = null
  recordingSettingsState.error = ''
  gateway.getRecordingSettings = async () => ({
    default_enabled: true,
    revision: 1
  })
  gateway.getCallRecording = async callID => {
    snapshotReads += 1
    return {
      state: { call_id: callID, enabled: false, status: 'off' },
      segments: []
    }
  }
  gateway.listRecordings = async () => ({
    items: [],
    meta: { limit: 50, next_cursor: '', has_more: false }
  })

  try {
    syncCallRecording(session)
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(callRecordingState.enabled, true)

    await refreshRecordingWorkspace()

    assert.equal(snapshotReads, 0)
    assert.equal(callRecordingState.enabled, true)
    assert.equal(callRecordingState.recordingStatus, 'off')
  } finally {
    syncCallRecording(null)
    gateway.getRecordingSettings = originalGetRecordingSettings
    gateway.getCallRecording = originalGetCallRecording
    gateway.listRecordings = originalListRecordings
  }
})

test('claiming an incoming call invalidates its pending default lookup', async () => {
  const originalGetRecordingSettings = gateway.getRecordingSettings
  const originalGetCallRecording = gateway.getCallRecording
  let resolveSettings
  const session = {
    ...callResponse(
      'call-recording-claim-default-race',
      'line-main',
      '+818000000020'
    ).call,
    direction: 'incoming',
    phase: 'ringing',
    control_state: 'available'
  }

  recordingSettingsState.status = 'idle'
  recordingSettingsState.data = null
  recordingSettingsState.error = ''
  gateway.getRecordingSettings = () =>
    new Promise(resolve => {
      resolveSettings = resolve
    })
  gateway.getCallRecording = async callID => ({
    state: {
      call_id: callID,
      enabled: true,
      status: 'recording',
      active_segment_id: 'segment-claimed-incoming'
    },
    segments: [{
      id: 'segment-claimed-incoming',
      call_id: callID,
      segment_index: 1,
      status: 'recording',
      recorded_at: '2026-07-23T12:00:03Z',
      started_at: '2026-07-23T12:00:03Z',
      duration_seconds: 0,
      size_bytes: 0,
      playable: false
    }]
  })

  try {
    syncCallRecording(session)
    assert.equal(callRecordingState.status, 'initializing')

    syncCallRecording({
      ...session,
      phase: 'active',
      control_state: 'owned',
      media_available: true,
      active_at: '2026-07-23T12:00:03Z'
    })
    await Promise.resolve()
    await Promise.resolve()
    assert.equal(callRecordingState.recordingStatus, 'recording')

    resolveSettings({ default_enabled: false, revision: 1 })
    await Promise.resolve()
    await Promise.resolve()

    assert.equal(callRecordingState.enabled, true)
    assert.equal(callRecordingState.recordingStatus, 'recording')
    assert.equal(callRecordingState.activeSegmentID, 'segment-claimed-incoming')
  } finally {
    syncCallRecording(null)
    gateway.getRecordingSettings = originalGetRecordingSettings
    gateway.getCallRecording = originalGetCallRecording
  }
})

test('an active transition reads authority without replaying a recording mutation', async () => {
  const originalSetCallRecording = gateway.setCallRecording
  const originalGetCallRecording = gateway.getCallRecording
  const requests = []
  const resolvers = []
  let snapshotReads = 0
  const session = callResponse(
    'call-recording-connect-race',
    'line-main',
    '+818000000016'
  ).call

  gateway.setCallRecording = (callID, enabled) => {
    requests.push({ callID, enabled })
    return new Promise(resolve => {
      resolvers.push(resolve)
    })
  }
  gateway.getCallRecording = async callID => {
    snapshotReads += 1
    const recording = snapshotReads > 1
    return {
      state: {
        call_id: callID,
        enabled: true,
        status: recording ? 'recording' : 'pending',
        ...(recording
          ? { active_segment_id: 'segment-connect-race' }
          : {})
      },
      segments: recording
        ? [{
            id: 'segment-connect-race',
            call_id: callID,
            segment_index: 1,
            status: 'recording',
            recorded_at: '2026-07-23T12:00:01Z',
            started_at: '2026-07-23T12:00:01Z',
            duration_seconds: 0,
            size_bytes: 0,
            playable: false
          }]
        : []
    }
  }

  try {
    syncCallRecording(session)
    await Promise.resolve()
    assert.equal(snapshotReads, 1)

    rememberCallRecordingPreference(session.id, true)
    const preConnectUpdate = setCallRecordingEnabled(true)
    assert.equal(callRecordingState.busy, true)
    assert.equal(requests.length, 1)

    syncCallRecording({
      ...session,
      phase: 'active',
      media_available: true,
      active_at: '2026-07-23T12:00:01Z'
    })
    assert.equal(requests.length, 1)
    assert.equal(snapshotReads, 1)

    resolvers.shift()({
      call_id: session.id,
      enabled: true,
      status: 'pending'
    })
    await preConnectUpdate
    await Promise.resolve()
    assert.equal(requests.length, 1)
    assert.equal(requests[0].enabled, true)
    assert.equal(snapshotReads, 2)

    assert.equal(callRecordingState.busy, false)
    assert.equal(callRecordingState.status, 'ready')
    assert.equal(callRecordingState.enabled, true)
    assert.equal(callRecordingState.recordingStatus, 'recording')
    assert.equal(callRecordingState.activeSegmentID, 'segment-connect-race')
  } finally {
    syncCallRecording(null)
    gateway.setCallRecording = originalSetCallRecording
    gateway.getCallRecording = originalGetCallRecording
  }
})

test('an owned recording toggle stays pessimistic and defers runtime refreshes', async () => {
  const originalSetCallRecording = gateway.setCallRecording
  const originalGetCallRecording = gateway.getCallRecording
  const originalListRecordings = gateway.listRecordings
  let snapshotReads = 0
  let releaseMutation
  const session = {
    ...callResponse(
      'call-recording-pessimistic',
      'line-main',
      '+818000000018'
    ).call,
    phase: 'active',
    media_available: true,
    active_at: '2026-07-23T12:00:01Z'
  }

  gateway.getCallRecording = async callID => {
    snapshotReads += 1
    if (snapshotReads === 1) {
      return {
        state: { call_id: callID, enabled: false, status: 'off' },
        segments: []
      }
    }
    return {
      state: {
        call_id: callID,
        enabled: true,
        status: 'recording',
        active_segment_id: 'segment-pessimistic'
      },
      segments: [{
        id: 'segment-pessimistic',
        call_id: callID,
        segment_index: 1,
        status: 'recording',
        recorded_at: '2026-07-23T12:00:02Z',
        started_at: '2026-07-23T12:00:02Z',
        duration_seconds: 0,
        size_bytes: 0,
        playable: false
      }]
    }
  }
  gateway.setCallRecording = () =>
    new Promise(resolve => {
      releaseMutation = resolve
    })
  gateway.listRecordings = async () => ({
    items: [],
    meta: { limit: 50, next_cursor: '', has_more: false }
  })

  try {
    syncCallRecording(session)
    await Promise.resolve()
    await Promise.resolve()
    assert.equal(snapshotReads, 1)
    assert.equal(callRecordingState.status, 'ready')
    assert.equal(callRecordingState.enabled, false)

    const mutation = setCallRecordingEnabled(true)
    assert.equal(callRecordingState.busy, true)
    assert.equal(callRecordingState.enabled, false)

    await refreshRecordingWorkspace()
    assert.equal(snapshotReads, 1)

    releaseMutation({
      call_id: session.id,
      enabled: true,
      status: 'pending'
    })
    await mutation
    await Promise.resolve()

    assert.equal(snapshotReads, 2)
    assert.equal(callRecordingState.busy, false)
    assert.equal(callRecordingState.enabled, true)
    assert.equal(callRecordingState.recordingStatus, 'recording')
    assert.equal(callRecordingState.activeSegmentID, 'segment-pessimistic')

    gateway.setCallRecording = async () => {
      throw new Error('forced recording failure')
    }
    const failedMutation = setCallRecordingEnabled(false)
    assert.equal(callRecordingState.enabled, true)
    await failedMutation

    assert.equal(snapshotReads, 3)
    assert.equal(callRecordingState.enabled, true)
    assert.equal(callRecordingState.recordingStatus, 'recording')
    assert.match(callRecordingState.error, /forced recording failure/)
  } finally {
    syncCallRecording(null)
    gateway.setCallRecording = originalSetCallRecording
    gateway.getCallRecording = originalGetCallRecording
    gateway.listRecordings = originalListRecordings
  }
})

test('answer sends the final pre-connect recording choice with the claim', async () => {
  const originalCallAction = gateway.callAction
  const originalRenewCallLease = gateway.renewCallLease
  const calls = []
  const session = {
    ...callResponse(
      'call-recording-incoming-answer',
      'line-main',
      '+818000000017'
    ).call,
    direction: 'incoming',
    phase: 'ringing',
    control_state: 'available'
  }

  gateway.callAction = async (callID, action, recordingEnabled) => {
    calls.push({ callID, action, recordingEnabled })
  }
  gateway.renewCallLease = async callID => ({
    call_id: callID,
    holder_id: 'fixture-browser',
    expires_at: '2026-07-23T12:01:00Z'
  })

  try {
    callState.session = session
    callState.owned = false
    callState.busy = false
    rememberCallRecordingPreference(session.id, false)

    await answerCall()

    assert.deepEqual(calls, [
      {
        callID: session.id,
        action: 'answer',
        recordingEnabled: false
      }
    ])
  } finally {
    shutdownCallRuntime()
    gateway.callAction = originalCallAction
    gateway.renewCallLease = originalRenewCallLease
  }
})

test('call reconciliation repeats when a terminal event arrives during an active read', async () => {
  const originalGetActiveCallSnapshot = gateway.getActiveCallSnapshot
  let requestCount = 0
  let resolveFirst

  gateway.getActiveCallSnapshot = () => {
    requestCount += 1
    if (requestCount === 1) {
      return new Promise(resolve => {
        resolveFirst = resolve
      })
    }
    return Promise.resolve({ calls: [], reservations: [] })
  }

  try {
    initializeCallRuntime()
    await Promise.resolve()
    assert.equal(requestCount, 1)

    const terminalRefresh = requestActiveCallRefresh()
    resolveFirst({
      calls: [
        {
          id: 'call-reconcile-1',
          line_id: 'line-main',
          direction: 'outgoing',
          remote_number: '+818000000015',
          phase: 'dialing',
          control_state: 'owned',
          media_available: false,
          created_at: '2026-07-28T09:34:07Z'
        }
      ],
      reservations: []
    })
    await terminalRefresh

    assert.equal(requestCount, 2)
    assert.equal(callState.session, null)
  } finally {
    shutdownCallRuntime()
    gateway.getActiveCallSnapshot = originalGetActiveCallSnapshot
  }
})
