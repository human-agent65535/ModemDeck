import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const signalBarsSource = readFileSync(
  new URL('../src/components/SignalBars.vue', import.meta.url),
  'utf8'
)

function functionBody(name, nextName) {
  const start = source.indexOf(`async function ${name}`)
  const end = source.indexOf(`async function ${nextName}`, start)
  assert.ok(start >= 0, `${name} 不存在`)
  assert.ok(end > start, `${name} 边界无效`)
  return source.slice(start, end)
}

test('flight mode is the inverse of the existing radio operation', () => {
  const body = functionBody('changeRadio', 'applyDataConnection')

  assert.match(body, /const flightModeEnabled = control\.checked/)
  assert.match(body, /control\.checked = Boolean\(hardware\.value\?\.flight_mode\)/)
  assert.match(
    body,
    /setRadioEnabled\(selectedLineID\.value, !flightModeEnabled\)/
  )
  assert.match(source, /:checked="hardware\.flight_mode"/)
  assert.match(source, /<Plane :size="18" \/><h4>\{\{ t\('device\.radio'\) \}\}<\/h4>/)
  assert.doesNotMatch(source, /<h4>蜂窝射频<\/h4>/)
})

test('mobile data is unavailable while airplane mode or radio state prevents it', () => {
  const connectBody = functionBody('applyDataConnection', 'stopDataConnection')
  const switchBody = functionBody('changeDataConnection', 'applyVoLTE')
  const networkStart = source.indexOf("<template v-else-if=\"activeTab === 'network'\">")
  const networkEnd = source.indexOf("<template v-else-if=\"activeTab === 'sim'\">", networkStart)
  const networkSection = source.slice(networkStart, networkEnd)
  const ipModeStart = networkSection.indexOf('<fieldset')
  const ipModeEnd = networkSection.indexOf('</fieldset>', ipModeStart)
  const ipModeSection = networkSection.slice(ipModeStart, ipModeEnd)

  assert.match(
    connectBody,
    /!selectedLineID\.value \|\| !dataConnectionWritable\.value/
  )
  assert.match(
    connectBody,
    /connectData\(selectedLineID\.value, apn\.value, ipFamily\.value\)/
  )
  assert.match(
    source,
    /current\.radio\.enabled_known[\s\S]*current\.radio\.enabled[\s\S]*current\.flight_mode_known[\s\S]*!current\.flight_mode/
  )
  assert.match(switchBody, /enabled \? await applyDataConnection\(\) : await stopDataConnection\(\)/)
  assert.match(source, /<strong>\{\{ t\('device\.mobileData'\) \}\}<\/strong>/)
  assert.match(source, /:checked="hardware\.network_enabled"/)
  assert.match(source, /:disabled="hardwareBusy \|\| !dataConnectionWritable"/)
  assert.match(source, /t\('runtime\.turnOffFlightModeForData'\)/)
  assert.match(source, /t\('runtime\.waitForRadioRecoveryForData'\)/)
  assert.match(source, /<span>APN<\/span>/)
  assert.match(networkSection, /<legend>\{\{ t\('device\.ipMode'\) \}\}<\/legend>/)
  assert.match(networkSection, /type="radio" value="ipv4"/)
  assert.match(networkSection, /type="radio" value="ipv6"/)
  assert.match(networkSection, /type="radio" value="ipv4v6"/)
  assert.doesNotMatch(ipModeSection, /value="auto"/)
  assert.match(source, /const ipFamily = ref<IPFamily>\('ipv4v6'\)/)
  assert.match(
    source,
    /connection\.ip_family\s*:\s*'ipv4v6'/
  )
})

test('airplane mode and zero signal have distinct icons', () => {
  assert.match(signalBarsSource, /import \{ Plane \} from '@lucide\/vue'/)
  assert.match(signalBarsSource, /v-if="flightMode"/)
  assert.match(signalBarsSource, /if \(props\.flightMode\) return 'is-flight-mode'/)
  assert.doesNotMatch(signalBarsSource, /\.signal-bars\.is-zero::after/)
})

