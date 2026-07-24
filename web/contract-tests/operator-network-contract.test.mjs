import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseDevices, parseLine } from '../src/api/normalize.ts'
import {
  formatOperator,
  isRegisteredNetwork,
  operatorFacts,
  registrationStateLabel
} from '../src/utils/operatorNetwork.ts'

const networkFields = {
  operator: 'Pine Wireless',
  home_operator_code: '00102',
  home_operator_name: 'Pine Wireless',
  serving_operator_code: '00101',
  serving_operator_name: 'Aurora Mobile',
  registration_state_known: true,
  registration_state_code: 5,
  registration_state: 'roaming',
  roaming: true
}

const diagnosticLineCapabilities = {
  modem: true,
  sim: true,
  voice: true,
  messaging: true,
  media: false,
  dial: true,
  answer_call: true,
  reject_call: true,
  hangup_call: true,
  send_dtmf: true,
  send_message: true
}

const diagnosticAgentCapabilities = {
  discovery: true,
  snapshot: true,
  device_configuration: true,
  network: true,
  proxy: true,
  dial: true,
  answer_call: true,
  reject_call: true,
  hangup_call: true,
  send_dtmf: true,
  send_message: true,
  sim_management: true,
  connection_profiles: true,
  ussd: true,
  media: false
}

function roamingLine() {
  return {
    id: 'line-roaming',
    iccid: '89840400000000000099',
    imsi: '001020000000001',
    device_imei: '867530900000099',
    state: 'registered',
    emergency_only: false,
    ...networkFields
  }
}

test('operator facts distinguish the serving network from the home operator', () => {
  assert.equal(formatOperator('Aurora Mobile', '00101'), 'Aurora Mobile（00101）')
  assert.deepEqual(
    operatorFacts({
      ...networkFields,
      home_operator_code: '00101',
      home_operator_name: 'Aurora Mobile',
      registration_state_code: 1,
      registration_state: 'home',
      roaming: false
    }),
    [{ id: 'current', label: '运营商', value: 'Aurora Mobile（00101）' }]
  )
  assert.deepEqual(operatorFacts(networkFields), [
    { id: 'serving', label: '当前网络', value: 'Aurora Mobile（00101）' },
    { id: 'home', label: '归属运营商', value: 'Pine Wireless（00102）' }
  ])
  assert.deepEqual(
    operatorFacts({
      operator: 'China Unicom',
      home_operator_code: '46001',
      home_operator_name: 'China Unicom',
      registration_state_known: true,
      registration_state_code: 5,
      registration_state: 'roaming',
      roaming: true
    }),
    [
      { id: 'serving', label: '当前网络', value: '未识别' },
      { id: 'home', label: '归属运营商', value: 'China Unicom（46001）' }
    ]
  )
  assert.equal(registrationStateLabel(networkFields, '已驻网'), '漫游')
})

test('searching lines do not present cached serving operators as current networks', () => {
  const searching = {
    state: 'searching',
    home_operator_code: '00102',
    home_operator_name: 'Pine Wireless',
    serving_operator_code: '00101',
    serving_operator_name: 'Aurora Mobile',
    registration_state_known: true,
    registration_state: 'searching',
    roaming: false
  }

  assert.equal(isRegisteredNetwork(searching), false)
  assert.equal(registrationStateLabel(searching, '搜索网络'), '正在搜网')
  assert.deepEqual(operatorFacts(searching), [
    {
      id: 'home',
      label: '归属运营商',
      value: 'Pine Wireless（00102）'
    }
  ])
  assert.equal(
    isRegisteredNetwork({
      ...searching,
      registration_state: 'home'
    }),
    true
  )
})

test('idle emergency-only lines report the usable service state', () => {
  assert.equal(
    registrationStateLabel(
      {
        registration_state_known: true,
        registration_state: 'idle',
        emergency_only: true
      },
      '已启用'
    ),
    '仅限紧急呼叫'
  )
  assert.equal(
    registrationStateLabel(
      {
        registration_state_known: true,
        registration_state: 'idle',
        emergency_only: false
      },
      '已启用'
    ),
    '等待驻网'
  )
  assert.equal(
    registrationStateLabel(
      {
        registration_state_known: true,
        registration_state: 'searching',
        emergency_only: true
      },
      '已启用'
    ),
    '正在搜网'
  )
})

test('bootstrap and device decoders preserve the complete network identity', () => {
  const line = parseLine(roamingLine())
  assert.deepEqual(
    {
      operator: line.operator,
      homeCode: line.home_operator_code,
      homeName: line.home_operator_name,
      servingCode: line.serving_operator_code,
      servingName: line.serving_operator_name,
      stateKnown: line.registration_state_known,
      stateCode: line.registration_state_code,
      state: line.registration_state,
      roaming: line.roaming
    },
    {
      operator: 'Pine Wireless',
      homeCode: '00102',
      homeName: 'Pine Wireless',
      servingCode: '00101',
      servingName: 'Aurora Mobile',
      stateKnown: true,
      stateCode: 5,
      state: 'roaming',
      roaming: true
    }
  )

  const [device] = parseDevices({
    devices: [
      {
        imei: '867530900000099',
        sim: {
          iccid: '89840400000000000099',
          current_imei: '867530900000099',
          ...networkFields
        }
      }
    ]
  })
  assert.ok(device?.sim)
  assert.equal(device.sim.operator, 'Pine Wireless')
  assert.equal(device.sim.home_operator_name, 'Pine Wireless')
  assert.equal(device.sim.serving_operator_name, 'Aurora Mobile')
  assert.equal(device.sim.registration_state, 'roaming')
  assert.equal(device.sim.roaming, true)
})

