import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  DeviceConfiguration,
  DeviceHardwareConfiguration,
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

type IncomingPolicyUpdate = Extract<
  UpdateDeviceConfigurationInput,
  { operation: 'set_incoming_call_policy' }
>

type HardwareUpdateInput = Exclude<
  UpdateDeviceConfigurationInput,
  { operation: 'set_incoming_call_policy' }
>

type HardwareUpdateIntent =
  | { operation: 'set_radio_enabled'; radio_enabled: boolean }
  | { operation: 'connect_data'; apn: string; ip_family: IPFamily }
  | { operation: 'disconnect_data' }
  | { operation: 'set_volte_policy'; volte_policy: 'enabled' | 'disabled' }
  | { operation: 'restart_modem' }
  | { operation: 'reset_usb' }

type DeviceUpdateIntent = IncomingPolicyUpdate | HardwareUpdateIntent

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

const deviceConfigurationLoads = new Map<string, Promise<boolean>>()
const deviceConfigurationGenerations = new Map<string, number>()

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
}

function deviceConfigurationErrorText(
  error: unknown,
  input: DeviceUpdateIntent,
  hardware?: DeviceHardwareConfiguration
): string {
  if (
    error instanceof ApiError &&
    error.code === 'conflict' &&
    input.operation !== 'set_incoming_call_policy'
  ) {
    return translate('runtime.deviceConfigurationChanged')
  }
  if (
    error instanceof ApiError &&
    error.code === 'failed_precondition' &&
    input.operation === 'connect_data' &&
    hardware
  ) {
    if (!hardware.radio.enabled_known || !hardware.flight_mode_known) {
      return translate('device.radioStateUnavailableForData')
    }
    if (hardware.flight_mode) {
      return translate('runtime.turnOffFlightModeForData')
    }
    if (!hardware.radio.enabled) {
      return translate('runtime.waitForRadioRecoveryForData')
    }
  }
  return errorText(error)
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

function beginDeviceConfigurationRequest(lineID: string): number {
  const generation = (deviceConfigurationGenerations.get(lineID) || 0) + 1
  deviceConfigurationGenerations.set(lineID, generation)
  return generation
}

function isCurrentDeviceConfigurationRequest(lineID: string, generation: number): boolean {
  return deviceConfigurationGenerations.get(lineID) === generation
}

function beginDeviceRecovery(lineID: string): number {
  deviceConfigurationLoads.delete(lineID)
  return beginDeviceConfigurationRequest(lineID)
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
  if (!force && target.status === 'ready') return true
  const pending = deviceConfigurationLoads.get(normalizedLineID)
  if (!force && pending) return pending

  const hasConfiguration = target.data !== null
  if (!hasConfiguration) target.status = 'loading'
  target.error = ''
  const generation = beginDeviceConfigurationRequest(normalizedLineID)
  let operation: Promise<boolean> = Promise.resolve(false)
  operation = (async () => {
    try {
      const configuration = await gateway.getDeviceConfiguration(normalizedLineID)
      if (!configuration.hardware || !configuration.incoming_calls) {
        throw new Error(translate('runtime.invalidDeviceConfiguration'))
      }
      if (isCurrentDeviceConfigurationRequest(normalizedLineID, generation)) {
        target.data = configuration
        target.status = 'ready'
      }
      return true
    } catch (error) {
      if (isCurrentDeviceConfigurationRequest(normalizedLineID, generation)) {
        target.status = hasConfiguration ? 'ready' : errorStatus(error)
        target.error = errorText(error)
      }
      return false
    } finally {
      if (deviceConfigurationLoads.get(normalizedLineID) === operation) {
        deviceConfigurationLoads.delete(normalizedLineID)
      }
    }
  })()
  deviceConfigurationLoads.set(normalizedLineID, operation)
  return operation
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
  input: DeviceUpdateIntent
): Promise<boolean> {
  const target = resourceFor(lineID)
  if (!target.data || target.savingOperation) return false
  target.savingOperation = input.operation
  target.error = ''
  try {
    const updated =
      input.operation === 'set_incoming_call_policy'
        ? await gateway.updateDeviceConfiguration(lineID, input)
        : await applyHardwareUpdate(lineID, target, input)
    target.data = mergeConfiguration(target.data, updated)
    target.status = 'ready'
    return true
  } catch (error) {
    await restoreDeviceConfiguration(
      lineID,
      target,
      deviceConfigurationErrorText(error, input, target.data?.hardware)
    )
    return false
  } finally {
    target.savingOperation = ''
  }
}

async function applyHardwareUpdate(
  lineID: string,
  target: DeviceConfigurationResource,
  input: HardwareUpdateIntent
): Promise<DeviceConfiguration> {
  let lastConflict: unknown
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const latest = await gateway.getDeviceConfiguration(lineID)
    if (!latest.hardware || !latest.incoming_calls) {
      throw new Error(translate('runtime.invalidDeviceConfiguration'))
    }
    target.data = mergeConfiguration(target.data, latest)
    target.status = 'ready'

    const request = {
      ...input,
      request_id: requestID(),
      expected_device_revision: latest.hardware.revision
    } as HardwareUpdateInput
    try {
      return await gateway.updateDeviceConfiguration(lineID, request)
    } catch (error) {
      if (!(error instanceof ApiError) || error.code !== 'conflict' || attempt > 0) {
        throw error
      }
      lastConflict = error
    }
  }
  if (lastConflict) throw lastConflict
  throw new Error(translate('runtime.requestFailed'))
}

