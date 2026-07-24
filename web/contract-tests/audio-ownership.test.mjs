import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const devicePanel = new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url)
const diagnosticsPanel = new URL('../src/components/DiagnosticsPanel.vue', import.meta.url)

test('module voice settings show an observed bearer or a capability-confirmed path', async () => {
  const source = await readFile(devicePanel, 'utf8')

  assert.match(
    source,
    /\{ id: 'voice', label: '呼叫控制', capability: capabilities\.voice \}/
  )
  assert.doesNotMatch(source, /\{ id: 'voice', label: 'Voice'/)
  assert.doesNotMatch(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /<strong>通话路径<\/strong>/)
  assert.match(source, /case 'volte':[\s\S]*return 'VoLTE'/)
  assert.match(source, /case 'vowifi':[\s\S]*return 'VoWiFi'/)
  assert.match(source, /case 'gsm':[\s\S]*case 'cs':[\s\S]*return 'GSM \/ CS'/)
  assert.match(
    source,
    /volte\?\.modem_capability_known[\s\S]*volte\.modem_capability_enabled[\s\S]*\? 'VoLTE'[\s\S]*: 'GSM'/
  )
  assert.doesNotMatch(source, /'无通话'/)
  assert.doesNotMatch(source, /'待接通'/)
  assert.match(source, /\{\{ selectedCallPathLabel \}\}/)
  assert.match(source, /\.includes\(session\.line_key\)/)
})

test('browser audio is a global diagnostic and enumeration does not request a microphone', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /\{ name: '呼叫控制', available: line\.capabilities\?\.voice === true \}/)
  assert.doesNotMatch(source, /\{ name: '语音通话'/)
  assert.match(source, /\{ name: '浏览器音频', available: capabilities\.media \}/)
  assert.doesNotMatch(source, /voice_interface/)
  assert.match(source, /refreshAudioDevices\(\)/)
  assert.match(source, /typeof RTCPeerConnection !== 'undefined'/)
  assert.doesNotMatch(source, /getUserMedia\(\{/)
})
