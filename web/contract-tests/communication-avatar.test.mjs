import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  communicationAvatarFallback,
  communicationAvatarPaletteKey
} from '../src/utils/communicationAvatar.ts'

test('communication avatars do not derive initials from telephone numbers', () => {
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: '+1 202 555 0198',
      address: '+1 202 555 0198'
    }),
    'person'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: '+1 202 555 0198',
      address: '0012025550198'
    }),
    'person'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: 'Alex Rowan',
      address: '+1 202 555 0198'
    }),
    'initials'
  )
})

test('communication avatars distinguish people, services, and anonymous callers', () => {
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: 'DEMO_ALERT',
      address: 'DEMO_ALERT'
    }),
    'initials'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: '12345',
      address: '12345'
    }),
    'service'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: '191',
      address: '191'
    }),
    'service'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: '12345678901234567890',
      address: '12345678901234567890'
    }),
    'service'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: '202 555 0197',
      address: '202 555 0197'
    }),
    'person'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: 'Private',
      address: 'Private'
    }),
    'unknown'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: 'Unknown number',
      address: ''
    }),
    'unknown'
  )
})

test('equivalent international forms share a stable avatar palette', () => {
  const plusIdentity = {
    channel: 'call',
    name: '+1 202 555 0198',
    address: '+12025550198'
  }
  const internationalPrefixIdentity = {
    channel: 'call',
    name: '+1 202 555 0198',
    address: '0012025550198'
  }
  assert.equal(
    communicationAvatarPaletteKey(plusIdentity, 'person'),
    communicationAvatarPaletteKey(internationalPrefixIdentity, 'person')
  )
})

test('communication surfaces use the semantic avatar component', async () => {
  const files = await Promise.all(
    [
      '../src/components/CallHistoryListItem.vue',
      '../src/components/MessageThreadListItem.vue',
      '../src/components/ContactHeaderIdentity.vue',
      '../src/components/CallSurface.vue',
      '../src/views/RecordingsView.vue'
    ].map(path => readFile(new URL(path, import.meta.url), 'utf8'))
  )

  for (const source of files) assert.match(source, /CommunicationAvatar/)
  assert.match(files[0], /channel="call"/)
  assert.match(files[1], /channel="message"/)
  assert.match(files[3], /:address="presentedNumber"/)
})