export function setRadioEnabled(lineID: string, enabled: boolean): Promise<boolean> {
  return updateDevice(lineID, {
    operation: 'set_radio_enabled',
    radio_enabled: enabled
  })
}

export function connectData(
  lineID: string,
  apn: string,
  ipFamily: IPFamily
): Promise<boolean> {
  return updateDevice(lineID, {
    operation: 'connect_data',
    apn,
    ip_family: ipFamily
  })
}

export function disconnectData(lineID: string): Promise<boolean> {
  return updateDevice(lineID, {
    operation: 'disconnect_data'
  })
}

export function setVoLTEPolicy(
  lineID: string,
  policy: 'enabled' | 'disabled'
): Promise<boolean> {
  return updateDevice(lineID, {
    operation: 'set_volte_policy',
    volte_policy: policy
  })
}

function wait(milliseconds: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, milliseconds))
}

export async function restartModem(lineID: string): Promise<boolean> {
  const target = resourceFor(lineID)
  const accepted = await updateDevice(lineID, {
    operation: 'restart_modem'
  })
  if (!accepted) return false

  const generation = beginDeviceRecovery(lineID)
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
        if (isCurrentDeviceConfigurationRequest(lineID, generation)) {
          target.data = configuration
          target.status = 'ready'
        }
        return true
      }
    } catch {
      // The ModemManager object normally disappears while the modem restarts.
    }
    await wait(1000)
  }
  if (isCurrentDeviceConfigurationRequest(lineID, generation)) {
    target.status = 'error'
    target.error = translate('runtime.modemRestartTimeout')
  }
  return false
}

export async function resetUSBDevice(lineID: string): Promise<boolean> {
  const target = resourceFor(lineID)
  const accepted = await updateDevice(lineID, {
    operation: 'reset_usb'
  })
  if (!accepted) return false

  const generation = beginDeviceRecovery(lineID)
  target.status = 'loading'
  target.error = ''
  await wait(1500)
  const deadline = Date.now() + 60_000
  while (Date.now() < deadline) {
    try {
      const configuration = await gateway.getDeviceConfiguration(lineID)
      if (configuration.hardware) {
        if (isCurrentDeviceConfigurationRequest(lineID, generation)) {
          target.data = configuration
          target.status = 'ready'
        }
        return true
      }
    } catch {
      // The ModemManager object disappears while USB re-enumeration is in progress.
    }
    await wait(1000)
  }
  if (isCurrentDeviceConfigurationRequest(lineID, generation)) {
    target.status = 'error'
    target.error = translate('runtime.usbResetTimeout')
  }
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
