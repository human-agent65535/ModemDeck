import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import { callState, initializeCallRuntime, shutdownCallRuntime, requestActiveCallRefresh, acceptRuntimeActiveCalls, hangupCall, rejectCall, answerCall, sendDTMF, dismissCall, occupiedLineIDs, renewActiveCallLease } from '../src/state/call.ts'
import { uiState, minimizeCallSurface } from '../src/state/ui.ts'

const session = (overrides = {}) => ({ id: 'call-1', line_id: 'line-1', direction: 'outgoing', remote_number: '+818000000000', phase: 'active', control_state: 'owned', media_available: false, created_at: '2026-10-08T00:00:00Z', active_at: '2026-10-08T00:00:02Z', ...overrides })
async function runtime(t, initial, run) {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const original = { get: gateway.getActiveCallSnapshot, act: gateway.callAction, renew: gateway.renewCallLease, dtmf: gateway.sendDTMF }
  let snapshot = { calls: [initial], reservations: [] }
  let leases = 0
  gateway.getActiveCallSnapshot = async () => snapshot
  gateway.renewCallLease = async () => { leases += 1; return { call_id: initial.id, expires_at: '' } }
  gateway.callAction = async () => { snapshot = { calls: [], reservations: [] } }
  try {
    initializeCallRuntime()
    await requestActiveCallRefresh()
    await run({ set: value => { snapshot = value }, leases: () => leases })
  } finally {
    shutdownCallRuntime()
    gateway.getActiveCallSnapshot = original.get
    gateway.callAction = original.act
    gateway.renewCallLease = original.renew
    gateway.sendDTMF = original.dtmf
    uiState.callMinimized = false
    t.mock.timers.reset()
  }
}

test('confirmed end holds frozen identity for one second without retaining occupation or lease', async t => {
  await runtime(t, session(), async ({ leases }) => {
    minimizeCallSurface()
    await hangupCall()
    assert.equal(callState.session?.phase, 'ended')
    assert.equal(callState.session?.id, 'call-1')
    const endedAt = callState.session.ended_at
    assert.ok(endedAt)
    assert.equal(callState.owned, false)
    assert.deepEqual(callState.sessions, [])
    assert.equal(occupiedLineIDs().size, 0)
    assert.equal(uiState.callMinimized, true)
    const before = leases()
    await renewActiveCallLease()
    assert.equal(leases(), before)
    t.mock.timers.tick(999)
    await requestActiveCallRefresh() // Repeated empty snapshots do not restart the hold.
    assert.equal(callState.session?.ended_at, endedAt)
    t.mock.timers.tick(1)
    assert.equal(callState.session, null)
  })
})

test('ending pending remains busy; a new incoming call preempts it and rejects late action completion', async t => {
  await runtime(t, session(), async () => {
    let complete
    gateway.callAction = () => new Promise(resolve => { complete = resolve })
    const action = hangupCall()
    assert.equal(callState.pendingAction, 'hangup')
    assert.equal(callState.busy, true)
    const incoming = session({ id: 'call-2', direction: 'incoming', phase: 'ringing', control_state: 'available', active_at: undefined })
    acceptRuntimeActiveCalls({ calls: [incoming], reservations: [] })
    assert.equal(callState.session?.id, 'call-2')
    assert.equal(callState.busy, false)
    complete()
    await action
    t.mock.timers.tick(2000)
    assert.equal(callState.session?.id, 'call-2')
    assert.equal(callState.session?.phase, 'ringing')
  })
})

test('new incoming cancels an old ended dismissal; explicit failure remains until manual dismissal', async t => {
  await runtime(t, session(), async () => {
    await hangupCall()
    acceptRuntimeActiveCalls({ calls: [session({ id: 'call-2', phase: 'ringing', direction: 'incoming', control_state: 'available', active_at: undefined })], reservations: [] })
    t.mock.timers.tick(2000)
    assert.equal(callState.session?.id, 'call-2')
    acceptRuntimeActiveCalls({ calls: [session({ id: 'call-2', phase: 'failed', failure_reason: 'Modem failed', active_at: undefined })], reservations: [] })
    t.mock.timers.tick(5000)
    assert.equal(callState.session?.phase, 'failed')
    assert.equal(callState.session?.failure_reason, 'Modem failed')
    dismissCall()
    assert.equal(callState.session, null)
  })
})

