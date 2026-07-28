import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const devicePanel = new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url)
const diagnosticsPanel = new URL('../src/components/DiagnosticsPanel.vue', import.meta.url)

test('module voice settings show an observed bearer or a capability-confirmed path', async () => {
  const source = await readFile(devicePanel, 'utf8')

  assert.match(
    source,
    /\{ id: 'voice', label: t\('diagnostics\.callControl'\), capability: capabilities\.voice \}/
  )
  assert.doesNotMatch(source, /\{ id: 'voice', label: 'Voice'/)
  assert.doesNotMatch(source, /diagnostics\.browserAudio/)
  assert.match(source, /t\('device\.callPath'\)/)
  assert.match(source, /case 'volte':[\s\S]*return 'VoLTE'/)
  assert.match(source, /case 'vowifi':[\s\S]*return 'VoWiFi'/)
  assert.match(source, /case 'gsm':[\s\S]*case 'cs':[\s\S]*return 'GSM \/ CS'/)
  assert.match(
    source,
    /const selectedLineVoLTEConfigured = computed[\s\S]*volte\.policy !== 'enabled'[\s\S]*provisioning\.ims_profile_reported[\s\S]*provisioning\.ims_profile_present/
  )
  assert.match(
    source,
    /return selectedLineVoLTEConfigured\.value \? 'VoLTE' : 'GSM'/
  )
  assert.match(source, /t\('device\.carrierConfiguration'\)/)
  assert.match(source, /t\('device\.imsProfile'\)/)
  assert.doesNotMatch(source, /'无通话'/)
  assert.doesNotMatch(source, /'待接通'/)
  assert.match(source, /\{\{ selectedCallPathLabel \}\}/)
  assert.match(source, /lineKey\(line\) === session\.line_id/)
})

test('browser audio is a global diagnostic with explicit microphone access states', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /t\('diagnostics\.browserAudio'\)/)
  assert.match(
    source,
    /\{ name: t\('diagnostics\.callControl'\), available: lineHasCallControl\(line\) \}/
  )
  assert.match(
    source,
    /\{ name: t\('diagnostics\.modemMediaRoute'\), available: line\.capabilities\?\.media === true \}/
  )
  assert.doesNotMatch(source, /\{ name: '语音通话'/)
  assert.match(
    source,
    /\{ name: t\('diagnostics\.mediaBridge'\), available: capabilities\.media \}/
  )
  assert.doesNotMatch(source, /\{ name: '浏览器音频', available: capabilities\.media \}/)
  assert.doesNotMatch(source, /voice_interface/)
  assert.match(source, /refreshAudioDevices\(\)/)
  assert.match(source, /audioState\.microphoneAccessStatus === 'granted'/)
  assert.match(source, /case 'insecure-context':[\s\S]*t\('diagnostics\.httpsRequired'\)/)
  assert.match(
    source,
    /case 'prompt':[\s\S]*t\('diagnostics\.microphonePermissionWaiting'\)/
  )
  assert.match(
    source,
    /case 'pending':[\s\S]*t\('diagnostics\.microphonePermissionRequesting'\)/
  )
  assert.match(source, /case 'denied':[\s\S]*t\('diagnostics\.microphoneBlocked'\)/)
  assert.match(source, /case 'no-device':[\s\S]*t\('diagnostics\.noMicrophone'\)/)
  assert.match(source, /<LoaderCircle[\s\S]*microphoneAccessStatus === 'pending'/)
  assert.match(source, /<LockKeyhole[\s\S]*microphoneAccessStatus === 'denied'/)
  assert.match(source, /<MicOff[\s\S]*microphoneAccessStatus === 'no-device'/)
  assert.match(
    source,
    /call\.phase !== 'active'\) return t\('diagnostics\.audioAfterConnect'\)/
  )
  assert.match(
    source,
    /!call\.media_available\) return t\('diagnostics\.modemAudioUnavailable'\)/
  )
  assert.match(source, /'is-unavailable': callAudioUnavailable\(call\)/)
  assert.doesNotMatch(source, /getUserMedia\(\{/)
})
