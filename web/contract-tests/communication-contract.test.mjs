import assert from 'node:assert/strict'
import test from 'node:test'
import {
  callActionPath,
  callActionContract,
  callMediaContract,
  callMediaPath,
  callRecordingContract,
  callRecordingPath,
  callRecordingsPath,
  communicationContracts,
  communicationPaths,
  createCallActionPayload,
  createCallMediaPayload,
  createCallPayload,
  createCallRecordingPayload,
  createDTMFPayload,
  createMessagePayload,
  createMessageReadPayload,
  createRecordingSettingsPayload,
  createTelegramUnitPayload,
  parseActiveCallsResponse,
  parseCallMediaResponse,
  parseCallRecordingState,
  parseCallRecordingsResponse,
  parseCallResponse,
  parseMessageResponse,
  parseRecordingEntriesResponse,
  parseRecordingSettingsResponse,
  parseTelegramUnitResponse,
  parseTelegramUnitsResponse,
  telegramUnitContract,
  telegramUnitDeletePath,
  telegramUnitPath
} from '../src/api/contract.ts'

const canonicalCall = {
  id: 'call-1',
  line_id: 'line-main',
  direction: 'outgoing',
  remote_number: '+818012345678',
  phase: 'active',
  media_available: true,
  created_at: '2026-07-23T12:00:00Z',
  active_at: '2026-07-23T12:00:03Z',
  bearer: 'VoLTE'
}

test('communication and Telegram endpoints match the root API', () => {
  assert.deepEqual(communicationPaths, {
    messages: '/api/v1/messages',
    messageRead: '/api/v1/messages/read',
    calls: '/api/v1/calls',
    missedCallsRead: '/api/v1/calls/missed/read',
    activeCalls: '/api/v1/calls/active',
    recordings: '/api/v1/recordings',
    callSettings: '/api/v1/settings/calls',
    recordingSettings: '/api/v1/settings/recording',
    telegram: '/api/v1/settings/telegram'
  })
  assert.equal(callActionPath('call / 1', 'answer'), '/api/v1/calls/call%20%2F%201/answer')
  assert.equal(callActionPath('call-1', 'dtmf'), '/api/v1/calls/call-1/dtmf')
  assert.equal(callMediaPath('call / 1'), '/api/v1/calls/call%20%2F%201/media')
  assert.equal(
    callRecordingPath('call / 1'),
    '/api/v1/calls/call%20%2F%201/recording'
  )
  assert.equal(
    callRecordingsPath('call / 1'),
    '/api/v1/calls/call%20%2F%201/recordings'
  )
  assert.deepEqual(communicationContracts.sendMessage, {
    method: 'POST',
    path: '/api/v1/messages',
    successStatus: 201
  })
  assert.deepEqual(communicationContracts.markMessageRead, {
    method: 'PATCH',
    path: '/api/v1/messages/read',
    successStatus: 204
  })
  assert.deepEqual(communicationContracts.startCall, {
    method: 'POST',
    path: '/api/v1/calls',
    successStatus: 201
  })
  assert.deepEqual(communicationContracts.markMissedCallsRead, {
    method: 'PATCH',
    path: '/api/v1/calls/missed/read',
    successStatus: 204
  })
  assert.deepEqual(communicationContracts.activeCalls, {
    method: 'GET',
    path: '/api/v1/calls/active',
    successStatus: 200
  })
  assert.deepEqual(communicationContracts.listRecordings, {
    method: 'GET',
    path: '/api/v1/recordings',
    successStatus: 200
  })
  assert.deepEqual(communicationContracts.getRecordingSettings, {
    method: 'GET',
    path: '/api/v1/settings/recording',
    successStatus: 200
  })
  assert.deepEqual(communicationContracts.updateRecordingSettings, {
    method: 'PUT',
    path: '/api/v1/settings/recording',
    successStatus: 200
  })
  assert.deepEqual(callActionContract('call-1', 'reject'), {
    method: 'POST',
    path: '/api/v1/calls/call-1/reject',
    successStatus: 200
  })
  assert.deepEqual(callMediaContract('call-1'), {
    method: 'POST',
    path: '/api/v1/calls/call-1/media',
    successStatus: 200
  })
  assert.deepEqual(callRecordingContract('call-1'), {
    update: {
      method: 'PUT',
      path: '/api/v1/calls/call-1/recording',
      successStatus: 200
    },
    list: {
      method: 'GET',
      path: '/api/v1/calls/call-1/recordings',
      successStatus: 200
    }
  })
  assert.deepEqual(communicationContracts.listTelegram, {
    method: 'GET',
    path: '/api/v1/settings/telegram',
    successStatus: 200
  })
  assert.deepEqual(communicationContracts.createTelegram, {
    method: 'POST',
    path: '/api/v1/settings/telegram',
    successStatus: 201
  })
  assert.equal(
    telegramUnitPath('bot / main'),
    '/api/v1/settings/telegram/bot%20%2F%20main'
  )
  assert.deepEqual(telegramUnitContract('bot-main').update, {
    method: 'PUT',
    path: '/api/v1/settings/telegram/bot-main',
    successStatus: 200
  })
  assert.equal(
    telegramUnitDeletePath('bot-main', 4),
    '/api/v1/settings/telegram/bot-main?revision=4'
  )
})

