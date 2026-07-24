import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const devicePanel = new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url)
const diagnosticsPanel = new URL('../src/components/DiagnosticsPanel.vue', import.meta.url)

test('module voice settings always show the configured or observed call path', async () => {
  const source = await readFile(devicePanel, 'utf8')

  assert.doesNotMatch(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /<strong>通话路径<\/strong>/)
  assert.match(source, /case 'volte':[\s\S]*return 'VoLTE'/)
  assert.match(source, /case 'vowifi':[\s\S]*return 'VoWiFi'/)
  assert.match(source, /case 'gsm':[\s\S]*case 'cs':[\s\S]*return 'GSM \/ CS'/)
  assert.match(
    source,
    /hardware\.value\?\.volte\.policy_known[\s\S]*hardware\.value\.volte\.policy === 'enabled'[\s\S]*\? 'VoLTE'[\s\S]*: 'GSM'/
  )
  assert.doesNotMatch(source, /'无通话'/)
  assert.doesNotMatch(source, /'待接通'/)
  assert.match(source, /\{\{ selectedCallPathLabel \}\}/)
  assert.match(source, /\.includes\(session\.line_key\)/)
})

test('browser audio is a global diagnostic and enumeration does not request a microphone', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /refreshAudioDevices\(\)/)
  assert.match(source, /typeof RTCPeerConnection !== 'undefined'/)
  assert.doesNotMatch(source, /getUserMedia\(\{/)
})
