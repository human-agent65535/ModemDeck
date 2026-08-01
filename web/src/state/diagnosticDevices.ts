import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  DeviceHardwareConfiguration,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import {
  isExpectedDeviceRecoveryOutage,
  waitForUnavailableThenReadable
} from './deviceRecovery'

export type DiagnosticDeviceResource = {
  status: ResourceStatus
  hardware: DeviceHardwareConfiguration | null
  error: string
  resetting: boolean
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
}

function errorStatus(error: unknown): ResourceStatus {
  return error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
}

export function useDiagnosticDevices() {
  const resources = reactive<Record<string, DiagnosticDeviceResource>>({})
  const loads = new Map<string, Promise<boolean>>()
  const generations = new Map<string, number>()
  const unconfirmedRecoveries = new Set<string>()

  function resource(lineID: string): DiagnosticDeviceResource {
    const normalizedLineID = lineID.trim()
    if (!resources[normalizedLineID]) {
      resources[normalizedLineID] = {
        status: 'idle',
        hardware: null,
        error: '',
        resetting: false
      }
    }
    return resources[normalizedLineID]
  }

  function beginRequest(lineID: string): number {
    const generation = (generations.get(lineID) || 0) + 1
    generations.set(lineID, generation)
    return generation
  }

  function isCurrent(lineID: string, generation: number): boolean {
    return generations.get(lineID) === generation
  }

  async function readHardware(lineID: string): Promise<DeviceHardwareConfiguration> {
    const configuration = await gateway.getDiagnosticDeviceConfiguration(lineID)
    if (!configuration.hardware) {
      throw new Error(translate('runtime.invalidDeviceConfiguration'))
    }
    return configuration.hardware
  }

  async function load(
    lineID: string,
    force = false
  ): Promise<boolean> {
    const normalizedLineID = lineID.trim()
    if (!normalizedLineID) return false
    const target = resource(normalizedLineID)
    if (target.resetting) return target.status === 'ready'
    if (!force && target.status === 'ready') return true
    const pending = loads.get(normalizedLineID)
    if (!force && pending) return pending

    const hasHardware = target.hardware !== null
    if (!hasHardware) target.status = 'loading'
    target.error = ''
    const generation = beginRequest(normalizedLineID)
    let operation: Promise<boolean> = Promise.resolve(false)
    operation = (async () => {
      try {
        const hardware = await readHardware(normalizedLineID)
        if (isCurrent(normalizedLineID, generation)) {
          target.hardware = hardware
          target.status = 'ready'
          unconfirmedRecoveries.delete(normalizedLineID)
          return true
        }
        return false
      } catch (error) {
        if (isCurrent(normalizedLineID, generation)) {
          target.status = hasHardware ? 'ready' : errorStatus(error)
          target.error = errorText(error)
        }
        return false
      } finally {
        if (loads.get(normalizedLineID) === operation) {
          loads.delete(normalizedLineID)
        }
      }
    })()
    loads.set(normalizedLineID, operation)
    return operation
  }

  async function resetUSB(lineID: string): Promise<boolean> {
    const normalizedLineID = lineID.trim()
    if (!normalizedLineID) return false
    const target = resource(normalizedLineID)
    if (!target.hardware || target.resetting) return false

    loads.delete(normalizedLineID)
    const generation = beginRequest(normalizedLineID)
    target.resetting = true
    target.error = ''
    unconfirmedRecoveries.delete(normalizedLineID)
    try {
      let accepted = false
      let lastConflict: unknown
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const latest = await readHardware(normalizedLineID)
        if (isCurrent(normalizedLineID, generation)) {
          target.hardware = latest
          target.status = 'ready'
        }
        try {
          const updated = await gateway.resetDiagnosticUSB(
            normalizedLineID,
            latest.revision
          )
          if (!updated.hardware) {
            throw new Error(translate('runtime.invalidDeviceConfiguration'))
          }
          accepted = true
          break
        } catch (error) {
          if (
            error instanceof ApiError &&
            error.code === 'conflict' &&
            attempt === 0
          ) {
            lastConflict = error
            continue
          }
          throw error
        }
      }
      if (!accepted) {
        throw lastConflict || new Error(translate('runtime.requestFailed'))
      }

      const result = await waitForUnavailableThenReadable({
        read: () => readHardware(normalizedLineID),
        isCurrent: () => isCurrent(normalizedLineID, generation),
        isUnavailable: isExpectedDeviceRecoveryOutage,
        timeoutMs: 60_000,
        intervalMs: 1_000
      })
      if (result.status === 'recovered') {
        if (isCurrent(normalizedLineID, generation)) {
          target.hardware = result.value
          target.status = 'ready'
          unconfirmedRecoveries.delete(normalizedLineID)
          return true
        }
        return false
      }
      if (result.status === 'timeout' && isCurrent(normalizedLineID, generation)) {
        target.status = 'ready'
        target.error = translate('runtime.usbResetTimeout')
        if (result.observedUnavailable) unconfirmedRecoveries.add(normalizedLineID)
      }
      return false
    } catch (error) {
      if (isCurrent(normalizedLineID, generation)) {
        target.status = target.hardware ? 'ready' : errorStatus(error)
        target.error = errorText(error)
      }
      return false
    } finally {
      if (isCurrent(normalizedLineID, generation)) target.resetting = false
    }
  }

  async function refreshUnconfirmed(lineIDs: Iterable<string>): Promise<void> {
    const available = new Set(Array.from(lineIDs, lineID => lineID.trim()))
    await Promise.all(
      Array.from(unconfirmedRecoveries).map(async lineID => {
        if (!available.has(lineID)) return
        const pending = loads.get(lineID)
        if (pending) {
          await pending
          return
        }
        await load(lineID, true)
      })
    )
  }

  return {
    diagnosticDeviceResource: resource,
    loadDiagnosticDeviceConfiguration: load,
    resetDiagnosticUSBDevice: resetUSB,
    refreshUnconfirmedDiagnosticDevices: refreshUnconfirmed
  }
}
