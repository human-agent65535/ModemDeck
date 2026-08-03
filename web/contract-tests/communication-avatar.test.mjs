import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  communicationAvatarFallback,
  communicationAvatarPaletteKey
} from '../src/utils/communicationAvatar.ts'

test('unbound communication identities use their channel icon', () => {
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: '+1 202 555 0198',
      address: '+1 202 555 0198',
      contactBound: false
    }),
    'call'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: 'DEMO_ALERT',
      address: 'DEMO_ALERT',
      contactBound: false
    }),
    'message'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'recording',
      name: '+1 202 555 0198',
      address: '+1 202 555 0198',
      contactBound: false
    }),
    'recording'
  )
})

test('bound contacts use their image, initials, or person fallback regardless of channel', () => {
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: 'Alex Rowan',
      address: '+1 202 555 0198',
      contactBound: true
    }),
    'initials'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: 'Alex Rowan',
      address: '+1 202 555 0198',
      contactBound: true
    }),
    'initials'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'recording',
      name: 'Alex Rowan',
      address: '+1 202 555 0198',
      contactBound: true
    }),
    'initials'
  )
  assert.equal(
    communicationAvatarFallback({
      channel: 'message',
      name: '+1 202 555 0198',
      address: '+1 202 555 0198',
      contactBound: true
    }),
    'person'
  )
})

test('equivalent international forms share a stable avatar palette', () => {
  const plusIdentity = {
    channel: 'call',
    name: '+1 202 555 0198',
    address: '+12025550198',
    contactBound: false
  }
  const internationalPrefixIdentity = {
    channel: 'call',
    name: '+1 202 555 0198',
    address: '0012025550198',
    contactBound: false
  }
  assert.equal(
    communicationAvatarPaletteKey(plusIdentity, 'call'),
    communicationAvatarPaletteKey(internationalPrefixIdentity, 'call')
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
  assert.match(files[4], /channel="recording"/)
  for (const source of files) assert.match(source, /contact-bound/)
})

test('bound communication avatars own one shared bottom-right channel badge', async () => {
  const avatar = await readFile(
    new URL('../src/components/CommunicationAvatar.vue', import.meta.url),
    'utf8'
  )
  const callRow = await readFile(
    new URL('../src/components/CallHistoryListItem.vue', import.meta.url),
    'utf8'
  )

  assert.match(avatar, /v-if="contactBound"/)
  assert.match(avatar, /right: -4px;[\s\S]*bottom: -4px;/)
  assert.match(avatar, /channel === 'message'[\s\S]*MessageSquareText/)
  assert.match(avatar, /channel === 'recording'[\s\S]*AudioLines/)
  assert.doesNotMatch(callRow, /call-direction-icon/)
})
