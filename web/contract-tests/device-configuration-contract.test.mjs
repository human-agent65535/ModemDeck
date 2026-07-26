import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  communicationContracts,
  createDeviceConfigurationPayload,
  createGlobalCallSettingsPayload,
  deviceConfigurationContract,
  deviceConfigurationPath,
  parseDeviceConfigurationResponse,
  parseGlobalCallSettings
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'

const devicePanelSource = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const deviceConfigurationStateSource = readFileSync(
  new URL('../src/state/deviceConfiguration.ts', import.meta.url),
  'utf8'
)
const incomingCallModeSource = readFileSync(
  new URL('../src/components/IncomingCallModeControl.vue', import.meta.url),
  'utf8'
)
const moduleCardSource = readFileSync(
  new URL('../src/components/ModuleCard.vue', import.meta.url),
  'utf8'
)

function functionBody(source, declaration) {
  const declarationStart = source.indexOf(declaration)
  assert.ok(declarationStart >= 0, `缺少 ${declaration}`)
  const bodyStart = source.indexOf('{', declarationStart)
  assert.ok(bodyStart >= 0, `${declaration} 缺少函数体`)

  let depth = 0
  for (let index = bodyStart; index < source.length; index += 1) {
    if (source[index] === '{') depth += 1
    if (source[index] !== '}') continue
    depth -= 1
    if (depth === 0) return source.slice(bodyStart + 1, index)
  }
  assert.fail(`${declaration} 函数体不完整`)
}

test('call policy and device configuration contracts match the root API', () => {
  assert.deepEqual(communicationContracts.getCallSettings, {
    method: 'GET',
    path: '/api/v1/settings/calls',
    successStatus: 200
  })
  assert.deepEqual(communicationContracts.updateCallSettings, {
    method: 'PATCH',
    path: '/api/v1/settings/calls',
    successStatus: 200
  })
  assert.equal(
    deviceConfigurationPath('line / main'),
    '/api/v1/devices/line%20%2F%20main/configuration'
  )
  assert.deepEqual(deviceConfigurationContract('line-main'), {
    get: {
      method: 'GET',
      path: '/api/v1/devices/line-main/configuration',
      successStatus: 200
    },
    update: {
      method: 'PATCH',
      path: '/api/v1/devices/line-main/configuration',
      successStatus: 200
    }
  })
  assert.deepEqual(
    createGlobalCallSettingsPayload({ receive_calls: false, expected_revision: 3 }),
    { receive_calls: false, expected_revision: 3 }
  )
  assert.deepEqual(
    createDeviceConfigurationPayload({
      request_id: 'request-radio-1',
      operation: 'set_radio_enabled',
      expected_device_revision: 'sha256:current',
      radio_enabled: false
    }),
    {
      request_id: 'request-radio-1',
      operation: 'set_radio_enabled',
      expected_device_revision: 'sha256:current',
      radio_enabled: false
    }
  )
  assert.deepEqual(
    createDeviceConfigurationPayload({
      operation: 'set_incoming_call_policy',
      expected_policy_revision: 2,
      incoming_call_policy: 'follow_global'
    }),
    {
      operation: 'set_incoming_call_policy',
      expected_policy_revision: 2,
      incoming_call_policy: 'follow_global'
    }
  )
  const restartPayload = createDeviceConfigurationPayload({
    request_id: ' request-restart-1 ',
    operation: 'restart_modem',
    expected_device_revision: ' sha256:current ',
    volte_policy: 'enabled',
    restart_required: true
  })
  assert.deepEqual(restartPayload, {
    request_id: 'request-restart-1',
    operation: 'restart_modem',
    expected_device_revision: 'sha256:current'
  })
  assert.deepEqual(Object.keys(restartPayload).sort(), [
    'expected_device_revision',
    'operation',
    'request_id'
  ])
  assert.throws(
    () =>
      createDeviceConfigurationPayload({
        request_id: 'request-connect-1',
        operation: 'connect_data',
        expected_device_revision: 'sha256:current',
        apn: 'invalid apn',
        ip_family: 'ipv4'
      }),
    /APN/
  )
})

