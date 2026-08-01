import assert from 'node:assert/strict'
import test from 'node:test'
import {
  callActionPath,
  callActionContract,
  callLeaseContract,
  callLeasePath,
  callMediaContract,
  callMediaReleaseContract,
  callMediaPath,
  callRecordPath,
  callRecordingContract,
  callRecordingPath,
  callRecordingResourcePath,
  callRecordingsPath,
  communicationContracts,
  communicationPaths,
  createCallActionPayload,
  createCallLeasePayload,
  createCallMediaPayload,
  createCallMediaReleasePayload,
  createCallPayload,
  createCallRecordingPayload,
  createDTMFPayload,
  createMessagePayload,
  createMessageReadPayload,
  createRecordingSettingsPayload,
  createTelegramUnitPayload,
  missedCallReadPath,
  parseActiveCallSnapshotResponse,
  parseCallLeaseStatus,
  parseCallMediaResponse,
  parseCallRecordingSnapshotResponse,
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
  control_state: 'owned',
  media_available: true,
  created_at: '2026-07-23T12:00:00Z',
  active_at: '2026-07-23T12:00:03Z',
  bearer: 'VoLTE'
}

const canonicalReservation = {
  request_id: 'request-call-2',
  line_id: 'line-2',
  control_state: 'occupied',
  created_at: '2026-07-29T09:00:00Z'
}

