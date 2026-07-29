import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
import {
  callState,
  dial,
  initializeCallRuntime,
  lineIsOccupied,
  occupiedLineIDs,
  requestActiveCallRefresh,
  selectForegroundSession,
  shutdownCallRuntime
} from '../src/state/call.ts'
import { bootstrapResource } from '../src/state/workspace.ts'

function call(overrides) {
  return {
    id: 'call-default',
    line_id: 'line-default',
    direction: 'incoming',
    remote_number: '+818000000000',
    phase: 'ringing',
    control_state: 'available',
    media_available: false,
    created_at: '2026-07-29T00:00:00Z',
    ...overrides
  }
}

function reservation(overrides = {}) {
  return {
    request_id: 'request-default',
    line_id: 'line-reserved',
    control_state: 'occupied',
    created_at: '2026-07-29T00:00:00Z',
    ...overrides
  }
}

test('foreground selection prefers owned, then answerable, then occupied calls', () => {
  const occupied = call({
    id: 'call-occupied',
    line_id: 'line-occupied',
    phase: 'active',
    control_state: 'occupied'
  })
  const incoming = call({
    id: 'call-incoming',
    line_id: 'line-incoming'
  })
  const owned = call({
    id: 'call-owned',
    line_id: 'line-owned',
    direction: 'outgoing',
    phase: 'active',
    control_state: 'owned'
  })

  assert.equal(
    selectForegroundSession([occupied, incoming, owned], occupied.id)?.id,
    owned.id
  )
  assert.equal(
    selectForegroundSession([occupied, incoming], occupied.id)?.id,
    incoming.id
  )
  assert.equal(
    selectForegroundSession([occupied], occupied.id)?.id,
    occupied.id
  )
})

test('foreground remains stable among calls with the same priority', () => {
  const first = call({
    id: 'call-first',
    line_id: 'line-first',
    phase: 'active',
    control_state: 'occupied'
  })
  const second = call({
    id: 'call-second',
    line_id: 'line-second',
    phase: 'active',
    control_state: 'occupied'
  })

  assert.equal(
    selectForegroundSession([first, second], second.id)?.id,
    second.id
  )
})

test('line occupancy merges calls and reservations and deduplicates by line', () => {
  const calls = [
    call({ id: 'call-1', line_id: 'line-1' }),
    call({ id: 'call-2', line_id: 'line-2', control_state: 'occupied' }),
    call({ id: 'call-ended', line_id: 'line-ended', phase: 'ended' })
  ]
  const reservations = [
    reservation({ request_id: 'request-2', line_id: 'line-2' }),
    reservation({ request_id: 'request-3', line_id: 'line-3' })
  ]
  const lines = occupiedLineIDs(calls, reservations)

  assert.deepEqual([...lines].sort(), ['line-1', 'line-2', 'line-3'])
  assert.equal(lineIsOccupied('line-3', calls, reservations), true)
  assert.equal(lineIsOccupied('line-ended', calls, reservations), false)
})

test('SSE reconciliation stores reservations without turning them into calls', async () => {
  const originalGetActiveCallSnapshot = gateway.getActiveCallSnapshot
  const occupied = call({
    id: 'call-occupied',
    line_id: 'line-occupied',
    phase: 'active',
    control_state: 'occupied'
  })
  const incoming = call({
    id: 'call-incoming',
    line_id: 'line-incoming'
  })
  const owned = call({
    id: 'call-owned',
    line_id: 'line-owned',
    direction: 'outgoing',
    phase: 'active',
    control_state: 'owned'
  })
  const pending = reservation()
  let snapshot = { calls: [], reservations: [pending] }
  gateway.getActiveCallSnapshot = async () => snapshot

  try {
    initializeCallRuntime()
    await requestActiveCallRefresh()
    assert.deepEqual(callState.sessions, [])
    assert.deepEqual(callState.reservations, [pending])
    assert.equal(callState.session, null)
    assert.equal(callState.owned, false)

    snapshot = { calls: [occupied, incoming], reservations: [pending] }
    await requestActiveCallRefresh()
    assert.deepEqual(
      callState.sessions.map(session => session.id),
      [occupied.id, incoming.id]
    )
    assert.deepEqual(callState.reservations, [pending])
    assert.equal(callState.session?.id, incoming.id)
    assert.equal(callState.owned, false)

    snapshot = {
      calls: [occupied, incoming, owned],
      reservations: []
    }
    await requestActiveCallRefresh()
    assert.deepEqual(
      callState.sessions.map(session => session.id),
      [occupied.id, incoming.id, owned.id]
    )
    assert.equal(callState.session?.id, owned.id)
    assert.equal(callState.owned, true)
  } finally {
    shutdownCallRuntime()
    gateway.getActiveCallSnapshot = originalGetActiveCallSnapshot
  }
})