test('message payload keeps only the finalized wire fields', () => {
  assert.deepEqual(
    createMessagePayload({
      thread_key: 'local-thread-key',
      request_id: 'request-message-1',
      line_id: 'line-main',
      to: '+818012345678',
      content: 'hello'
    }),
    {
      request_id: 'request-message-1',
      line_id: 'line-main',
      to: '+818012345678',
      content: 'hello'
    }
  )
})

test('message read payload uses the stable line and peer identity', () => {
  assert.deepEqual(
    createMessageReadPayload({
      line_id: '  line-main  ',
      peer: '  +818012345678  '
    }),
    {
      line_id: 'line-main',
      peer: '+818012345678'
    }
  )
  assert.throws(
    () => createMessageReadPayload({ line_id: '', peer: '+818012345678' }),
    /line_id/
  )
})

test('call and DTMF payloads use line_id, number, request_id, and digits', () => {
  assert.deepEqual(createCallPayload('line-main', '+818012345678', 'request-call-1'), {
    request_id: 'request-call-1',
    line_id: 'line-main',
    number: '+818012345678'
  })
  assert.deepEqual(
    createCallPayload('line-main', '+818012345678', 'request-call-2', true),
    {
      request_id: 'request-call-2',
      line_id: 'line-main',
      number: '+818012345678',
      recording_enabled: true
    }
  )
  assert.equal(
    createCallPayload('line-main', '+818012345678', 'request-call-3', false)
      .recording_enabled,
    false
  )
  assert.deepEqual(createCallActionPayload('hangup', 'request-hangup-1'), {
    request_id: 'request-hangup-1'
  })
  assert.deepEqual(createDTMFPayload('12#', 'request-dtmf-1'), {
    request_id: 'request-dtmf-1',
    digits: '12#'
  })
  assert.deepEqual(createCallMediaPayload('v=0\r\n'), {
    offer_sdp: 'v=0'
  })
  assert.deepEqual(createCallRecordingPayload(false), { enabled: false })
  assert.deepEqual(
    createRecordingSettingsPayload({ default_enabled: true, revision: 3 }),
    { default_enabled: true, revision: 3 }
  )
})

test('call response accepts unknown as an explicit phase without inventing a bearer', () => {
  const call = parseCallResponse({
    call: {
      ...canonicalCall,
      phase: 'unknown',
      bearer: ''
    }
  })
  assert.equal(call.phase, 'unknown')
  assert.equal(call.bearer, undefined)
  assert.throws(
    () => parseCallResponse({ call: { ...canonicalCall, phase: 'connected' } }),
    /call.phase/
  )
})