test('empty APN remains automatic and only displays a server-resolved value', () => {
  assert.match(
    source,
    /const apnPlaceholder = computed\(\(\) => automaticAPNLabel\(hardware\.value\?\.automatic_apn\)\)/
  )
  assert.match(
    source,
    /function automaticAPNLabel\(value\?: string\): string \{[\s\S]*t\('device\.automaticAPN', \{ apn: resolvedAPN \}\)[\s\S]*t\('device\.automatic'\)/
  )
  assert.match(source, /v-model\.trim="apn"[\s\S]*:placeholder="apnPlaceholder"/)
  assert.match(
    source,
    /watch\(\s*selectedLineID,\s*\(\) => \{\s*apn\.value = ''\s*\},\s*\{ immediate: true \}\s*\)/
  )
  assert.doesNotMatch(source, /apn\.value = connection\?\.apn/)
  assert.doesNotMatch(source, /automaticAPNLabel\([^)]*(?:operator|imsi|iccid)/)
})

test('network keeps a compact bearer status and folds full profiles into details', () => {
  const networkStart = source.indexOf("<template v-else-if=\"activeTab === 'network'\">")
  const networkEnd = source.indexOf("<template v-else-if=\"activeTab === 'sim'\">", networkStart)
  const networkSection = source.slice(networkStart, networkEnd)

  assert.match(networkSection, /class="data-connection-status"/)
  assert.match(networkSection, /dataConnectionStatusLabel/)
  assert.doesNotMatch(networkSection, /v-for="connection in hardware\.data_connections"/)
  assert.match(
    networkSection,
    /<details class="advanced-profiles">[\s\S]*t\('device\.advancedProfiles'\)/
  )
  assert.doesNotMatch(networkSection, /<details class="advanced-profiles"[^>]*\sopen/)
  assert.match(networkSection, /@submit\.prevent="saveProfile"/)
  assert.match(networkSection, /@click\.stop="deleteProfile\(profile\)"/)
})

test('VoWiFi is status-only while VoLTE uses the shared binary switch', () => {
  const networkStart = source.indexOf("<template v-else-if=\"activeTab === 'network'\">")
  const networkEnd = source.indexOf("<template v-else-if=\"activeTab === 'sim'\">", networkStart)
  const networkSection = source.slice(networkStart, networkEnd)
  const voiceStart = source.indexOf("<template v-else-if=\"activeTab === 'voice'\">")
  const voiceEnd = source.indexOf('<template v-else>', voiceStart)
  const voiceSection = source.slice(voiceStart, voiceEnd)
  const volteStart = source.indexOf('<h4>VoLTE</h4>')
  const volteEnd = source.indexOf('</section>', volteStart)
  const volteSection = source.slice(volteStart, volteEnd)

  assert.doesNotMatch(networkSection, />VoWiFi</)
  assert.match(voiceSection, /<strong>VoWiFi<\/strong>/)
  assert.match(voiceSection, /capabilityStatus\(hardware\.capabilities\.vowifi/)
  assert.match(voiceSection, /<strong>\{\{ t\('device\.callPath'\) \}\}<\/strong>/)
  assert.match(voiceSection, /:class="\{ 'is-available': voiceAvailable \}"/)
  assert.doesNotMatch(
    voiceSection.slice(
      voiceSection.indexOf('<strong>VoWiFi</strong>'),
      voiceSection.indexOf('</div>', voiceSection.indexOf('<strong>VoWiFi</strong>'))
    ),
    /role="switch"/
  )

  assert.ok(volteStart >= 0)
  assert.match(volteSection, /role="switch"/)
  assert.match(volteSection, /:checked="voltePolicyDraft === 'enabled'"/)
  assert.match(volteSection, /@change="applyVoLTE"/)
  assert.match(volteSection, /!hardware\.capabilities\.volte\.writable/)
  assert.match(volteSection, /volteStatusDetail/)
  assert.doesNotMatch(volteSection, /<select/)
})

test('incoming call override uses a compact three-state segmented control', () => {
  const voiceStart = source.indexOf("<template v-else-if=\"activeTab === 'voice'\">")
  const voiceEnd = source.indexOf('<template v-else>', voiceStart)
  const voiceSection = source.slice(voiceStart, voiceEnd)
  const incomingStart = voiceSection.indexOf("t('device.incomingCalls')")
  const incomingEnd = voiceSection.indexOf('</section>', incomingStart)
  const incomingSection = voiceSection.slice(incomingStart, incomingEnd)

  assert.match(incomingSection, /class="incoming-policy"/)
  assert.match(incomingSection, /type="radio"\s+value="follow_global"/)
  assert.match(incomingSection, /type="radio"\s+value="receive"/)
  assert.match(incomingSection, /type="radio"\s+value="do_not_disturb"/)
  assert.match(incomingSection, /@change="applyIncomingPolicy"/)
  assert.match(incomingSection, /:data-selection="incomingPolicyDraft"/)
  assert.equal(
    (incomingSection.match(/class="incoming-policy__slider"/g) || []).length,
    1
  )
  assert.doesNotMatch(incomingSection, /<select/)
  assert.doesNotMatch(incomingSection, />保存</)
  assert.match(
    source,
    /\.incoming-policy__options\s*\{[^}]*grid-template-columns: repeat\(3, minmax\(0, 1fr\)\)[^}]*gap: 0/s
  )
  assert.match(
    source,
    /\.incoming-policy__slider\s*\{[^}]*width: calc\(\(100% - 6px\) \/ 3\)[^}]*transition: transform 180ms ease/s
  )
  assert.match(
    source,
    /\[data-selection='receive'\] \.incoming-policy__slider\s*\{[^}]*transform: translateX\(100%\)/s
  )
  assert.match(
    source,
    /\[data-selection='do_not_disturb'\] \.incoming-policy__slider\s*\{[^}]*transform: translateX\(200%\)/s
  )
  assert.match(
    source,
    /@media \(prefers-reduced-motion: reduce\)\s*\{[\s\S]*?\.incoming-policy__slider\s*\{[^}]*transition: none/
  )
  assert.doesNotMatch(
    source,
    /\.incoming-policy__options input:checked \+ span\s*\{[^}]*(?:background|box-shadow)/s
  )
})

test('hardware details show only backend-provided radio measurements', () => {
  assert.match(
    source,
    /v-if="selectedDevice\?\.signal_dbm != null"[\s\S]*?<dt>RSSI<\/dt>[\s\S]*?\{\{ selectedDevice\.signal_dbm \}\} dBm/
  )
  assert.match(
    source,
    /v-if="selectedDevice\?\.signal_rsrp != null"[\s\S]*?<dt>RSRP<\/dt>[\s\S]*?\{\{ selectedDevice\.signal_rsrp \}\} dBm/
  )
  assert.match(
    source,
    /v-if="selectedDevice\?\.signal_rsrq != null"[\s\S]*?<dt>RSRQ<\/dt>[\s\S]*?\{\{ selectedDevice\.signal_rsrq \}\} dB/
  )
  assert.match(
    source,
    /v-if="hardware\.details\.snr != null"[\s\S]*?<dt>SNR<\/dt>[\s\S]*?\{\{ hardware\.details\.snr \}\} dB/
  )
  assert.match(
    source,
    /t\('device\.primaryPort'\)[\s\S]*?hardware\.details\.primary_port/
  )
  assert.match(
    source,
    /<details v-if="hardware\.details\.ports\.length" class="hardware-ports">[\s\S]*?v-for="port in hardware\.details\.ports"/
  )
  assert.doesNotMatch(source, /signal_quality[^;\n]*(?:signal_dbm|signal_rsrp|signal_rsrq)/)
  assert.doesNotMatch(source, /signal_quality[^;\n]*hardware\.details\.snr/)
  assert.doesNotMatch(source, /hardware\.details\.ports[^;\n]*(?:voice|media|audio_available)/)
})