test('VoLTE restart remains an explicit user action', () => {
  const applyVoLTEBody = functionBody(devicePanelSource, 'async function applyVoLTE')
  const setVoLTEPolicyBody = functionBody(
    deviceConfigurationStateSource,
    'export function setVoLTEPolicy'
  )

  assert.match(deviceConfigurationStateSource, /operation:\s*['"]restart_modem['"]/)
  assert.match(devicePanelSource, /@click="[^"]*restart[^"]*"/i)
  assert.doesNotMatch(applyVoLTEBody, /\brestart[A-Za-z0-9_]*\s*\(/i)
  assert.doesNotMatch(applyVoLTEBody, /operation:\s*['"]restart_modem['"]/)
  assert.doesNotMatch(setVoLTEPolicyBody, /\brestart[A-Za-z0-9_]*\s*\(/i)
  assert.doesNotMatch(setVoLTEPolicyBody, /operation:\s*['"]restart_modem['"]/)
})

test('fixture exposes config-only enforcement without promising automatic rejection', async () => {
  const gateway = createFixtureGateway()
  const configuration = await gateway.getDeviceConfiguration('line-fixture-travel')
  const parsed = parseDeviceConfigurationResponse(configuration)

  assert.equal(parsed.incoming_calls?.enforcement.available, false)
  assert.equal(parsed.incoming_calls?.enforcement.config_only, true)
  assert.match(parsed.incoming_calls?.enforcement.reason || '', /reject-call capability/)
  assert.equal(parsed.incoming_calls?.effective_policy, 'receive')
  assert.equal(parsed.hardware?.capabilities.volte.writable, false)
})

test('device configuration preserves the server-resolved automatic APN', async () => {
  const configuration = await createFixtureGateway().getDeviceConfiguration(
    'line-fixture-main'
  )
  assert.ok(configuration.hardware)
  configuration.hardware.automatic_apn = 'automatic.example'

  const parsed = parseDeviceConfigurationResponse(configuration)

  assert.equal(parsed.hardware?.automatic_apn, 'automatic.example')
})

test('device configuration preserves truthful hardware details and nullable telemetry', async () => {
  const configuration = await createFixtureGateway().getDeviceConfiguration(
    'line-fixture-main'
  )
  assert.ok(configuration.hardware)

  const parsed = parseDeviceConfigurationResponse(configuration)

  assert.equal(parsed.hardware?.details.hardware_revision, 'fixture-hw-1')
  assert.equal(parsed.hardware?.details.primary_port, 'cdc-wdm0')
  assert.equal(parsed.hardware?.details.access_technologies, 1 << 14)
  assert.equal(parsed.hardware?.details.snr, 8.5)
  assert.deepEqual(
    parsed.hardware?.details.ports.map(port => [port.name, port.type, port.type_code]),
    [
      ['cdc-wdm0', 'qmi', 6],
      ['ttyUSB2', 'at', 3],
      ['wwan0', 'net', 2]
    ]
  )

  delete configuration.hardware.details
  const missing = parseDeviceConfigurationResponse(configuration)
  assert.deepEqual(missing.hardware?.details, {
    hardware_revision: '',
    primary_port: '',
    access_technologies: null,
    snr: null,
    ports: []
  })
})

test('device configuration rejects invented or malformed hardware telemetry', async () => {
  const configuration = await createFixtureGateway().getDeviceConfiguration(
    'line-fixture-main'
  )
  assert.ok(configuration.hardware)
  configuration.hardware.details.snr = Number.NaN
  assert.throws(
    () => parseDeviceConfigurationResponse(configuration),
    /hardware\.details\.snr/
  )

  configuration.hardware.details.snr = null
  configuration.hardware.details.access_technologies = -1
  assert.throws(
    () => parseDeviceConfigurationResponse(configuration),
    /hardware\.details\.access_technologies/
  )
})

test('VoLTE configuration keeps policy and modem capability separate', async () => {
  const configuration = await createFixtureGateway().getDeviceConfiguration(
    'line-fixture-main'
  )
  assert.ok(configuration.hardware)
  configuration.hardware.volte.policy = 'enabled'
  configuration.hardware.volte.configuration_mode = 'forced_enabled'
  configuration.hardware.volte.modem_capability_known = true
  configuration.hardware.volte.modem_capability_enabled = false
  configuration.hardware.volte.restart_required = true

  const parsed = parseDeviceConfigurationResponse(configuration)

  assert.equal(parsed.hardware?.volte.policy, 'enabled')
  assert.equal(parsed.hardware?.volte.configuration_mode, 'forced_enabled')
  assert.equal(parsed.hardware?.volte.modem_capability_known, true)
  assert.equal(parsed.hardware?.volte.modem_capability_enabled, false)
  assert.equal(parsed.hardware?.volte.restart_required, true)
})

test('global call settings contain only preference and revision', async () => {
  const settings = parseGlobalCallSettings({
    receive_calls: false,
    revision: 4
  })
  assert.deepEqual(settings, {
    receive_calls: false,
    revision: 4
  })
  assert.deepEqual(await createFixtureGateway().getGlobalCallSettings(), {
    receive_calls: true,
    revision: 1
  })
  assert.equal(incomingCallModeSource.includes('enforcement'), false)
  assert.equal(incomingCallModeSource.includes('当前策略已保存'), false)
  assert.equal(incomingCallModeSource.includes('当前无法自动拒接'), false)
  assert.match(incomingCallModeSource, /useI18n/)
  assert.match(incomingCallModeSource, /t\('incomingCallMode\.deviceCapabilityNotice'\)/)
  assert.doesNotMatch(incomingCallModeSource, /全局来电策略|接听来电|免打扰|重新读取/)
})

test('fixture can return no devices and does not invent a selection target', async () => {
  const gateway = createFixtureGateway({ noDevices: true })
  assert.deepEqual((await gateway.getBootstrap()).lines, [])
  assert.deepEqual(await gateway.listDevices(), [])
  await assert.rejects(
    () => gateway.getDeviceConfiguration('line-fixture-main'),
    error => error?.status === 404 && error?.code === 'not_found'
  )
})

test('fixture preserves every one of six server lines and exposes each configuration', async () => {
  const gateway = createFixtureGateway({ lineCount: 6 })
  const bootstrap = await gateway.getBootstrap()

  assert.equal(bootstrap.lines.length, 6)
  assert.equal(new Set(bootstrap.lines.map(line => line.id)).size, 6)
  const configurations = await Promise.all(
    bootstrap.lines.map(line => gateway.getDeviceConfiguration(line.id))
  )
  assert.deepEqual(
    configurations.map(configuration => configuration.hardware?.line_id),
    bootstrap.lines.map(line => line.id)
  )
})

test('module cards keep selection and default actions in a stable shared footer', () => {
  const footerStart = moduleCardSource.indexOf(
    '<footer class="module-card__footer">'
  )
  const capabilitiesStart = moduleCardSource.indexOf(
    'class="module-card__capabilities"',
    footerStart
  )
  const actionsStart = moduleCardSource.indexOf(
    'class="module-card__actions"',
    footerStart
  )

  assert.ok(footerStart >= 0, '模组卡片缺少固定底栏')
  assert.ok(capabilitiesStart > footerStart, '模组能力不在底栏内')
  assert.ok(actionsStart > capabilitiesStart, '默认线路与编辑动作不在能力右侧')
  assert.doesNotMatch(moduleCardSource, /\bStar\b/)
  assert.match(moduleCardSource, /\bCircleCheck\b/)
  assert.match(
    moduleCardSource,
    /defaultLine \? t\('lines\.defaultLine'\) : t\('lines\.setAsDefault'\)/
  )
  assert.match(moduleCardSource, /:disabled="defaultLine"/)
  assert.match(moduleCardSource, /\.module-card__current\s*\{[^}]*visibility: hidden/s)
  assert.match(
    moduleCardSource,
    /\.module-card__current\.is-visible\s*\{[^}]*visibility: visible/s
  )
  assert.match(
    moduleCardSource,
    /grid-template-columns: minmax\(0, 1fr\) auto/
  )
})

test('default line status is shown once in the module card action', () => {
  assert.doesNotMatch(devicePanelSource, /t\('lines\.defaultLine'\)/)
  assert.match(
    moduleCardSource,
    /<span>\{\{ defaultLine \? t\('lines\.defaultLine'\) : t\('lines\.setAsDefault'\) \}\}<\/span>/
  )
})

test('device configuration distinguishes module identity from the line label', () => {
  assert.match(devicePanelSource, /<strong>\{\{ selectedModuleName \}\}<\/strong>/)
  assert.match(
    devicePanelSource,
    /v-if="selectedLine && selectedExplicitLineLabel"/
  )
  assert.match(
    devicePanelSource,
    /label\.toLocaleLowerCase\(\) === selectedModuleName\.value\.toLocaleLowerCase\(\)/
  )
})

test('device discovery is automatic and does not ask for a manually entered IMEI', () => {
  const runtimeEventsSource = readFileSync(
    new URL('../src/state/runtimeEvents.ts', import.meta.url),
    'utf8'
  )

  assert.doesNotMatch(devicePanelSource, /addIMEI|addModule|新增模组/)
  assert.doesNotMatch(devicePanelSource, /<span>IMEI<\/span>\s*<input/)
  assert.doesNotMatch(devicePanelSource, /setInterval\(/)
  assert.match(
    runtimeEventsSource,
    /case 'lines':[\s\S]*?await refreshDeviceWorkspace\(\)/
  )
})

test('fixture applies successful revisioned updates and rejects stale revisions', async () => {
  const gateway = createFixtureGateway()
  const global = parseGlobalCallSettings(await gateway.getGlobalCallSettings())
  const updatedGlobal = await gateway.updateGlobalCallSettings({
    receive_calls: false,
    expected_revision: global.revision
  })
  assert.equal(updatedGlobal.receive_calls, false)
  assert.equal(updatedGlobal.revision, global.revision + 1)
  assert.deepEqual(Object.keys(updatedGlobal).sort(), ['receive_calls', 'revision'])
  await assert.rejects(
    () =>
      gateway.updateGlobalCallSettings({
        receive_calls: true,
        expected_revision: global.revision
      }),
    error => error?.status === 409 && error?.code === 'conflict'
  )

  const initial = await gateway.getDeviceConfiguration('line-fixture-main')
  const updated = await gateway.updateDeviceConfiguration('line-fixture-main', {
    request_id: 'request-radio-success',
    operation: 'set_radio_enabled',
    expected_device_revision: initial.hardware.revision,
    radio_enabled: false
  })
  assert.equal(updated.hardware?.radio.enabled, false)
  assert.equal(updated.hardware?.flight_mode, true)
  assert.equal(updated.hardware?.network_enabled, false)
  assert.deepEqual(updated.hardware?.data_connections, [])
  assert.notEqual(updated.hardware?.revision, initial.hardware.revision)

  await assert.rejects(
    () =>
      gateway.updateDeviceConfiguration('line-fixture-main', {
        request_id: 'request-connect-flight-mode',
        operation: 'connect_data',
        expected_device_revision: updated.hardware.revision,
        apn: '',
        ip_family: 'ipv4v6'
      }),
    error => error?.status === 412 && error?.code === 'failed_precondition'
  )

  await assert.rejects(
    () =>
      gateway.updateDeviceConfiguration('line-fixture-main', {
        request_id: 'request-radio-stale',
        operation: 'set_radio_enabled',
        expected_device_revision: initial.hardware.revision,
        radio_enabled: true
      }),
    error => error?.status === 409 && error?.code === 'conflict'
  )

  const policy = (await gateway.getDeviceConfiguration('line-fixture-main')).incoming_calls
  const updatedPolicy = await gateway.updateDeviceConfiguration('line-fixture-main', {
    operation: 'set_incoming_call_policy',
    expected_policy_revision: policy.revision,
    incoming_call_policy: 'do_not_disturb'
  })
  assert.equal(updatedPolicy.incoming_calls?.policy, 'do_not_disturb')
  assert.equal(updatedPolicy.incoming_calls?.effective_policy, 'do_not_disturb')
})

test('hardware writes refresh their revision and retry one concurrent change', () => {
  const body = functionBody(
    deviceConfigurationStateSource,
    'async function applyHardwareUpdate'
  )

  assert.match(body, /for \(let attempt = 0; attempt < 2; attempt \+= 1\)/)
  assert.match(body, /const latest = await gateway\.getDeviceConfiguration\(lineID\)/)
  assert.match(body, /request_id: requestID\(\)/)
  assert.match(body, /expected_device_revision: latest\.hardware\.revision/)
  assert.match(body, /error instanceof ApiError/)
  assert.match(body, /error\.code !== 'conflict'/)
})

test('risky hardware writes return on cancelled confirmation before invoking state writes', () => {
  const radioStart = devicePanelSource.indexOf('async function changeRadio')
  const dataStart = devicePanelSource.indexOf('async function stopDataConnection')
  const volteStart = devicePanelSource.indexOf('async function applyVoLTE')
  const policyStart = devicePanelSource.indexOf('async function applyIncomingPolicy')
  const radioBody = devicePanelSource.slice(radioStart, dataStart)
  const dataBody = devicePanelSource.slice(dataStart, volteStart)
  const volteBody = devicePanelSource.slice(volteStart, policyStart)

  for (const [body, writeCall] of [
    [radioBody, 'setRadioEnabled('],
    [dataBody, 'disconnectData('],
    [volteBody, 'setVoLTEPolicy(']
  ]) {
    const confirmation = body.indexOf('await requestConfirmation(')
    const cancellationReturn = body.indexOf('return', confirmation)
    const write = body.indexOf(writeCall)
    assert.ok(confirmation >= 0, `${writeCall} 缺少确认`)
    assert.ok(cancellationReturn > confirmation, `${writeCall} 取消时没有提前返回`)
    assert.ok(write > cancellationReturn, `${writeCall} 在确认取消前已执行`)
  }
})

test('connected data details render only values reported by the device API', () => {
  assert.match(devicePanelSource, /const dataConnectionFacts = computed/)
  assert.match(devicePanelSource, /if \(!connection\) return \[\]/)
  assert.match(devicePanelSource, /v-if="dataConnectionFacts\.length"/)
  assert.match(devicePanelSource, /addIPConfiguration\('IPv4', connection\.ipv4\)/)
  assert.match(devicePanelSource, /addIPConfiguration\('IPv6', connection\.ipv6\)/)
  for (const key of [
    'traffic.interface',
    'device.ipMode',
    'device.ipAddress',
    'device.ipPrefix',
    'device.ipGateway'
  ]) {
    assert.match(devicePanelSource, new RegExp(`t\\('${key.replace('.', '\\.')}\\'`))
  }
  for (const protocolLabel of ['APN', 'DNS', 'MTU']) {
    assert.match(devicePanelSource, new RegExp(protocolLabel))
  }
})
