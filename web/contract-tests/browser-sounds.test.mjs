import assert from 'node:assert/strict'
import { stat, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

import {
  callSoundMode,
  claimIncomingMessageSound,
  normalizeBrowserSoundPreferences,
  notificationCatalog,
  ringtoneCatalog,
  waitingToneSource
} from '../src/state/browserSounds.ts'
import {
  applySelectedAudioOutput,
  audioState,
  markAudioOutputInactive,
  normalizeAudioLevels
} from '../src/state/audio.ts'

const incomingCall = {
  id: 'call-incoming',
  line_id: 'line-main',
  direction: 'incoming',
  remote_number: '+818012345678',
  phase: 'ringing',
  control_state: 'available',
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

test('browser audio levels are bounded and retain independent channels', () => {
  assert.deepEqual(normalizeAudioLevels(null), {
    microphoneGain: 100,
    callVolume: 100,
    ringAlertsVolume: 100,
    recordingPlaybackVolume: 100
  })
  assert.deepEqual(
    normalizeAudioLevels({
      microphoneGain: 275,
      callVolume: -20,
      ringAlertsVolume: 55.4,
      recordingPlaybackVolume: 35
    }),
    {
      microphoneGain: 200,
      callVolume: 0,
      ringAlertsVolume: 55,
      recordingPlaybackVolume: 35
    }
  )
})

test('call sound output routing survives inactive call media and concurrent sounds', async () => {
  const originalOutputID = audioState.selectedOutputID
  const originalStatus = audioState.outputRoutingStatus
  const originalError = audioState.outputRoutingError
  audioState.selectedOutputID = ''

  let releaseWaitingTone
  let releaseNotification
  const waitingTone = {
    setSinkId: () =>
      new Promise(resolve => {
        releaseWaitingTone = resolve
      })
  }
  const notification = {
    setSinkId: () =>
      new Promise(resolve => {
        releaseNotification = resolve
      })
  }

  try {
    const waitingRouting = applySelectedAudioOutput(waitingTone)
    const notificationRouting = applySelectedAudioOutput(notification)
    markAudioOutputInactive()
    releaseNotification()
    releaseWaitingTone()

    assert.deepEqual(await Promise.all([waitingRouting, notificationRouting]), [
      true,
      true
    ])
  } finally {
    audioState.selectedOutputID = originalOutputID
    audioState.outputRoutingStatus = originalStatus
    audioState.outputRoutingError = originalError
  }
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

test('waiting calls and previews use one bundled browser-compatible audio asset', async () => {
  const asset = await stat(fileURLToPath(waitingToneSource))
  const implementation = await readFile(
    new URL('../src/state/browserSounds.ts', import.meta.url),
    'utf8'
  )

  assert.match(waitingToneSource, /\.ogg$/)
  assert.ok(asset.size > 0, 'waiting tone must contain audio data')
  assert.ok(asset.size < 50_000, `waiting tone is unexpectedly large: ${asset.size}`)
  assert.doesNotMatch(implementation, /createObjectURL|audio\/wav/)
  assert.equal(
    implementation.match(/source: waitingToneSource/g)?.length,
    2,
    'call playback and settings preview must use the same waiting tone'
  )
})

test('live call and SMS paths own sound playback instead of view components', async () => {
  const [calls, messages, workspace] = await Promise.all([
    readFile(new URL('../src/state/call.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/messageRuntime.ts', import.meta.url), 'utf8'),
    readFile(new URL('../src/state/workspace.ts', import.meta.url), 'utf8')
  ])

  assert.match(
    calls,
    /function applyForegroundSession[\s\S]*syncCallSounds\([\s\S]*owned[\s\S]*incomingAvailable[\s\S]*\? session : null/
  )
  assert.match(calls, /shutdownCallRuntime[\s\S]*syncCallSounds\(null\)/)
  assert.match(
    messages,
    /if \(shouldAlertIncomingMessage\(event, delivery\)\)[\s\S]*playIncomingMessageSound\(event\.message_id\)/
  )
  assert.match(
    workspace,
    /const sent = await gateway\.sendMessage\(input\)[\s\S]*playOutgoingMessageSound\(\)/
  )
})

test('audio settings expose persisted devices and browser-local communication sounds', async () => {
  const [
    settings,
    form,
    devices,
    menu,
    shell,
    audio,
    callMedia,
    dtmf,
    recordings,
    recordingList,
    notice,
    ignore,
    dockerIgnore
  ] =
    await Promise.all([
      readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/components/AudioSettingsForm.vue', import.meta.url), 'utf8'),
      readFile(
        new URL('../src/components/AudioDeviceControls.vue', import.meta.url),
        'utf8'
      ),
      readFile(new URL('../src/components/AudioSettingsMenu.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/components/AppShell.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/state/audio.ts', import.meta.url), 'utf8'),
      readFile(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8'),
      readFile(new URL('../src/state/dtmfAudio.ts', import.meta.url), 'utf8'),
      readFile(new URL('../src/views/RecordingsView.vue', import.meta.url), 'utf8'),
      readFile(new URL('../src/components/RecordingList.vue', import.meta.url), 'utf8'),
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
  assert.equal((form.match(/type="range"/g) || []).length, 4)
  assert.match(form, /setMicrophoneGain/)
  assert.match(form, /setCallVolume/)
  assert.match(form, /setRingAlertsVolume/)
  assert.match(form, /setRecordingPlaybackVolume/)
  assert.doesNotMatch(form, /<option value="silent"/)
  assert.match(devices, /setSelectedAudioInput/)
  assert.match(devices, /setSelectedAudioOutput/)
  assert.match(menu, /<AudioDeviceControls compact/)
  assert.match(shell, /initializeBrowserSounds\(\)/)
  assert.match(shell, /shutdownBrowserSounds\(\)/)
  assert.match(audio, /modemdeck\.audio\.levels\.v1/)
  assert.match(callMedia, /createMediaStreamDestination\(\)/)
  assert.match(callMedia, /createDynamicsCompressor\(\)/)
  assert.match(callMedia, /audioState\.callVolume \/ 100/)
  assert.match(dtmf, /audioState\.callVolume \/ 100/)
  assert.match(recordings, /audioState\.recordingPlaybackVolume \/ 100/)
  assert.match(recordingList, /audioState\.recordingPlaybackVolume \/ 100/)
  assert.match(notice, /Android Open Source Project ringtones/)
  assert.match(notice, /SMS alert sounds/)
  assert.match(notice, /Apache License 2\.0/)
  assert.match(ignore, /!\/web\/src\/assets\/ringtones\/\*\.ogg/)
  assert.match(ignore, /!\/web\/src\/assets\/notifications\/\*\.ogg/)
  assert.match(dockerIgnore, /!web\/src\/assets\/ringtones\/\*\.ogg/)
  assert.match(dockerIgnore, /!web\/src\/assets\/notifications\/\*\.ogg/)
})
