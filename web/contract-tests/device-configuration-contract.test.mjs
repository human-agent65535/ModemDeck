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
const incomingCallModeSource = readFileSync(
  new URL('../src/components/IncomingCallModeControl.vue', import.meta.url),
  'utf8'
)
const moduleCardSource = readFileSync(
  new URL('../src/components/ModuleCard.vue', import.meta.url),
  'utf8'
)

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
  assert.match(incomingCallModeSource, /线路实际执行能力见设备设置/)
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
  assert.match(moduleCardSource, /defaultLine \? '默认线路' : '设为默认'/)
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
  assert.notEqual(updated.hardware?.revision, initial.hardware.revision)

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
    const confirmation = body.indexOf('window.confirm(')
    const cancellationReturn = body.indexOf('return', confirmation)
    const write = body.indexOf(writeCall)
    assert.ok(confirmation >= 0, `${writeCall} 缺少确认`)
    assert.ok(cancellationReturn > confirmation, `${writeCall} 取消时没有提前返回`)
    assert.ok(write > cancellationReturn, `${writeCall} 在确认取消前已执行`)
  }
})
