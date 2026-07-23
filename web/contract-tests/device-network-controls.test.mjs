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

  assert.match(
    connectBody,
    /connectData\(selectedLineID\.value, apn\.value, ipFamily\.value\)/
  )
  assert.match(switchBody, /enabled \? await applyDataConnection\(\) : await stopDataConnection\(\)/)
  assert.match(source, /<strong>移动数据<\/strong>/)
  assert.match(source, /:checked="hardware\.network_enabled"/)
  assert.match(source, /<span>APN<\/span>/)
  assert.match(source, /<span>IP<\/span>/)
})

test('VoWiFi is status-only and VoLTE remains visible with write gating', () => {
  const vowifiStart = source.indexOf('<h4>VoWiFi</h4>')
  const profileStart = source.indexOf('<h4>连接配置</h4>', vowifiStart)
  const vowifiSection = source.slice(vowifiStart, profileStart)
  const volteStart = source.indexOf('<h4>VoLTE</h4>')
  const volteEnd = source.indexOf('</section>', volteStart)
  const volteSection = source.slice(volteStart, volteEnd)

  assert.ok(vowifiStart >= 0)
  assert.match(vowifiSection, /capabilityStatus\(hardware\.capabilities\.vowifi/)
  assert.doesNotMatch(vowifiSection, /role="switch"/)

  assert.ok(volteStart >= 0)
  assert.match(volteSection, /v-model="voltePolicyDraft"/)
  assert.match(volteSection, /!hardware\.capabilities\.volte\.writable/)
  assert.match(volteSection, /volteStatusDetail/)
})
