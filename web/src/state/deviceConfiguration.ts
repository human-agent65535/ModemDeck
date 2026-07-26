import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  DeviceConfiguration,
  GlobalCallSettings,
  IncomingCallPolicy,
  IPFamily,
  ResourceStatus,
  UpdateDeviceConfigurationInput
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'

type DeviceConfigurationResource = {
  status: ResourceStatus
  data: DeviceConfiguration | null
  error: string
  savingOperation: UpdateDeviceConfigurationInput['operation'] | ''
}

export const globalIncomingCallState = reactive<{
  status: ResourceStatus
  data: GlobalCallSettings | null
  error: string
  saving: boolean
}>({
  status: 'idle',
  data: null,
  error: '',
  saving: false
})

export const deviceConfigurationState = reactive<{
  selectedLineID: string
  resources: Record<string, DeviceConfigurationResource>
}>({
  selectedLineID: '',
  resources: {}
})

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
}

function errorStatus(error: unknown): ResourceStatus {
  return error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
}

function requestID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = ((bytes[6] || 0) & 0x0f) | 0x40
  bytes[8] = ((bytes[8] || 0) & 0x3f) | 0x80
  const encoded = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  return `${encoded.slice(0, 8)}-${encoded.slice(8, 12)}-${encoded.slice(12, 16)}-${encoded.slice(16, 20)}-${encoded.slice(20)}`
}

function resourceFor(lineID: string): DeviceConfigurationResource {
  if (!deviceConfigurationState.resources[lineID]) {
    deviceConfigurationState.resources[lineID] = {
      status: 'idle',
      data: null,
      error: '',
      savingOperation: ''
    }
  }
  return deviceConfigurationState.resources[lineID]
}

function mergeConfiguration(
  current: DeviceConfiguration | null,
  update: DeviceConfiguration
): DeviceConfiguration {
  return {
    ...(current?.hardware ? { hardware: current.hardware } : {}),
    ...(current?.incoming_calls ? { incoming_calls: current.incoming_calls } : {}),
    ...(update.hardware ? { hardware: update.hardware } : {}),
    ...(update.incoming_calls ? { incoming_calls: update.incoming_calls } : {})
  }
}

async function refreshConfigurationsAfterGlobalChange(): Promise<void> {
  for (const target of Object.values(deviceConfigurationState.resources)) {
    if (target.data) target.status = 'idle'
  }
  const selectedLineID = deviceConfigurationState.selectedLineID
  if (selectedLineID) await loadDeviceConfiguration(selectedLineID, true)
}

export async function loadGlobalIncomingCallSettings(force = false): Promise<boolean> {
  if (
    !force &&
    (globalIncomingCallState.status === 'ready' ||
      globalIncomingCallState.status === 'loading')
  ) {
    return globalIncomingCallState.status === 'ready'
  }
  globalIncomingCallState.status = 'loading'
  globalIncomingCallState.error = ''
  try {
    globalIncomingCallState.data = await gateway.getGlobalCallSettings()
    globalIncomingCallState.status = 'ready'
    return true
  } catch (error) {
    globalIncomingCallState.status = errorStatus(error)
    globalIncomingCallState.error = errorText(error)
    return false
  }
}

export async function updateGlobalIncomingCallSettings(
  receiveCalls: boolean
): Promise<boolean> {
  const current = globalIncomingCallState.data
  if (!current || globalIncomingCallState.saving) return false

  globalIncomingCallState.saving = true
  globalIncomingCallState.error = ''
  globalIncomingCallState.data = { ...current, receive_calls: receiveCalls }
  try {
    globalIncomingCallState.data = await gateway.updateGlobalCallSettings({
      receive_calls: receiveCalls,
      expected_revision: current.revision
    })
    globalIncomingCallState.status = 'ready'
    await refreshConfigurationsAfterGlobalChange()
    return true
  } catch (error) {
    const saveError = errorText(error)
    try {
      globalIncomingCallState.data = await gateway.getGlobalCallSettings()
      globalIncomingCallState.status = 'ready'
      globalIncomingCallState.error = saveError
      await refreshConfigurationsAfterGlobalChange()
    } catch (refreshError) {
      globalIncomingCallState.data = current
      globalIncomingCallState.status = errorStatus(refreshError)
      globalIncomingCallState.error = translate('runtime.refreshAfterSaveFailed', {
        error: saveError,
        refreshError: errorText(refreshError)
      })
    }
    return false
  } finally {
    globalIncomingCallState.saving = false
  }
}

export function selectDeviceConfiguration(lineID: string): void {
  deviceConfigurationState.selectedLineID = lineID.trim()
}

