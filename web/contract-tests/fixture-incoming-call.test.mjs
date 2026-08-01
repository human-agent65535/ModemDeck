import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'

test('incoming-call preview remains ringing until the user acts', async () => {
  const gateway = createFixtureGateway({ initialIncomingCall: true })

  for (let poll = 0; poll < 3; poll += 1) {
    const { calls, reservations } = await gateway.getActiveCallSnapshot()
    const [call] = calls
    assert.equal(call?.id, 'call-fixture-incoming')
    assert.equal(call?.direction, 'incoming')
    assert.equal(call?.phase, 'ringing')
    assert.equal(call?.display_name, 'Alex Rowan')
    assert.deepEqual(reservations, [])
  }

  await gateway.callAction('call-fixture-incoming', 'answer')
  const { calls: [answered] } = await gateway.getActiveCallSnapshot()
  assert.equal(answered.phase, 'active')
})

test('incoming-call preview can show a call claimed by another browser', async () => {
  const gateway = createFixtureGateway({ initialIncomingCall: 'occupied' })
  const { calls: [call] } = await gateway.getActiveCallSnapshot()
  const bootstrap = await gateway.getBootstrap()

  assert.equal(call?.direction, 'incoming')
  assert.equal(call?.phase, 'active')
  assert.equal(call?.control_state, 'occupied')
  assert.equal(call?.media_available, false)
  assert.equal(bootstrap.lines[1]?.capabilities?.dial, true)
  assert.equal(bootstrap.lines[1]?.capabilities?.media, true)
})

test('multi-call preview exposes two occupied modem lines', async () => {
  const gateway = createFixtureGateway({ initialConcurrentCalls: true })
  const snapshot = await gateway.getActiveCallSnapshot()

  assert.equal(snapshot.calls.length, 2)
  assert.deepEqual(
    snapshot.calls.map(call => call.line_id),
    ['line-fixture-main', 'line-fixture-travel']
  )
  assert.deepEqual(
    snapshot.calls.map(call => call.control_state),
    ['occupied', 'occupied']
  )
  assert.deepEqual(snapshot.reservations, [])
})

test('fixture active snapshot can expose a reservation without inventing a call', async () => {
  const gateway = createFixtureGateway({ initialOutgoingReservation: 'occupied' })
  const snapshot = await gateway.getActiveCallSnapshot()

  assert.deepEqual(snapshot.calls, [])
  assert.equal(snapshot.reservations.length, 1)
  assert.equal(snapshot.reservations[0]?.line_id, 'line-fixture-main')
  assert.equal(snapshot.reservations[0]?.control_state, 'occupied')
})

test('fixture client exposes incomingCallFixture modes only in fixture mode', async () => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8')
  const callMedia = await readFile(
    new URL('../src/state/callMedia.ts', import.meta.url),
    'utf8'
  )

  assert.match(source, /get\('callMediaFixture'\) === '1'/)
  assert.match(source, /get\('incomingCallFixture'\)/)
  assert.match(source, /incomingCallFixture === 'occupied' \? 'occupied'/)
  assert.match(source, /get\('multiCallFixture'\) === '1'/)
  assert.match(source, /initialConcurrentCalls: initialConcurrentCallsFixture/)
  assert.match(source, /get\('outgoingReservationFixture'\)/)
  assert.match(source, /initialOutgoingReservation: initialOutgoingReservationFixture/)
  assert.match(source, /createFixtureGateway\(fixturePreviewOptions\)/)
  assert.match(
    callMedia,
    /if \(fixtureCallMediaPreview\) \{[\s\S]*callMediaState\.callID = session\.id[\s\S]*callMediaState\.status = 'active'/
  )
  assert.match(
    callMedia,
    /export function toggleCallMute\(\): void \{[\s\S]*if \(fixtureCallMediaPreview\)[\s\S]*callMediaState\.muted = !callMediaState\.muted/
  )
})
