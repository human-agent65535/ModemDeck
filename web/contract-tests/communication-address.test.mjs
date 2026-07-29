import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  communicationAddressKind,
  isContactPhoneCandidate,
  isOneWayMessageSender
} from '../src/utils/communicationAddress.ts'

test('network communication addresses distinguish subscribers, short codes, and sender IDs', () => {
  assert.equal(communicationAddressKind('+12025550198'), 'subscriber')
  assert.equal(communicationAddressKind('12345'), 'short_code')
  assert.equal(communicationAddressKind('DEMO_ALERT'), 'alphanumeric')
  assert.equal(communicationAddressKind('not/an/address'), 'unknown')
})

test('only telephone-number syntax is offered to contact phone workflows', () => {
  assert.equal(isContactPhoneCandidate('+12025550198'), true)
  assert.equal(isContactPhoneCandidate('202-555-0198'), true)
  assert.equal(isContactPhoneCandidate('12345'), false)
  assert.equal(isContactPhoneCandidate('DEMO_ALERT'), false)
  assert.equal(isOneWayMessageSender('DEMO_ALERT'), true)
})

test('message and contact surfaces gate actions for one-way sender IDs', async () => {
  const [messages, contactActions] = await Promise.all([
    readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/ContactNumberActions.vue', import.meta.url), 'utf8')
  ])

  assert.match(messages, /selectedThreadIsOneWay/)
  assert.match(messages, /v-if="!selectedThreadIsOneWay"/)
  assert.match(messages, /activeRecipientIsContactable/)
  assert.match(contactActions, /v-if="numberIsContactable"/)
})
