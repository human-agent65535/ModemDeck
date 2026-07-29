import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
import {
  activeLineIDsForSessions,
  callState,
  initializeCallRuntime,
  requestActiveCallRefresh,
  selectForegroundSession,
  shutdownCallRuntime
} from '../src/state/call.ts'

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

test('active line identity covers every live modem call', () => {
  const lines = activeLineIDsForSessions([
    call({ id: 'call-1', line_id: 'line-1' }),
    call({ id: 'call-2', line_id: 'line-2', control_state: 'occupied' }),
    call({ id: 'call-ended', line_id: 'line-ended', phase: 'ended' })
  ])

  assert.deepEqual([...lines].sort(), ['line-1', 'line-2'])
})

test('SSE reconciliation keeps all calls and advances the foreground by ownership', async () => {
  const originalGetActiveCalls = gateway.getActiveCalls
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
  let calls = [occupied, incoming]
  gateway.getActiveCalls = async () => calls

  try {
    initializeCallRuntime()
    await requestActiveCallRefresh()
    assert.deepEqual(
      callState.sessions.map(session => session.id),
      [occupied.id, incoming.id]
    )
    assert.equal(callState.session?.id, incoming.id)
    assert.equal(callState.owned, false)

    calls = [occupied, incoming, owned]
    await requestActiveCallRefresh()
    assert.deepEqual(
      callState.sessions.map(session => session.id),
      [occupied.id, incoming.id, owned.id]
    )
    assert.equal(callState.session?.id, owned.id)
    assert.equal(callState.owned, true)
  } finally {
    shutdownCallRuntime()
    gateway.getActiveCalls = originalGetActiveCalls
  }
})

test('dialer keeps occupied lines viewable and retains a multi-call count', async () => {
  const [dialer, selector] = await Promise.all([
    readFile(new URL('../src/components/DialerPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/LineSelector.vue', import.meta.url), 'utf8')
  ])

  assert.match(dialer, /activeLineIDsForSessions\(callState\.sessions\)/)
  assert.match(dialer, /const activeCallLineCount = computed\(\(\) => occupiedLineIDs\.value\.size\)/)
  assert.match(dialer, /v-model="lineSwitcherID"/)
  assert.match(dialer, /:status-values="\[\.\.\.occupiedLineIDs\]"/)
  assert.match(dialer, /:disabled="callState\.owned"/)
  assert.match(dialer, /if \(callState\.owned\) return/)
  assert.match(dialer, /showActiveCallForLine\(lineID\)/)
  assert.match(dialer, /callState\.owned \? t\('dialer\.activeCall'\)/)
  assert.match(dialer, /v-else-if="showingCall && activeCallLineCount > 0"/)
  assert.match(
    dialer,
    /<span class="dialer-toolbar__title">[\s\S]*<h2>[\s\S]*<span class="dialer-status-slot">[\s\S]*dialer-active-calls--busy/
  )
  assert.match(dialer, /\.dialer-toolbar__title \{[\s\S]*width: 164px;[\s\S]*grid-template-columns:/)
  assert.match(dialer, /class="dialer-header-actions">[\s\S]*v-if="!permanent"[\s\S]*class="icon-button"/)
  assert.match(dialer, /\.dialer-active-calls--busy \{[\s\S]*color: var\(--danger\);/)
  assert.match(selector, /:aria-disabled="option\.disabled"/)
  assert.match(selector, /if \(option\.disabled\) return/)
  assert.match(selector, /statusValueSet\.value\.has\(value\)/)
  assert.match(selector, /<em v-if="displayStatus">\{\{ displayStatus \}\}<\/em>/)
  assert.match(selector, /\.line-selector__identity small > em,[\s\S]*color: var\(--danger\);/)
})
