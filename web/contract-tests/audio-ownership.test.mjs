import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const devicePanel = new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url)
const diagnosticsPanel = new URL('../src/components/DiagnosticsPanel.vue', import.meta.url)

test('module voice settings show cellular bearer instead of browser audio', async () => {
  const source = await readFile(devicePanel, 'utf8')

  assert.doesNotMatch(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /<strong>语音承载<\/strong>/)
  assert.match(source, /case 'volte':[\s\S]*return 'VoLTE'/)
  assert.match(source, /case 'vowifi':[\s\S]*return 'VoWiFi'/)
  assert.match(source, /case 'gsm':[\s\S]*case 'cs':[\s\S]*return 'GSM \/ CS'/)
  assert.match(source, /\{\{ selectedCallBearer \|\| '通话时确认' \}\}/)
  assert.match(source, /\.includes\(session\.line_key\)/)
})

test('browser audio is a global diagnostic and enumeration does not request a microphone', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /refreshAudioDevices\(\)/)
  assert.match(source, /typeof RTCPeerConnection !== 'undefined'/)
  assert.doesNotMatch(source, /getUserMedia\(\{/)
})
