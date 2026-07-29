import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'

test('incoming-call preview remains ringing until the user acts', async () => {
  const gateway = createFixtureGateway({ initialIncomingCall: true })

  for (let poll = 0; poll < 3; poll += 1) {
    const [call] = await gateway.getActiveCalls()
    assert.equal(call?.id, 'call-fixture-incoming')
    assert.equal(call?.direction, 'incoming')
    assert.equal(call?.phase, 'ringing')
    assert.equal(call?.display_name, 'Alex Rowan')
  }

  await gateway.callAction('call-fixture-incoming', 'answer')
  const [answered] = await gateway.getActiveCalls()
  assert.equal(answered.phase, 'active')
})

test('incoming-call preview can show a call claimed by another browser', async () => {
  const gateway = createFixtureGateway({ initialIncomingCall: 'occupied' })
  const [call] = await gateway.getActiveCalls()
  const bootstrap = await gateway.getBootstrap()

  assert.equal(call?.direction, 'incoming')
  assert.equal(call?.phase, 'active')
  assert.equal(call?.control_state, 'occupied')
  assert.equal(call?.media_available, false)
  assert.equal(bootstrap.lines[1]?.capabilities?.dial, true)
  assert.equal(bootstrap.lines[1]?.capabilities?.media, true)
})

test('fixture client exposes incomingCallFixture modes only in fixture mode', async () => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8')

  assert.match(source, /get\('incomingCallFixture'\)/)
  assert.match(source, /incomingCallFixture === 'occupied' \? 'occupied'/)
  assert.match(source, /createFixtureGateway\(fixturePreviewOptions\)/)
})