test('communication and Telegram endpoints match the root API', () => {
  assert.deepEqual(communicationPaths, {
    messages: '/api/v1/messages',
    messageThreads: '/api/v1/messages/threads',
    messageThreadState: '/api/v1/messages/threads/state',
    messageRead: '/api/v1/messages/read',
    calls: '/api/v1/calls',
    callsBatch: '/api/v1/calls/batch',
    missedCallsRead: '/api/v1/calls/missed/read',
    activeCalls: '/api/v1/calls/active',
    recordings: '/api/v1/recordings',
    recordingsBatch: '/api/v1/recordings/batch',
    callSettings: '/api/v1/settings/calls',
    recordingSettings: '/api/v1/settings/recording',
    telegram: '/api/v1/settings/telegram'
  })
  assert.equal(callActionPath('call / 1', 'answer'), '/api/v1/calls/call%20%2F%201/answer')
  assert.equal(callActionPath('call-1', 'dtmf'), '/api/v1/calls/call-1/dtmf')
  assert.equal(callLeasePath('call / 1'), '/api/v1/calls/call%20%2F%201/lease')
  assert.equal(callMediaPath('call / 1'), '/api/v1/calls/call%20%2F%201/media')
  assert.equal(
    callRecordingPath('call / 1'),
    '/api/v1/calls/call%20%2F%201/recording'
  )
  assert.equal(
    callRecordingsPath('call / 1'),
    '/api/v1/calls/call%20%2F%201/recordings'
  )
  assert.equal(callRecordPath('call / 1'), '/api/v1/calls/call%20%2F%201')
  assert.equal(missedCallReadPath('call / 1'), '/api/v1/calls/call%20%2F%201/read')
  assert.equal(
    callRecordingResourcePath('call / 1', 'segment / 1'),
    '/api/v1/calls/call%20%2F%201/recordings/segment%20%2F%201'
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
  assert.deepEqual(communicationContracts.deleteMessageThread, {
    method: 'DELETE',
    path: '/api/v1/messages/threads',
    successStatus: 204
  })
  assert.deepEqual(communicationContracts.updateMessageThreads, {
    method: 'PATCH',
    path: '/api/v1/messages/threads/state',
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
  assert.deepEqual(communicationContracts.updateCalls, {
    method: 'PATCH',
    path: '/api/v1/calls/batch',
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
  assert.deepEqual(communicationContracts.updateRecordings, {
    method: 'PATCH',
    path: '/api/v1/recordings/batch',
    successStatus: 204
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
    successStatus: 202
  })
  assert.deepEqual(callLeaseContract('call-1'), {
    method: 'PUT',
    path: '/api/v1/calls/call-1/lease',
    successStatus: 200
  })
  assert.deepEqual(callMediaContract('call-1'), {
    method: 'POST',
    path: '/api/v1/calls/call-1/media',
    successStatus: 200
  })
  assert.deepEqual(callMediaReleaseContract('call-1'), {
    method: 'DELETE',
    path: '/api/v1/calls/call-1/media',
    successStatus: 204
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

test('call media exchange and release require the same owner token', () => {
  assert.deepEqual(createCallMediaPayload(' owner-1 ', ' offer-sdp ', ' browser-1 '), {
    owner_token: 'owner-1',
    offer_sdp: ' offer-sdp ',
    holder_id: 'browser-1'
  })
  assert.deepEqual(createCallMediaReleasePayload(' owner-1 ', ' browser-1 '), {
    owner_token: 'owner-1',
    holder_id: 'browser-1'
  })
  assert.throws(
    () => createCallMediaPayload('', 'offer-sdp', 'browser-1'),
    /owner_token/
  )
  assert.throws(
    () => createCallMediaReleasePayload('', 'browser-1'),
    /owner_token/
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
  assert.deepEqual(
    createCallPayload(
      'line-main',
      '+818012345678',
      'request-call-1',
      'browser-1'
    ),
    {
      request_id: 'request-call-1',
      line_id: 'line-main',
      number: '+818012345678',
      holder_id: 'browser-1'
    }
  )
  assert.deepEqual(
    createCallPayload(
      'line-main',
      '+818012345678',
      'request-call-2',
      'browser-1',
      true
    ),
    {
      request_id: 'request-call-2',
      line_id: 'line-main',
      number: '+818012345678',
      holder_id: 'browser-1',
      recording_enabled: true
    }
  )
  assert.equal(
    createCallPayload(
      'line-main',
      '+818012345678',
      'request-call-3',
      'browser-1',
      false
    )
      .recording_enabled,
    false
  )
  assert.deepEqual(
    createCallActionPayload('hangup', 'request-hangup-1', 'browser-1'),
    {
      request_id: 'request-hangup-1',
      holder_id: 'browser-1'
    }
  )
  assert.deepEqual(
    createCallActionPayload('answer', 'request-answer-1', 'browser-1', false),
    {
      request_id: 'request-answer-1',
      holder_id: 'browser-1',
      recording_enabled: false
    }
  )
  assert.equal(
    'recording_enabled' in
      createCallActionPayload('hangup', 'request-hangup-2', 'browser-1', true),
    false
  )
  assert.deepEqual(createCallLeasePayload(' browser-1 '), {
    holder_id: 'browser-1'
  })
  assert.deepEqual(createDTMFPayload('12#', 'request-dtmf-1', 'browser-1'), {
    request_id: 'request-dtmf-1',
    digits: '12#',
    holder_id: 'browser-1'
  })
  assert.deepEqual(createCallMediaPayload(' owner-1 ', 'v=0\r\n', 'browser-1'), {
    owner_token: 'owner-1',
    offer_sdp: 'v=0\r\n',
    holder_id: 'browser-1'
  })
  assert.deepEqual(createCallRecordingPayload(false, 'browser-1'), {
    enabled: false,
    holder_id: 'browser-1'
  })
  assert.deepEqual(
    createRecordingSettingsPayload({ default_enabled: true, revision: 3 }),
    { default_enabled: true, revision: 3 }
  )
})

test('call lease status requires an active holder and a valid expiry', () => {
  assert.deepEqual(
    parseCallLeaseStatus({
      call_id: 'call-1',
      holder_id: 'browser-1',
      expires_at: '2026-07-28T12:00:15Z'
    }),
    {
      call_id: 'call-1',
      holder_id: 'browser-1',
      expires_at: '2026-07-28T12:00:15Z'
    }
  )
  assert.throws(
    () =>
      parseCallLeaseStatus({
        call_id: 'call-1',
        holder_id: 'browser-1',
        expires_at: 'not-a-time'
      }),
    /expires_at/
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
  assert.throws(
    () =>
      parseCallResponse({
        call: { ...canonicalCall, control_state: 'shared' }
      }),
    /call.control_state/
  )
})

test('active call snapshot preserves calls and outgoing line reservations', () => {
  assert.deepEqual(
    parseActiveCallSnapshotResponse({
      calls: [canonicalCall],
      reservations: [canonicalReservation]
    }),
    {
      calls: [canonicalCall],
      reservations: [canonicalReservation]
    }
  )
  assert.deepEqual(
    parseActiveCallSnapshotResponse({ calls: [], reservations: [] }),
    { calls: [], reservations: [] }
  )
  assert.throws(
    () =>
      parseActiveCallSnapshotResponse({
        calls: [canonicalCall, canonicalCall],
        reservations: []
      }),
    /重复通话/
  )
  assert.throws(
    () =>
      parseActiveCallSnapshotResponse({
        calls: [],
        reservations: [canonicalReservation, canonicalReservation]
      }),
    /重复预占/
  )
  assert.throws(
    () =>
      parseActiveCallSnapshotResponse({
        calls: [],
        reservations: [{ ...canonicalReservation, control_state: 'available' }]
      }),
    /control_state/
  )
  assert.throws(
    () => parseActiveCallSnapshotResponse({ calls: [] }),
    /reservations/
  )
})

test('call media response requires a non-empty SDP answer', () => {
  assert.equal(parseCallMediaResponse({ answer_sdp: 'v=0\r\n' }), 'v=0\r\n')
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
        status: 'recording',
        active_segment_id: 'segment-1'
      }
    }),
    {
      call_id: 'call-1',
      enabled: true,
      status: 'recording',
      active_segment_id: 'segment-1'
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
  assert.throws(
    () =>
      parseCallRecordingState({
        state: { call_id: 'call-1', enabled: true, status: 'starting' }
      }),
    /未知/
  )
})

test('recording metadata derives same-origin authenticated API downloads', () => {
  const segment = {
    id: 'recording-1',
    call_id: 'call-1',
    segment_index: 1,
    status: 'ready',
    started_at: '2026-07-23T12:00:04Z',
    ended_at: '2026-07-23T12:01:04Z',
    duration_ms: 60_000,
    size_bytes: 123456,
    created_at: '2026-07-23T12:00:03Z'
  }
  const parsedSegments = parseCallRecordingsResponse({ segments: [segment] })
  assert.deepEqual(parsedSegments, [
    {
      id: 'recording-1',
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
      download_url: '/api/v1/calls/call-1/recordings/recording-1/download'
    }
  ])
  assert.equal('favorite' in parsedSegments[0], false)
  assert.deepEqual(
    parseCallRecordingSnapshotResponse({
      state: {
        call_id: 'call-1',
        enabled: true,
        status: 'recording',
        active_segment_id: 'recording-1'
      },
      segments: [segment]
    }),
    {
      state: {
        call_id: 'call-1',
        enabled: true,
        status: 'recording',
        active_segment_id: 'recording-1'
      },
      segments: parsedSegments
    }
  )
  assert.deepEqual(
    parseCallRecordingsResponse({
      segments: [{ ...segment, status: 'recording', ended_at: undefined }]
    }),
    [
      {
        id: 'recording-1',
        call_id: 'call-1',
        segment_index: 1,
        status: 'recording',
        recorded_at: '2026-07-23T12:00:04Z',
        started_at: '2026-07-23T12:00:04Z',
        duration_seconds: 60,
        size_bytes: 123456,
        playable: false
      }
    ]
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
  const { items: [recording] } = parseRecordingEntriesResponse({
    recordings: [{ segment, call, playable: true, favorite: true }],
    meta: { limit: 50, next_cursor: '', has_more: false }
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
    favorite: true,
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
      favorite: false,
      failure_reason: undefined
    }
  })

  const { items: [failed] } = parseRecordingEntriesResponse({
    recordings: [{
      segment: {
        ...segment,
        id: 'segment-2',
        status: 'failed',
        failure_code: 'interrupted'
      },
      call,
      playable: false,
      favorite: false
    }],
    meta: { limit: 50, next_cursor: '', has_more: false }
  })
  assert.equal(failed.playable, false)
  assert.equal(failed.failure_code, 'interrupted')
  assert.equal(failed.download_url, undefined)
  assert.equal('endpoint_line_id' in recording.call, false)
  assert.throws(
    () =>
      parseRecordingEntriesResponse({
        recordings: [{
          segment: { ...segment, status: 'recording' },
          call,
          playable: true,
          favorite: false
        }]
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
  assert.equal(message.delivery_status, 'submitted')

  const delivered = parseMessageResponse({
    message: {
      id: 'message-2',
      line_id: 'line-main',
      peer: '+818012345678',
      content: 'delivered',
      timestamp: '2026-07-23T12:01:00Z',
      type: 2,
      status: 2,
      delivery_status: 'delivered'
    }
  })
  assert.equal(delivered.delivery_status, 'delivered')
  assert.throws(
    () =>
      parseMessageResponse({
        message: {
          id: 'message-3',
          line_id: 'line-main',
          peer: '+818012345678',
          content: 'unknown status',
          timestamp: '2026-07-23T12:02:00Z',
          type: 2,
          status: 2,
          delivery_status: 'expired'
        }
      }),
    /message\.delivery_status/
  )
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
        assigned_user_id: 'user-member',
        assigned_username: 'member',
        all_assigned_lines: false,
        effective_enabled: true,
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
        assigned_user_id: 'user-admin',
        assigned_username: 'admin',
        all_assigned_lines: true,
        effective_enabled: false,
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
    assigned_user_id: 'user-member',
    assigned_username: 'member',
    all_assigned_lines: false,
    effective_enabled: true,
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
      assigned_user_id: 'user-member',
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
      assigned_user_id: 'user-member',
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
        assigned_user_id: 'user-member',
        all_assigned_lines: false,
        effective_enabled: true,
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
      assigned_user_id: 'user-member',
      all_assigned_lines: false,
      effective_enabled: true,
      line_scopes: ['line-main'],
      incoming_sms: true,
      missed_calls: true,
      token_configured: true,
      revision: 5
    }
  )
})
