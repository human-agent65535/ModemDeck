import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'

import { parseBootstrap } from '../src/api/normalize.ts'

const workspaceSource = readFileSync(
  new URL('../src/state/workspace.ts', import.meta.url),
  'utf8'
)
const dashboardSource = readFileSync(
  new URL('../src/views/DashboardView.vue', import.meta.url),
  'utf8'
)

function line(id, label) {
  return {
    id,
    iccid: '',
    imsi: '',
    phone_number: '',
    operator: '',
    home_operator_code: '',
    home_operator_name: '',
    serving_operator_code: '',
    serving_operator_name: '',
    registration_state_known: false,
    registration_state_code: 0,
    registration_state: '',
    roaming: false,
    emergency_only: false,
    device_imei: '',
    device_name: '',
    line_label: label,
    line_color: '',
    radio_desired_enabled: false,
    radio_desired_enabled_known: false,
    capabilities: {}
  }
}

test('bootstrap keeps active routes separate from the stable line catalog', () => {
  const bootstrap = parseBootstrap({
    capabilities: {
      agent_connected: true,
      dial: true,
      message: true,
      webrtc_audio: false,
      device_control: true,
      volte_control: false,
      vowifi_control: false
    },
    lines: [line('line-active', 'Bac')],
    line_catalog: [
      line('line-active', 'Bac'),
      line('line-history', 'Thuy')
    ],
    line_settings: { default_line_id: 'line-active', revision: 1 },
    system_settings: { language: 'auto', revision: 1 }
  })

  assert.deepEqual(bootstrap.lines.map(item => item.id), ['line-active'])
  assert.deepEqual(
    bootstrap.line_catalog.map(item => [item.id, item.line_label]),
    [
      ['line-active', 'Bac'],
      ['line-history', 'Thuy']
    ]
  )
})

test('display identity resolution keeps live capabilities and falls back to stable history', () => {
  assert.match(
    workspaceSource,
    /bootstrap\?\.lines\.find\(line => lineKey\(line\) === normalizedKey\)[\s\S]*bootstrap\?\.line_catalog\.find\(line => lineKey\(line\) === normalizedKey\)/
  )
  assert.match(
    dashboardSource,
    /function lineForCall[\s\S]*?return lineForKey\(call\.line_id\)/
  )
  assert.match(
    dashboardSource,
    /function lineForThread[\s\S]*?return lineForKey\(thread\.line_id\)/
  )
})
