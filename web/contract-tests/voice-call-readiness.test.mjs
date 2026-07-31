import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import {
  bootstrapResource,
  capabilityReason,
  lineCanPlaceVoiceCall
} from '../src/state/workspace.ts'

function line(dial, media) {
  return {
    id: 'line-test',
    capabilities: { dial, media }
  }
}

test('placing a voice call requires dial control and voice media on the same line', () => {
  assert.equal(lineCanPlaceVoiceCall(line(true, true)), true)
  assert.equal(lineCanPlaceVoiceCall(line(true, false)), false)
  assert.equal(lineCanPlaceVoiceCall(line(false, true)), false)
  assert.equal(lineCanPlaceVoiceCall(line(false, false)), false)
})

test('global dial availability rejects control-only and media-only lines', () => {
  const original = bootstrapResource.data
  try {
    bootstrapResource.data = {
      capabilities: {
        agent_connected: true,
        dial: true,
        message: true,
        webrtc_audio: true,
        device_control: false,
        volte_control: false,
        vowifi_control: false
      },
      lines: [line(true, false)]
    }
    assert.equal(
      capabilityReason('dial'),
      'Call control is available, but browser audio is unavailable'
    )

    bootstrapResource.data.lines = [line(false, true)]
    assert.equal(
      capabilityReason('dial'),
      'No line is currently available for voice calling'
    )

    bootstrapResource.data.lines = [line(true, true)]
    assert.equal(capabilityReason('dial'), '')

    bootstrapResource.data.capabilities.webrtc_audio = false
    assert.equal(
      capabilityReason('dial'),
      'Call control is available, but browser audio is unavailable'
    )
  } finally {
    bootstrapResource.data = original
  }
})

test('control-only incoming calls keep reject available and disable answer in pale green', async () => {
  const surface = await readFile(
    new URL('../src/components/CallSurface.vue', import.meta.url),
    'utf8'
  )

  assert.match(
    surface,
    /bootstrapResource\.data\?\.capabilities\.webrtc_audio !== true[\s\S]*?lineSupports\(line\.value, 'media'\) !== true[\s\S]*?t\('calls\.answerAudioUnavailable'\)/
  )
  assert.match(
    surface,
    /class="call-button call-button--answer"[\s\S]*?:disabled="callState\.busy \|\| Boolean\(answerUnavailable\)"/
  )
  assert.match(
    surface,
    /\.call-button--answer:disabled\s*\{[\s\S]*?background: var\(--accent-soft\);[\s\S]*?opacity: 1;/
  )
  assert.match(
    surface,
    /:disabled="callState\.busy \|\| Boolean\(rejectUnavailable\)"[\s\S]*?@click="rejectCall"/
  )
})

test('dialer keeps assigned lines visible while disabling lines without browser voice', async () => {
  const [dialer, selector] = await Promise.all([
    readFile(new URL('../src/components/DialerPanel.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/LineSelector.vue', import.meta.url), 'utf8')
  ])

  assert.match(dialer, /const dialLines = computed\(\(\) =>[\s\S]*?filter\(lineCanPlaceVoiceCall\)/)
  assert.match(
    dialer,
    /const unavailableDialLineIDs = computed\(\(\) =>[\s\S]*?!voiceCallingAvailable\.value \|\| !lineCanPlaceVoiceCall\(line\)/
  )
  assert.match(
    dialer,
    /const voiceCallingAvailable = computed\([\s\S]*?capabilities\.dial === true[\s\S]*?capabilities\.webrtc_audio === true/
  )
  assert.match(dialer, /<LineSelector[\s\S]*?:lines="lines"/)
  assert.match(dialer, /:disabled-values="unavailableDialLineIDs"/)
  assert.match(
    dialer,
    /:placeholder="dialLines\.length > 0 \? t\('dialer\.selectLine'\) : t\('dialer\.noLines'\)"/
  )
  assert.match(selector, /:aria-disabled="option\.disabled"/)
  assert.match(selector, /if \(option\.disabled\) return/)
  assert.match(
    selector,
    /\.line-selector__option\.is-disabled\s*\{[\s\S]*?background: var\(--surface-subtle\);[\s\S]*?opacity: 0\.68;/
  )
})