test('declined and cancelled calls have accurate outcomes with no fabricated connected duration', async t => {
  await runtime(t, session({ direction: 'incoming', phase: 'ringing', control_state: 'available', active_at: undefined }), async () => {
    await rejectCall()
    assert.equal(callState.endReason, 'declined')
    assert.equal(callState.session?.active_at, undefined)
  })
  await runtime(t, session({ phase: 'dialing', active_at: undefined }), async () => {
    await hangupCall()
    assert.equal(callState.endReason, 'cancelled')
    assert.equal(callState.session?.active_at, undefined)
  })
})

test('failed hangup keeps the live session and exposes a retryable action error', async t => {
  await runtime(t, session(), async () => {
    gateway.callAction = async () => { throw new Error('Temporary command failure') }
    await hangupCall()
    assert.equal(callState.session?.phase, 'active')
    assert.equal(callState.error, 'Temporary command failure')
    assert.equal(callState.busy, false)
    assert.equal(callState.pendingAction, '')
    t.mock.timers.tick(2000)
    assert.equal(callState.session?.id, 'call-1')
    gateway.callAction = async () => { acceptRuntimeActiveCalls({ calls: [], reservations: [] }) }
    await hangupCall()
    assert.equal(callState.session?.phase, 'ended')
  })
})

test('terminal SSE during DTMF is authoritative; old failure cannot disturb a new pending decline', async t => {
  await runtime(t, session(), async ({ set }) => {
    let failDTMF
    gateway.sendDTMF = () => new Promise((_resolve, reject) => { failDTMF = reject })
    const dtmf = sendDTMF('1')
    acceptRuntimeActiveCalls({ calls: [], reservations: [] })
    assert.equal(callState.session?.phase, 'ended')
    const incoming = session({ id: 'call-2', direction: 'incoming', phase: 'ringing', control_state: 'available', active_at: undefined })
    acceptRuntimeActiveCalls({ calls: [incoming], reservations: [] })
    let completeDecline
    gateway.callAction = () => new Promise(resolve => { completeDecline = resolve })
    const decline = rejectCall()
    failDTMF(new Error('Old DTMF failed'))
    await dtmf
    assert.equal(callState.session?.id, 'call-2')
    assert.equal(callState.pendingAction, 'reject')
    assert.equal(callState.busy, true)
    assert.equal(callState.error, '')
    set({ calls: [], reservations: [] })
    completeDecline()
    await decline
    assert.equal(callState.session?.id, 'call-2')
    assert.equal(callState.endReason, 'declined')
  })
})

test('answer completion cannot resurrect a call replaced by authoritative SSE', async t => {
  await runtime(t, session({ phase: 'ringing', direction: 'incoming', control_state: 'available', active_at: undefined }), async () => {
    let completeAnswer
    gateway.callAction = () => new Promise(resolve => { completeAnswer = resolve })
    const answer = answerCall()
    acceptRuntimeActiveCalls({ calls: [], reservations: [] })
    assert.equal(callState.session?.phase, 'ended')
    acceptRuntimeActiveCalls({ calls: [session({ id: 'call-2', phase: 'ringing', direction: 'incoming', control_state: 'available', active_at: undefined })], reservations: [] })
    completeAnswer()
    await answer
    assert.equal(callState.session?.id, 'call-2')
    assert.equal(callState.session?.control_state, 'available')
    assert.equal(callState.busy, false)
  })
})

test('answer completion preserves a newer connected SSE phase before refreshing', async t => {
  await runtime(t, session({ phase: 'ringing', direction: 'incoming', control_state: 'available', active_at: undefined }), async () => {
    let completeAnswer
    gateway.callAction = () => new Promise(resolve => { completeAnswer = resolve })
    const answer = answerCall()
    const active = session({ direction: 'incoming' })
    acceptRuntimeActiveCalls({ calls: [active], reservations: [] })
    gateway.getActiveCallSnapshot = async () => {
      assert.equal(callState.session?.phase, 'active')
      return { calls: [active], reservations: [] }
    }
    completeAnswer()
    await answer
    assert.equal(callState.session?.phase, 'active')
  })
})
