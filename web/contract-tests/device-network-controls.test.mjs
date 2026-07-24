import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
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
  assert.doesNotMatch(source, /<h4>蜂窝射频<\/h4>/)
})

test('mobile data switch uses APN and IP settings for connect and disconnect', () => {
  const connectBody = functionBody('applyDataConnection', 'stopDataConnection')
  const switchBody = functionBody('changeDataConnection', 'applyVoLTE')
  const networkStart = source.indexOf("<template v-else-if=\"activeTab === 'network'\">")
  const networkEnd = source.indexOf("<template v-else-if=\"activeTab === 'sim'\">", networkStart)
  const networkSection = source.slice(networkStart, networkEnd)

  assert.match(
    connectBody,
    /connectData\(selectedLineID\.value, apn\.value, ipFamily\.value\)/
  )
  assert.match(switchBody, /enabled \? await applyDataConnection\(\) : await stopDataConnection\(\)/)
  assert.match(source, /<strong>移动数据<\/strong>/)
  assert.match(source, /:checked="hardware\.network_enabled"/)
  assert.match(source, /<span>APN<\/span>/)
  assert.match(networkSection, /<legend>IP 模式<\/legend>/)
  assert.match(networkSection, /type="radio" value="ipv4"/)
  assert.match(networkSection, /type="radio" value="ipv6"/)
  assert.match(networkSection, /type="radio" value="ipv4v6"/)
  assert.doesNotMatch(networkSection, /value="auto"/)
  assert.match(source, /const ipFamily = ref<IPFamily>\('ipv4v6'\)/)
  assert.match(
    source,
    /connection\.ip_family\s*:\s*'ipv4v6'/
  )
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
    /<details class="advanced-profiles">[\s\S]*<strong>高级连接配置<\/strong>/
  )
  assert.doesNotMatch(networkSection, /<details class="advanced-profiles"[^>]*\sopen/)
  assert.match(networkSection, /@submit\.prevent="saveProfile"/)
  assert.match(networkSection, /@click\.stop="deleteProfile\(profile\)"/)
})

test('VoWiFi is status-only in the call tab and VoLTE keeps write gating', () => {
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
  assert.match(voiceSection, /<strong>通话路径<\/strong>/)
  assert.match(voiceSection, /:class="\{ 'is-available': voiceAvailable \}"/)
  assert.doesNotMatch(
    voiceSection.slice(
      voiceSection.indexOf('<strong>VoWiFi</strong>'),
      voiceSection.indexOf('</div>', voiceSection.indexOf('<strong>VoWiFi</strong>'))
    ),
    /role="switch"/
  )

  assert.ok(volteStart >= 0)
  assert.match(volteSection, /v-model="voltePolicyDraft"/)
  assert.match(volteSection, /!hardware\.capabilities\.volte\.writable/)
  assert.match(volteSection, /volteStatusDetail/)
})
