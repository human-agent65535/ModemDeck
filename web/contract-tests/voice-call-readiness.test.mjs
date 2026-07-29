import assert from 'node:assert/strict'
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
    assert.notEqual(capabilityReason('dial'), '')

    bootstrapResource.data.lines = [line(false, true)]
    assert.notEqual(capabilityReason('dial'), '')

    bootstrapResource.data.lines = [line(true, true)]
    assert.equal(capabilityReason('dial'), '')

    bootstrapResource.data.capabilities.webrtc_audio = false
    assert.notEqual(capabilityReason('dial'), '')
  } finally {
    bootstrapResource.data = original
  }
})