test('active calls response is authoritative and limited to one app call', () => {
  assert.deepEqual(parseActiveCallsResponse({ calls: [canonicalCall] }), [canonicalCall])
  assert.deepEqual(parseActiveCallsResponse({ calls: [] }), [])
  assert.throws(
    () => parseActiveCallsResponse({ calls: [canonicalCall, { ...canonicalCall, id: 'call-2' }] }),
    /多个活动通话/
  )
})

test('call media response requires a non-empty SDP answer', () => {
  assert.equal(parseCallMediaResponse({ answer_sdp: 'v=0\r\n' }), 'v=0')
  assert.throws(() => parseCallMediaResponse({ answer_sdp: '' }), /answer_sdp/)
  assert.throws(
    () => parseCallResponse({ call: { ...canonicalCall, media_available: null } }),
    /media_available/
  )
})

test('recording settings and active state require authoritative booleans and revisions', () => {
  assert.deepEqual(
    parseRecordingSettingsResponse({
      settings: { default_enabled: true, revision: 4 }
    }),
    { default_enabled: true, revision: 4 }
  )
  assert.deepEqual(
    parseCallRecordingState({
      state: {
        call_id: 'call-1',
        enabled: true,
        status: 'recording'
      }
    }),
    {
      call_id: 'call-1',
      enabled: true,
      active: true
    }
  )
  assert.throws(
    () =>
      parseRecordingSettingsResponse({
        settings: { default_enabled: false, revision: 0 }
      }),
    /revision/
  )
  assert.throws(
    () =>
      parseCallRecordingState({
        state: { call_id: 'call-1', enabled: true }
      }),
    /status/
  )
})

test('recording metadata derives same-origin authenticated API downloads', () => {
  const segment = {
    id: 'recording-1',
    call_id: 'call-1',
    status: 'ready',
    started_at: '2026-07-23T12:00:04Z',
    ended_at: '2026-07-23T12:01:04Z',
    duration_ms: 60_000,
    size_bytes: 123456
  }
  assert.deepEqual(parseCallRecordingsResponse({ segments: [segment] }), [
    {
      id: 'recording-1',
      call_id: 'call-1',
      started_at: '2026-07-23T12:00:04Z',
      ended_at: '2026-07-23T12:01:04Z',
      duration_seconds: 60,
      content_type: 'audio/ogg; codecs=opus',
      size_bytes: 123456,
      download_url: '/api/v1/calls/call-1/recordings/recording-1/download'
    }
  ])
  assert.deepEqual(
    parseCallRecordingsResponse({
      segments: [{ ...segment, status: 'recording' }]
    }),
    []
  )
})

test('recording aggregation preserves call metadata and only exposes ready downloads', () => {
  const call = {
    id: 'call-1',
    line_id: 'line-main',
    direction: 'incoming',
    remote_number: '+818012345678',
    contact_name: 'Alex Rowan',
    started_at: '2026-07-23T12:00:00Z',
    ended_at: '2026-07-23T12:02:00Z',
    duration_seconds: 120,
    missed: false
  }
  const segment = {
    id: 'segment-1',
    call_id: 'call-1',
    segment_index: 1,
    status: 'ready',
    started_at: '2026-07-23T12:00:04Z',
    ended_at: '2026-07-23T12:01:04Z',
    duration_ms: 60_000,
    size_bytes: 123456,
    created_at: '2026-07-23T12:00:03Z'
  }
  const [recording] = parseRecordingEntriesResponse({
    recordings: [{ segment, call, playable: true }]
  })
  assert.deepEqual(recording, {
    id: 'segment-1',
    call_id: 'call-1',
    segment_index: 1,
    status: 'ready',
    recorded_at: '2026-07-23T12:00:04Z',
    started_at: '2026-07-23T12:00:04Z',
    ended_at: '2026-07-23T12:01:04Z',
    duration_seconds: 60,
    size_bytes: 123456,
    playable: true,
    content_type: 'audio/ogg; codecs=opus',
    download_url: '/api/v1/calls/call-1/recordings/segment-1/download',
    call: {
      id: 'call-1',
      line_id: 'line-main',
      direction: 'incoming',
      remote_number: '+818012345678',
      display_name: 'Alex Rowan',
      contact_id: undefined,
      started_at: '2026-07-23T12:00:00Z',
      ended_at: '2026-07-23T12:02:00Z',
      duration_seconds: 120,
      missed: false,
      read: false,
      failure_reason: undefined
    }
  })

  const [failed] = parseRecordingEntriesResponse({
    recordings: [{
      segment: {
        ...segment,
        id: 'segment-2',
        status: 'failed',
        failure_code: 'interrupted'
      },
      call,
      playable: false
    }]
  })
  assert.equal(failed.playable, false)
  assert.equal(failed.failure_code, 'interrupted')
  assert.equal(failed.download_url, undefined)
  assert.equal('endpoint_line_id' in recording.call, false)
  assert.throws(
    () =>
      parseRecordingEntriesResponse({
        recordings: [{ segment: { ...segment, status: 'recording' }, call, playable: true }]
      }),
    /playable/
  )
})

