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

test('browser audio is a global diagnostic with explicit microphone access states', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /<strong>浏览器音频<\/strong>/)
  assert.match(source, /\{ name: '呼叫控制', available: line\.capabilities\?\.voice === true \}/)
  assert.doesNotMatch(source, /\{ name: '语音通话'/)
  assert.match(source, /\{ name: '模组音频桥接', available: capabilities\.media \}/)
  assert.doesNotMatch(source, /\{ name: '浏览器音频', available: capabilities\.media \}/)
  assert.doesNotMatch(source, /voice_interface/)
  assert.match(source, /refreshAudioDevices\(\)/)
  assert.match(source, /audioState\.microphoneAccessStatus === 'granted'/)
  assert.match(source, /case 'insecure-context':[\s\S]*需要 HTTPS 安全上下文/)
  assert.match(source, /case 'prompt':[\s\S]*等待麦克风授权/)
  assert.match(source, /case 'pending':[\s\S]*正在请求麦克风权限/)
  assert.match(source, /case 'denied':[\s\S]*麦克风权限已被阻止/)
  assert.match(source, /case 'no-device':[\s\S]*未检测到麦克风/)
  assert.match(source, /<LoaderCircle[\s\S]*microphoneAccessStatus === 'pending'/)
  assert.match(source, /<LockKeyhole[\s\S]*microphoneAccessStatus === 'denied'/)
  assert.match(source, /<MicOff[\s\S]*microphoneAccessStatus === 'no-device'/)
  assert.match(source, /call\.phase !== 'active'\) return '接通后建立'/)
  assert.match(source, /!call\.media_available\) return '模组音频不可用'/)
  assert.match(source, /'is-unavailable': callAudioUnavailable\(call\)/)
  assert.doesNotMatch(source, /getUserMedia\(\{/)
})
