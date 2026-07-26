import assert from 'node:assert/strict'
import { stat, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

import {
  callSoundMode,
  claimIncomingMessageSound,
  normalizeBrowserSoundPreferences,
  notificationCatalog,
  ringtoneCatalog
} from '../src/state/browserSounds.ts'

const incomingCall = {
  id: 'call-incoming',
  line_key: 'line-main',
  direction: 'incoming',
  remote_number: '+818012345678',
  phase: 'ringing',
  media_available: false,
  created_at: '2026-07-26T10:00:00Z'
}

const outgoingCall = {
  ...incomingCall,
  id: 'call-outgoing',
  direction: 'outgoing'
}

test('call sounds map one call session to one audible lifecycle', () => {
  assert.equal(callSoundMode(null), 'idle')
  assert.equal(callSoundMode(incomingCall), 'incoming')
  assert.equal(callSoundMode({ ...incomingCall, phase: 'active' }), 'idle')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'unknown' }), 'waiting')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'dialing' }), 'waiting')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'ringing' }), 'waiting')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'connecting' }), 'idle')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'active' }), 'idle')
  assert.equal(callSoundMode({ ...outgoingCall, phase: 'ended' }), 'idle')
})

test('incoming message sounds are claimed once and retain bounded history', () => {
  const claimed = new Set()
  assert.equal(claimIncomingMessageSound('message-1', claimed), true)
  assert.equal(claimIncomingMessageSound('message-1', claimed), false)
  assert.equal(claimIncomingMessageSound('', claimed), false)

  for (let index = 2; index <= 300; index += 1) {
    assert.equal(claimIncomingMessageSound(`message-${index}`, claimed), true)
  }
  assert.equal(claimed.size, 256)
  assert.equal(claimed.has('message-1'), false)
  assert.equal(claimed.has('message-300'), true)
})

test('legacy silent choices migrate to disabled sounds without losing the model', () => {
  assert.deepEqual(
    normalizeBrowserSoundPreferences({
      ringtone: 'silent',
      incomingMessage: 'silent',
      outgoingMessage: false,
      waiting: false
    }),
    {
      ringtone: 'orion',
      ringtoneEnabled: false,
      incomingMessage: 'pixie-dust',
      incomingMessageEnabled: false,
      outgoingMessage: 'pizzicato',
      outgoingMessageEnabled: false,
      waiting: false
    }
  )

  assert.deepEqual(
    normalizeBrowserSoundPreferences({
      ringtone: 'digital',
      ringtoneEnabled: false,
      incomingMessage: 'drip',
      incomingMessageEnabled: true,
      outgoingMessage: 'voila',
      outgoingMessageEnabled: false,
      waiting: true
    }),
    {
      ringtone: 'digital',
      ringtoneEnabled: false,
      incomingMessage: 'drip',
      incomingMessageEnabled: true,
      outgoingMessage: 'voila',
      outgoingMessageEnabled: false,
      waiting: true
    }
  )
})

test('the curated AOSP ringtone catalog is complete, unique, and compact', async () => {
  assert.equal(ringtoneCatalog.length, 20)
  assert.equal(new Set(ringtoneCatalog.map(ringtone => ringtone.id)).size, 20)
  assert.equal(new Set(ringtoneCatalog.map(ringtone => ringtone.name)).size, 20)
  assert.equal(ringtoneCatalog[0].id, 'orion')

  let totalBytes = 0
  for (const ringtone of ringtoneCatalog) {
    const asset = await stat(fileURLToPath(ringtone.source))
    assert.ok(asset.size > 0, `${ringtone.name} must contain audio data`)
    totalBytes += asset.size
  }
  assert.ok(totalBytes < 2_000_000, `ringtone catalog is unexpectedly large: ${totalBytes}`)
})

test('SMS received and sent sounds share a compact AOSP notification catalog', async () => {
  assert.equal(notificationCatalog.length, 10)
  assert.equal(
    new Set(notificationCatalog.map(notification => notification.id)).size,
    notificationCatalog.length
  )
  assert.equal(
    new Set(notificationCatalog.map(notification => notification.name)).size,
    notificationCatalog.length
  )

  let totalBytes = 0
  for (const notification of notificationCatalog) {
    const asset = await stat(fileURLToPath(notification.source))
    assert.ok(asset.size > 0, `${notification.name} must contain audio data`)
    totalBytes += asset.size
  }
  assert.ok(
    totalBytes < 500_000,
    `notification catalog is unexpectedly large: ${totalBytes}`
  )
})

test('live call and SMS paths own sound playback instead of view components', async () => {
  const [calls, messages, workspace] = await Promise.all([
    readFile(new URL('../src/state/call.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/messageRuntime.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8')
  ])

  assert.match(calls, /function acceptSession[\s\S]*syncCallSounds\(session\)/)
  assert.match(calls, /shutdownCallRuntime[\s\S]*syncCallSounds\(null\)/)
  assert.match(
    messages,
    /if \(allowNotification\)[\s\S]*playIncomingMessageSound\(event\.message_id\)/
  )
  assert.match(
    workspace,
    /const sent = await gateway\.sendMessage\(input\)[\s\S]*playOutgoingMessageSound\(\)/
  )
})

test('audio settings expose persisted devices and browser-local communication sounds', async () => {
  const [settings, form, devices, menu, shell, notice, ignore, dockerIgnore] =
    await Promise.all([
      readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/components/AudioSettingsForm.vue', import.meta.url), 'utf8'),
      readFile(
        new URL('../src/components/AudioDeviceControls.vue', import.meta.url),
        'utf8'
      ),
      readFile(new URL('../src/components/AudioSettingsMenu.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/components/AppShell.vue', import.meta.url), 'utf8'),
      readFile(new URL('../../NOTICE.md', import.meta.url), 'utf8'),
      readFile(new URL('../../.gitignore', import.meta.url), 'utf8'),
      readFile(new URL('../../.dockerignore', import.meta.url), 'utf8')
    ])

  assert.match(settings, /id: 'audio'/)
  assert.match(settings, /<AudioSettingsForm/)
  assert.match(form, /ringtoneCatalog/)
  assert.match(form, /notificationCatalog/)
  assert.match(form, /setIncomingMessageSound/)
  assert.match(form, /setIncomingMessageSoundEnabled/)
  assert.match(form, /setOutgoingMessageSound/)
  assert.match(form, /setOutgoingMessageSoundEnabled/)
  assert.match(form, /setRingtoneEnabled/)
  assert.match(form, /setWaitingSound/)
  assert.equal((form.match(/role="switch"/g) || []).length, 4)
  assert.doesNotMatch(form, /<option value="silent"/)
  assert.match(devices, /setSelectedAudioInput/)
  assert.match(devices, /setSelectedAudioOutput/)
  assert.match(menu, /<AudioDeviceControls compact/)
  assert.match(shell, /initializeBrowserSounds\(\)/)
  assert.match(shell, /shutdownBrowserSounds\(\)/)
  assert.match(notice, /Android Open Source Project ringtones/)
  assert.match(notice, /SMS alert sounds/)
  assert.match(notice, /Apache License 2\.0/)
  assert.match(ignore, /!\/web\/src\/assets\/ringtones\/\*\.ogg/)
  assert.match(ignore, /!\/web\/src\/assets\/notifications\/\*\.ogg/)
  assert.match(dockerIgnore, /!web\/src\/assets\/ringtones\/\*\.ogg/)
  assert.match(dockerIgnore, /!web\/src\/assets\/notifications\/\*\.ogg/)
})