test('SIM and diagnostics HTTP decoders preserve serving and home operators', async () => {
  const originalFetch = globalThis.fetch
  const requestedPaths = []
  const line = roamingLine()

  globalThis.fetch = async input => {
    const path = String(input)
    requestedPaths.push(path)
    const body =
      path === '/api/v1/diagnostics'
        ? {
            status: 'ok',
            observed_at: '2026-07-24T00:00:00Z',
            database: { available: true },
            host_agent: {
              connected: true,
              capabilities: diagnosticAgentCapabilities
            },
            call_runtime: { available: true },
            lines: [{ ...line, capabilities: diagnosticLineCapabilities }],
            active_calls: []
          }
        : {
            sim: {
              line_id: 'line-roaming',
              present: true,
              active: true,
              identifier: line.iccid,
              imsi: line.imsi,
              sim_type: 'physical',
              esim_status: 'unknown',
              sim_slots: [],
              sim_slots_known: false,
              primary_sim_slot: 0,
              primary_sim_slot_known: false,
              current_sim_slot: 0,
              current_sim_slot_known: false,
              profile_management: {
                supported: false,
                reason: 'eUICC profile management is unavailable'
              },
              operator_identifier: '00102',
              operator_name: 'Pine Wireless',
              unlock_required: 'none',
              unlock_required_code: 1,
              unlock_retries: { 'sim-pin': 3 },
              observed_at: '2026-07-24T00:00:00Z',
              ...networkFields
            }
          }
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }

  try {
    const sim = await gateway.getSIMStatus('line-roaming')
    const diagnostics = await gateway.getDiagnostics()

    assert.equal(sim.operator_name, 'Pine Wireless')
    assert.equal(sim.home_operator_name, 'Pine Wireless')
    assert.equal(sim.serving_operator_name, 'Aurora Mobile')
    assert.equal(sim.roaming, true)
    assert.equal(diagnostics.lines[0]?.operator, 'Pine Wireless')
    assert.equal(diagnostics.lines[0]?.home_operator_name, 'Pine Wireless')
    assert.equal(diagnostics.lines[0]?.serving_operator_name, 'Aurora Mobile')
    assert.equal(diagnostics.lines[0]?.roaming, true)
    assert.equal(diagnostics.lines[0]?.emergency_only, false)
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(requestedPaths, [
    '/api/v1/devices/line-roaming/sim',
    '/api/v1/diagnostics'
  ])
})

test('fixture presents the travel line as roaming on every frontend surface', async () => {
  const fixture = createFixtureGateway()
  const bootstrap = await fixture.getBootstrap()
  const devices = await fixture.listDevices()
  const sim = await fixture.getSIMStatus('line-fixture-travel')
  const diagnostics = await fixture.getDiagnostics()
  const line = bootstrap.lines.find(item => item.id === 'line-fixture-travel')
  const device = devices.find(item => item.imei === line?.device_imei)
  const diagnosticLine = diagnostics.lines.find(
    item => item.id === 'line-fixture-travel'
  )

  for (const source of [line, device?.sim, sim, diagnosticLine]) {
    assert.equal(source?.home_operator_name, 'Pine Wireless')
    assert.equal(source?.home_operator_code, '00102')
    assert.equal(source?.serving_operator_name, 'Aurora Mobile')
    assert.equal(source?.serving_operator_code, '00101')
    assert.equal(source?.registration_state, 'roaming')
    assert.equal(source?.roaming, true)
  }
})

test('device cards, settings, and diagnostics share the operator fact mapping', async () => {
  const [moduleCard, devicePanel, diagnosticsPanel] = await Promise.all([
    readFile(new URL('../src/components/ModuleCard.vue', import.meta.url), 'utf8'),
    readFile(
      new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
      'utf8'
    ),
    readFile(
      new URL('../src/components/DiagnosticsPanel.vue', import.meta.url),
      'utf8'
    )
  ])

  assert.match(moduleCard, /operatorFacts\(props\.line/)
  assert.match(moduleCard, /isRegisteredNetwork\(props\.line\)/)
  assert.doesNotMatch(moduleCard, /'searching'\]\.includes/)
  assert.match(moduleCard, /v-for="fact in networkFacts"/)
  assert.doesNotMatch(moduleCard, /\{\{\s*line\.operator\s*\|\|/)

  assert.match(devicePanel, /selectedOperatorFacts/)
  assert.match(devicePanel, /simOperatorFacts/)
  assert.match(devicePanel, /v-for="fact in selectedOperatorFacts"/)
  assert.match(devicePanel, /v-for="fact in simOperatorFacts"/)
  assert.doesNotMatch(devicePanel, /selectedLine\?\.operator/)
  assert.doesNotMatch(devicePanel, /simStatus\.operator_name/)

  assert.match(diagnosticsPanel, /lineOperatorFacts\(line\)/)
  assert.match(diagnosticsPanel, /lineRegistrationLabel\(line\)/)
  assert.match(diagnosticsPanel, /lineStateTone\(line\)/)
  assert.doesNotMatch(diagnosticsPanel, /lineStateTone\(line\.state\)/)
  assert.match(
    diagnosticsPanel,
    /\.line-grid\s*\{[\s\S]*grid-template-columns:\s*repeat\([\s\S]*auto-fill,[\s\S]*minmax\(min\(100%, 320px\), 420px\)[\s\S]*justify-content:\s*start/
  )
  assert.doesNotMatch(
    diagnosticsPanel,
    /\.line-grid\s*\{[\s\S]*minmax\(min\(100%, 440px\), 1fr\)/
  )
  assert.doesNotMatch(diagnosticsPanel, /\{\{\s*line\.operator\s*\|\|/)
})