test('message response unwraps the finalized message envelope', () => {
  const message = parseMessageResponse({
    message: {
      id: 'message-1',
      line_id: 'line-main',
      peer: '+818012345678',
      content: 'hello',
      timestamp: '2026-07-23T12:00:00Z',
      type: 2,
      status: 2
    }
  })
  assert.equal(message.id, 'message-1')
  assert.equal(message.direction, 'outgoing')
})

test('Telegram collection parsing drops tokens and preserves independent units', () => {
  const units = parseTelegramUnitsResponse({
    units: [
      {
        id: 'bot-main',
        display_name: 'Main Bot',
        enabled: true,
        chat_id: '-1001234567890',
        admin_id: '100000001',
        line_scopes: ['line-main'],
        incoming_sms: true,
        missed_calls: true,
        token_configured: true,
        bot_username: 'modemdeck_bot',
        bot_token: 'must-not-leak',
        revision: 4
      },
      {
        id: 'bot-travel',
        display_name: 'Travel Bot',
        enabled: false,
        chat_id: '-1001234567891',
        admin_id: '100000002',
        line_scopes: [],
        incoming_sms: true,
        missed_calls: false,
        token_configured: false,
        revision: 1
      }
    ]
  })
  assert.equal(units.length, 2)
  assert.deepEqual(units[0], {
    id: 'bot-main',
    display_name: 'Main Bot',
    enabled: true,
    chat_id: '-1001234567890',
    admin_id: '100000001',
    line_scopes: ['line-main'],
    incoming_sms: true,
    missed_calls: true,
    token_configured: true,
    bot_username: 'modemdeck_bot',
    revision: 4
  })
  assert.equal('bot_token' in units[0], false)
})

test('Telegram create and update payloads keep revision checks and write-only token semantics', () => {
  assert.deepEqual(
    createTelegramUnitPayload({
      display_name: 'Main Bot',
      enabled: true,
      chat_id: '-1001234567890',
      admin_id: '100000001',
      line_scopes: ['line-main'],
      incoming_sms: true,
      missed_calls: true,
      bot_token: '   ',
      revision: 4
    }),
    {
      display_name: 'Main Bot',
      enabled: true,
      chat_id: '-1001234567890',
      admin_id: '100000001',
      line_scopes: ['line-main'],
      incoming_sms: true,
      missed_calls: true,
      revision: 4
    }
  )
  assert.deepEqual(
    parseTelegramUnitResponse({
      unit: {
        id: 'bot-main',
        display_name: 'Main Bot',
        enabled: true,
        chat_id: '-1001234567890',
        admin_id: '100000001',
        line_scopes: ['line-main'],
        incoming_sms: true,
        missed_calls: true,
        token_configured: true,
        revision: 5
      }
    }),
    {
      id: 'bot-main',
      display_name: 'Main Bot',
      enabled: true,
      chat_id: '-1001234567890',
      admin_id: '100000001',
      line_scopes: ['line-main'],
      incoming_sms: true,
      missed_calls: true,
      token_configured: true,
      revision: 5
    }
  )
})
