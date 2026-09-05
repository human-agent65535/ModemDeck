import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { clearSession, login } from '../src/state/session.ts'
import {
  deviceConfigurationState, deviceConfigurationResource, globalIncomingCallState,
  loadDeviceConfiguration, loadGlobalIncomingCallSettings,
  resetDeviceConfigurationState, selectDeviceConfiguration
} from '../src/state/deviceConfiguration.ts'

test('logout removes device data and rejects late responses even after the same line reloads', async () => {
  const fixture = createFixtureGateway()
  const configuration = await fixture.getDeviceConfiguration('line-fixture-main')
  const original = { getDeviceConfiguration: gateway.getDeviceConfiguration,
    getGlobalCallSettings: gateway.getGlobalCallSettings, login: gateway.login }
  try {
    gateway.getDeviceConfiguration = async () => configuration
    await loadDeviceConfiguration('private-line')
    selectDeviceConfiguration('private-line')
    const previous = deviceConfigurationResource('private-line')
    let completeDevice, completeSettings
    gateway.getDeviceConfiguration = () => new Promise(resolve => { completeDevice = resolve })
    gateway.getGlobalCallSettings = () => new Promise(resolve => { completeSettings = resolve })
    const pendingDevice = loadDeviceConfiguration('private-line', true)
    const pendingSettings = loadGlobalIncomingCallSettings(true)
    clearSession()
    assert.equal(previous.data, null)
    assert.equal(deviceConfigurationState.selectedLineID, '')
    assert.deepEqual(Object.keys(deviceConfigurationState.resources), [])
    gateway.login = async () => ({ authenticated: true, user_id: 'member-no-lines',
      username: 'member', role: 'member', allowed_line_ids: [], language: 'en-US' })
    await login('member', 'test-only')
    gateway.getDeviceConfiguration = async () => configuration
    await loadDeviceConfiguration('private-line')
    completeDevice(configuration)
    completeSettings(await fixture.getGlobalCallSettings())
    assert.equal(await pendingDevice, false)
    assert.equal(await pendingSettings, false)
    assert.equal(previous.data, null)
    assert.equal(globalIncomingCallState.data, null)
    assert.equal(globalIncomingCallState.status, 'idle')
  } finally {
    Object.assign(gateway, original)
    resetDeviceConfigurationState()
  }
})