export function selectedDeviceConfiguration(): DeviceConfigurationResource | null {
  const lineID = deviceConfigurationState.selectedLineID
  return lineID ? resourceFor(lineID) : null
}

export function deviceConfigurationResource(lineID: string): DeviceConfigurationResource {
  return resourceFor(lineID)
}

export async function loadDeviceConfiguration(
  lineID: string,
  force = false
): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return false
  const target = resourceFor(normalizedLineID)
  if (!force && (target.status === 'ready' || target.status === 'loading')) {
    return target.status === 'ready'
  }
  target.status = 'loading'
  target.error = ''
  try {
    const configuration = await gateway.getDeviceConfiguration(normalizedLineID)
    if (!configuration.hardware || !configuration.incoming_calls) {
      throw new Error(translate('runtime.invalidDeviceConfiguration'))
    }
    target.data = configuration
    target.status = 'ready'
    return true
  } catch (error) {
    target.status = errorStatus(error)
    target.error = errorText(error)
    return false
  }
}

async function restoreDeviceConfiguration(
  lineID: string,
  target: DeviceConfigurationResource,
  saveError: string
): Promise<void> {
  try {
    target.data = await gateway.getDeviceConfiguration(lineID)
    target.status = 'ready'
    target.error = saveError
  } catch (refreshError) {
    target.status = errorStatus(refreshError)
    target.error = translate('runtime.refreshAfterSaveFailed', {
      error: saveError,
      refreshError: errorText(refreshError)
    })
  }
}

async function updateDevice(
  lineID: string,
  input: UpdateDeviceConfigurationInput
): Promise<boolean> {
  const target = resourceFor(lineID)
  if (!target.data || target.savingOperation) return false
  target.savingOperation = input.operation
  target.error = ''
  try {
    const updated = await gateway.updateDeviceConfiguration(lineID, input)
    target.data = mergeConfiguration(target.data, updated)
    target.status = 'ready'
    return true
  } catch (error) {
    await restoreDeviceConfiguration(lineID, target, errorText(error))
    return false
  } finally {
    target.savingOperation = ''
  }
}

function hardwareRevision(lineID: string): string {
  return resourceFor(lineID).data?.hardware?.revision || ''
}

export function setRadioEnabled(lineID: string, enabled: boolean): Promise<boolean> {
  return updateDevice(lineID, {
    request_id: requestID(),
    operation: 'set_radio_enabled',
    expected_device_revision: hardwareRevision(lineID),
    radio_enabled: enabled
  })
}

export function connectData(
  lineID: string,
  apn: string,
  ipFamily: IPFamily
): Promise<boolean> {
  return updateDevice(lineID, {
    request_id: requestID(),
    operation: 'connect_data',
    expected_device_revision: hardwareRevision(lineID),
    apn,
    ip_family: ipFamily
  })
}

export function disconnectData(lineID: string): Promise<boolean> {
  return updateDevice(lineID, {
    request_id: requestID(),
    operation: 'disconnect_data',
    expected_device_revision: hardwareRevision(lineID)
  })
}

export function setVoLTEPolicy(
  lineID: string,
  policy: 'enabled' | 'disabled'
): Promise<boolean> {
  return updateDevice(lineID, {
    request_id: requestID(),
    operation: 'set_volte_policy',
    expected_device_revision: hardwareRevision(lineID),
    volte_policy: policy
  })
}

function wait(milliseconds: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, milliseconds))
}

export async function restartModem(lineID: string): Promise<boolean> {
  const target = resourceFor(lineID)
  const accepted = await updateDevice(lineID, {
    request_id: requestID(),
    operation: 'restart_modem',
    expected_device_revision: hardwareRevision(lineID)
  })
  if (!accepted) return false

  target.status = 'loading'
  target.error = ''
  await wait(1500)
  const deadline = Date.now() + 45_000
  while (Date.now() < deadline) {
    try {
      const configuration = await gateway.getDeviceConfiguration(lineID)
      if (
        configuration.hardware?.volte.policy_known &&
        !configuration.hardware.volte.restart_required
      ) {
        target.data = configuration
        target.status = 'ready'
        return true
      }
    } catch {
      // The ModemManager object normally disappears while the modem restarts.
    }
    await wait(1000)
  }
  target.status = 'error'
  target.error = translate('runtime.modemRestartTimeout')
  return false
}

export function setIncomingCallPolicy(
  lineID: string,
  policy: IncomingCallPolicy
): Promise<boolean> {
  const revision = resourceFor(lineID).data?.incoming_calls?.revision || 0
  return updateDevice(lineID, {
    operation: 'set_incoming_call_policy',
    expected_policy_revision: revision,
    incoming_call_policy: policy
  })
}
