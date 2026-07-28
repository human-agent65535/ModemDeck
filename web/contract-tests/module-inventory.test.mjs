import assert from 'node:assert/strict'
import test from 'node:test'

import { createFixtureGateway } from '../src/api/fixture.ts'
import {
  displayModuleLines,
  presentModuleLines
} from '../src/state/workspace.ts'

test('module inventory includes SIM-less and disconnected devices without creating service lines', async () => {
  const gateway = createFixtureGateway({ lineCount: 1 })
  const bootstrap = await gateway.getBootstrap()
  const devices = await gateway.listDevices()
  const simless = {
    ...devices[0],
    imei: 'fixture-no-sim',
    endpoint_id: 'line-endpoint-no-sim',
    name: 'Spare modem',
    current_iccid: '',
    sim_inserted: false,
    present: true,
    sim: undefined
  }
  const historical = {
    ...simless,
    imei: 'fixture-unplugged',
    endpoint_id: 'line-endpoint-unplugged',
    present: false
  }

  const modules = displayModuleLines(bootstrap.lines, devices.concat(simless, historical))

  assert.equal(bootstrap.lines.length, 1)
  assert.equal(modules.length, 3)
  assert.equal(modules[0]?.id, bootstrap.lines[0]?.id)
  assert.equal(modules[0]?.module_only, undefined)
  assert.deepEqual(
    {
      imei: modules[1]?.device_imei,
      state: modules[1]?.state,
      moduleOnly: modules[1]?.module_only,
      modem: modules[1]?.capabilities?.modem,
      sim: modules[1]?.capabilities?.sim
    },
    {
      imei: 'fixture-no-sim',
      state: 'sim-missing',
      moduleOnly: true,
      modem: true,
      sim: undefined
    }
  )
  assert.deepEqual(
    {
      imei: modules[2]?.device_imei,
      state: modules[2]?.state,
      moduleOnly: modules[2]?.module_only,
      modem: modules[2]?.capabilities?.modem
    },
    {
      imei: 'fixture-unplugged',
      state: 'disconnected',
      moduleOnly: true,
      modem: false
    }
  )

  const present = presentModuleLines(
    bootstrap.lines.concat({
      ...bootstrap.lines[0],
      id: 'duplicate-service-line'
    }),
    devices.concat(simless, historical)
  )
  assert.deepEqual(
    present.map(line => line.device_imei),
    [bootstrap.lines[0]?.device_imei, 'fixture-no-sim']
  )
})

test('fixture gateway only deletes disconnected modem inventory records', async () => {
  const gateway = createFixtureGateway({ lineCount: 1 })
  const connected = (await gateway.listDevices())[0]
  assert.ok(connected)

  await assert.rejects(
    gateway.deleteDevice(connected.imei),
    error => error?.code === 'device_present' && error?.status === 409
  )

  const historical = await gateway.createDevice({
    imei: 'fixture-delete-offline',
    name: 'Offline fixture'
  })
  assert.equal(historical.present, false)

  await gateway.deleteDevice(historical.imei)
  assert.equal(
    (await gateway.listDevices()).some(device => device.imei === historical.imei),
    false
  )
})
