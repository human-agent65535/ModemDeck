import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import {
  answerCall,
  callState,
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
      JSON.stringify({ recording_enabled: body.recording_enabled ?? true, preparation_id: `prep-${bodies.length}`, media_socket_path: `/api/v1/calls/preparations/prep-${bodies.length}/media/ws`, expires_at: '2026-10-20T00:00:00Z' }),
      {
        status: 201,
        headers: { 'Content-Type': 'application/json' }
      }
    )
  }

  try {
    await gateway.prepareCall('line-main', '+818000000001', true)
    await gateway.prepareCall('line-main', '+818000000002', false)
    failNext = true
    await assert.rejects(
      () => gateway.prepareCall('line-main', '+818000000003', true),
      /forced failure/
    )
    await gateway.prepareCall('line-main', '+818000000004')
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(
    bodies.map(body => body.recording_enabled),
    [true, false, true, undefined]
  )
  assert.equal('recording_enabled' in bodies[3], false)
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
    assert.equal(callState.session?.id, 'call-reconcile-1')
    assert.equal(callState.session?.phase, 'ended')
    assert.equal(callState.owned, false)
    assert.deepEqual(callState.sessions, [])
  } finally {
    shutdownCallRuntime()
    gateway.getActiveCallSnapshot = originalGetActiveCallSnapshot
  }
})

test('real outgoing gateway uses prepare then idempotent commit and scoped lease/cancel paths', async () => {
  const originalFetch = globalThis.fetch, requests = []
  globalThis.fetch = async (input, init) => {
    const path = String(input), body = JSON.parse(String(init?.body || '{}'))
    requests.push({path,method:init?.method,body})
    if (path.endsWith('/prepare')) return new Response(JSON.stringify({recording_enabled:true,preparation_id:'prepared-call',media_socket_path:'/api/v1/calls/preparations/prepared-call/media/ws',expires_at:'2026-10-20T00:00:00Z'}),{status:201,headers:{'Content-Type':'application/json'}})
    if (path.endsWith('/commit')) return new Response(JSON.stringify(callResponse('committed-call','line-main','555')),{status:201,headers:{'Content-Type':'application/json'}})
    if (path.endsWith('/lease')) return new Response(JSON.stringify({preparation_id:'prepared-call',expires_at:'2026-10-20T00:00:00Z'}),{status:200,headers:{'Content-Type':'application/json'}})
    if (init?.method==='DELETE') return new Response(null,{status:204})
    throw new Error(`Unexpected outgoing endpoint: ${path}`)
  }
  try {
    const preparation = await gateway.prepareCall('line-main','555',true)
    assert.equal(preparation.recording_enabled,true)
    await gateway.renewCallPreparation(preparation.preparation_id)
    const first = await gateway.commitCallPreparation(preparation.preparation_id)
    const replay = await gateway.commitCallPreparation(preparation.preparation_id)
    assert.equal(first.id,replay.id)
    await gateway.cancelCallPreparation(preparation.preparation_id)
    assert.equal('startCall' in gateway,false)
    assert.deepEqual(requests.map(request=>request.method),['POST','PUT','POST','POST','DELETE'])
    assert.equal(requests[0].body.recording_enabled,true)
    assert.deepEqual(requests[2].body,{audio_running:true})
    assert.equal(requests.some(request=>request.path==='/api/v1/calls'),false)
  } finally {globalThis.fetch=originalFetch}
})