test('dial refuses a reserved line before invoking the gateway', async () => {
  const originalBootstrap = bootstrapResource.data
  const originalBootstrapStatus = bootstrapResource.status
  const originalStartCall = gateway.startCall
  let startRequests = 0

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
        id: 'line-reserved',
        capabilities: {
          dial: true,
          media: true
        }
      }
    ]
  }
  bootstrapResource.status = 'ready'
  callState.reservations = [reservation()]
  gateway.startCall = async () => {
    startRequests += 1
    throw new Error('startCall must not run for a reserved line')
  }

  try {
    assert.equal(await dial('+818012345678', 'line-reserved'), false)
    assert.equal(startRequests, 0)
    assert.equal(callState.errorStatus, 409)
    assert.equal(callState.session, null)
  } finally {
    shutdownCallRuntime()
    gateway.startCall = originalStartCall
    bootstrapResource.data = originalBootstrap
    bootstrapResource.status = originalBootstrapStatus
  }
})

test('dialer keeps reservations as line status without making them call surfaces', async () => {
  const [dialer, selector] = await Promise.all([
    readFile(new URL('../src/components/DialerPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/LineSelector.vue', import.meta.url), 'utf8')
  ])

  assert.match(dialer, /collectOccupiedLineIDs\(\)/)
  assert.match(dialer, /const occupiedLineCount = computed\(\(\) => occupiedLineIDs\.value\.size\)/)
  assert.match(dialer, /v-model="lineSwitcherID"/)
  assert.match(dialer, /:status-values="\[\.\.\.occupiedLineIDs\]"/)
  assert.match(dialer, /:disabled="callState\.owned"/)
  assert.match(dialer, /if \(callState\.owned\) return/)
  assert.match(
    dialer,
    /if \(lineHasActiveCall\(lineID\)\) \{[\s\S]*showActiveCallForLine\(lineID\)/
  )
  assert.match(dialer, /callState\.owned \? t\('dialer\.activeCall'\)/)
  assert.match(dialer, /v-else-if="occupiedLineCount > 0"/)
  assert.match(
    dialer,
    /const selectedStillExists = dialLines\.value\.some\([\s\S]*Boolean\(number\.value\.trim\(\)\)/
  )
  assert.doesNotMatch(
    dialer,
    /const selectedStillExists = availableDialLines\.value\.some/
  )
  assert.match(
    dialer,
    /<span class="dialer-toolbar__title">[\s\S]*<h2>[\s\S]*<span class="dialer-status-slot">[\s\S]*dialer-active-calls--busy/
  )
  assert.match(dialer, /\.dialer-toolbar__title \{[\s\S]*width: 104px;[\s\S]*grid-template-columns:/)
  assert.match(dialer, /class="dialer-header-actions">[\s\S]*v-if="!permanent"[\s\S]*class="icon-button"/)
  assert.match(dialer, /\.dialer-active-calls--busy \{[\s\S]*color: var\(--danger\);/)
  assert.match(selector, /:aria-disabled="option\.disabled"/)
  assert.match(selector, /if \(option\.disabled\) return/)
  assert.match(selector, /statusValueSet\.value\.has\(value\)/)
  assert.match(selector, /<em v-if="displayStatus">\{\{ displayStatus \}\}<\/em>/)
  assert.match(selector, /\.line-selector__identity small > em,[\s\S]*color: var\(--danger\);/)
})
