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

test('fixture client exposes incomingCallFixture only in fixture mode', async () => {
  const source = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8')

  assert.match(source, /get\('incomingCallFixture'\) === '1'/)
  assert.match(source, /createFixtureGateway\(fixturePreviewOptions\)/)
})
