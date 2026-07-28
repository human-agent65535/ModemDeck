import assert from 'node:assert/strict'
import test from 'node:test'
import { knownCallBearerLabel } from '../src/callBearer.ts'

test('call surfaces share canonical cellular voice bearer labels', () => {
  assert.equal(knownCallBearerLabel('volte'), 'VoLTE')
  assert.equal(knownCallBearerLabel('VoWiFi'), 'VoWiFi')
  assert.equal(knownCallBearerLabel('gsm'), 'GSM / CS')
  assert.equal(knownCallBearerLabel('cs'), 'GSM / CS')
  assert.equal(knownCallBearerLabel('circuit-switched'), 'GSM / CS')
  assert.equal(knownCallBearerLabel('unknown'), '')
  assert.equal(knownCallBearerLabel(undefined), '')
})
