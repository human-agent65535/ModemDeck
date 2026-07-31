import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  DeviceHardwareConfiguration,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'

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

function wait(milliseconds: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, milliseconds))
}

export function useDiagnosticDevices() {
  const resources = reactive<Record<string, DiagnosticDeviceResource>>({})
  const loads = new Map<string, Promise<boolean>>()
  const generations = new Map<string, number>()

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
        }
        return true
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

    target.resetting = true
    target.error = ''
    try {
      let accepted = false
      let lastConflict: unknown
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const latest = await readHardware(normalizedLineID)
        target.hardware = latest
        target.status = 'ready'
        try {
          const updated = await gateway.resetDiagnosticUSB(
            normalizedLineID,
            latest.revision
          )
          if (!updated.hardware) {
            throw new Error(translate('runtime.invalidDeviceConfiguration'))
          }
          target.hardware = updated.hardware
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

      loads.delete(normalizedLineID)
      const generation = beginRequest(normalizedLineID)
      target.status = 'loading'
      await wait(1500)
      const deadline = Date.now() + 60_000
      while (Date.now() < deadline) {
        try {
          const hardware = await readHardware(normalizedLineID)
          if (isCurrent(normalizedLineID, generation)) {
            target.hardware = hardware
            target.status = 'ready'
          }
          return true
        } catch {
          // The modem normally disappears while USB re-enumeration is in progress.
        }
        await wait(1000)
      }
      if (isCurrent(normalizedLineID, generation)) {
        target.status = 'error'
        target.error = translate('runtime.usbResetTimeout')
      }
      return false
    } catch (error) {
      target.status = target.hardware ? 'ready' : errorStatus(error)
      target.error = errorText(error)
      return false
    } finally {
      target.resetting = false
    }
  }

  return {
    diagnosticDeviceResource: resource,
    loadDiagnosticDeviceConfiguration: load,
    resetDiagnosticUSBDevice: resetUSB
  }
}
