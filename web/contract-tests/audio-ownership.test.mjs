import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const devicePanel = new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url)
const diagnosticsPanel = new URL('../src/components/DiagnosticsPanel.vue', import.meta.url)

test('module voice settings separate the user policy from an observed bearer', async () => {
  const source = await readFile(devicePanel, 'utf8')
  const diagnosticsSource = await readFile(diagnosticsPanel, 'utf8')

  assert.doesNotMatch(
    source,
    /\{ id: 'voice', label: t\('diagnostics\.callControl'\), capability: capabilities\.voice \}/
  )
  assert.match(
    diagnosticsSource,
    /\{ id: 'voice', label: t\('diagnostics\.callControl'\), capability: capabilities\.voice \}/
  )
  assert.doesNotMatch(source, /\{ id: 'voice', label: 'Voice'/)
  assert.doesNotMatch(source, /diagnostics\.browserAudio/)
  assert.match(source, /t\('device\.voiceMode'\)/)
  assert.match(source, /t\('device\.currentCallBearer'\)/)
  assert.match(source, /case 'volte':[\s\S]*return 'VoLTE'/)
  assert.match(source, /case 'vowifi':[\s\S]*return 'VoWiFi'/)
  assert.match(source, /case 'gsm':[\s\S]*case 'cs':[\s\S]*return 'GSM \/ CS'/)
  assert.match(
    source,
    /const selectedLineVoLTEEnabled = computed[\s\S]*volte\?\.policy_known === true && volte\.policy === 'enabled'/
  )
  assert.match(
    source,
    /return selectedLineVoLTEEnabled\.value \? t\('device\.voltePreferred'\) : 'GSM'/
  )
  assert.match(
    source,
    /const selectedLineVoLTEAvailable = computed\([\s\S]*selectedLineVoLTEEnabled\.value &&[\s\S]*modem_capability_enabled === true/
  )
  assert.match(
    source,
    /:class="\{ 'is-available': selectedLineVoLTEAvailable \}"/
  )
  const voiceModeStart = source.indexOf('const selectedVoiceModeLabel = computed')
  const voiceModeEnd = source.indexOf('\n})', voiceModeStart)
  assert.ok(voiceModeStart >= 0 && voiceModeEnd > voiceModeStart)
  assert.doesNotMatch(source.slice(voiceModeStart, voiceModeEnd), /policy_known/)
  const policyStart = source.indexOf('const selectedLineVoLTEEnabled = computed')
  const policyEnd = source.indexOf('\n})', policyStart)
  assert.ok(policyStart >= 0 && policyEnd > policyStart)
  assert.doesNotMatch(source.slice(policyStart, policyEnd), /provisioning|modem_capability/)
  assert.match(source, /t\('device\.carrierConfiguration'\)/)
  assert.match(source, /t\('device\.imsProfile'\)/)
  assert.match(source, /\{\{ selectedVoiceModeTitle \}\}/)
  assert.match(source, /\{\{ selectedVoiceModeLabel \}\}/)
  assert.match(
    source,
    /callState\.sessions\.find\([\s\S]*session\.line_id === key/
  )
})

test('VoLTE switch follows the selected line policy and discovery only gates writes', async () => {
  const source = await readFile(devicePanel, 'utf8')

  assert.match(
    source,
    /watch\([\s\S]*selectedLineID,[\s\S]*hardware\.value\?\.volte\.policy_known,[\s\S]*hardware\.value\?\.volte\.policy[\s\S]*voltePolicyDraft\.value =/
  )
  assert.match(
    source,
    /:disabled="hardwareBusy \|\| !hardware\.capabilities\.volte\.writable"/
  )
  assert.match(
    source,
    /return volte\.policy === 'enabled' \? t\('device\.enabled'\) : t\('device\.disabled'\)/
  )
})

test('browser audio is a global diagnostic with explicit microphone access states', async () => {
  const source = await readFile(diagnosticsPanel, 'utf8')

  assert.match(source, /t\('diagnostics\.browserAudio'\)/)
  assert.doesNotMatch(source, /function lineCapabilities/)
  assert.doesNotMatch(source, /function agentCapabilities/)
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
