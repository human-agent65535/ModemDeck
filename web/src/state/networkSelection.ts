import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  MobileNetwork,
  MobileNetworkScan,
  NetworkSelectionMode,
  NetworkSelectionPolicy,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'

type ScanStatus = 'idle' | 'loading' | 'ready' | 'error'

export type NetworkSelectionResource = {
  policyStatus: ResourceStatus
  policy: NetworkSelectionPolicy | null
  mode: NetworkSelectionMode
  policyError: string
  saving: boolean
  selectingOperatorCode: string
  scanStatus: ScanStatus
  scan: MobileNetworkScan | null
  scanError: string
  requestSequence: number
}

export const networkSelectionState = reactive<{
  activeLineID: string
  activation: number
  resources: Record<string, NetworkSelectionResource>
}>({
  activeLineID: '',
  activation: 0,
  resources: {}
})

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : '请求失败'
}

function errorStatus(error: unknown): ResourceStatus {
  return error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
}

function resourceFor(lineID: string): NetworkSelectionResource {
  if (!networkSelectionState.resources[lineID]) {
    networkSelectionState.resources[lineID] = {
      policyStatus: 'idle',
      policy: null,
      mode: 'auto',
      policyError: '',
      saving: false,
      selectingOperatorCode: '',
      scanStatus: 'idle',
      scan: null,
      scanError: '',
      requestSequence: 0
    }
  }
  return networkSelectionState.resources[lineID]
}

function requestContext(lineID: string): {
  activation: number
  requestSequence: number
} {
  const target = resourceFor(lineID)
  target.requestSequence += 1
  return {
    activation: networkSelectionState.activation,
    requestSequence: target.requestSequence
  }
}

function isCurrentRequest(
  lineID: string,
  context: { activation: number; requestSequence: number }
): boolean {
  return (
    networkSelectionState.activeLineID === lineID &&
    networkSelectionState.activation === context.activation &&
    resourceFor(lineID).requestSequence === context.requestSequence
  )
}

function resetInterruptedResource(lineID: string): void {
  if (!lineID) return
  const target = resourceFor(lineID)
  if (target.policyStatus === 'loading') target.policyStatus = target.policy ? 'ready' : 'idle'
  if (target.scanStatus === 'loading') target.scanStatus = target.scan ? 'ready' : 'idle'
  target.saving = false
  target.selectingOperatorCode = ''
}

export function activateNetworkSelection(lineID: string): void {
  const normalizedLineID = lineID.trim()
  if (normalizedLineID === networkSelectionState.activeLineID) return
  resetInterruptedResource(networkSelectionState.activeLineID)
  networkSelectionState.activation += 1
  networkSelectionState.activeLineID = normalizedLineID
  if (!normalizedLineID) return
  const target = resourceFor(normalizedLineID)
  target.mode = target.policy?.mode || 'auto'
}

export function networkSelectionResource(lineID: string): NetworkSelectionResource {
  return resourceFor(lineID)
}

export async function loadNetworkSelection(lineID: string, force = false): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return false
  const target = resourceFor(normalizedLineID)
  if (!force && (target.policyStatus === 'ready' || target.policyStatus === 'loading')) {
    return target.policyStatus === 'ready'
  }

  const context = requestContext(normalizedLineID)
  target.policyStatus = 'loading'
  target.policyError = ''
  try {
    const policy = await gateway.getNetworkSelection(normalizedLineID)
    if (!isCurrentRequest(normalizedLineID, context)) return false
    if (policy.line_id !== normalizedLineID) {
      throw new Error('网络设置返回了错误的线路')
    }
    target.policy = policy
    target.mode = policy.mode
    target.policyStatus = 'ready'
    target.policyError = !policy.applied && policy.last_error ? policy.last_error : ''
    return true
  } catch (error) {
    if (!isCurrentRequest(normalizedLineID, context)) return false
    target.policyStatus = errorStatus(error)
    target.policyError = errorText(error)
    return false
  }
}

export async function scanMobileNetworks(lineID: string): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return false
  const target = resourceFor(normalizedLineID)
  const context = requestContext(normalizedLineID)
  target.scanStatus = 'loading'
  target.scan = null
  target.scanError = ''
  try {
    const scan = await gateway.scanMobileNetworks(normalizedLineID)
    if (!isCurrentRequest(normalizedLineID, context)) return false
    if (scan.line_id !== normalizedLineID) throw new Error('网络扫描返回了错误的线路')
    target.scan = scan
    target.scanStatus = 'ready'
    return true
  } catch (error) {
    if (!isCurrentRequest(normalizedLineID, context)) return false
    target.scanStatus = 'error'
    target.scanError = errorText(error)
    return false
  }
}

export function enterManualNetworkSelection(lineID: string): Promise<boolean> {
  const target = resourceFor(lineID)
  target.mode = 'manual'
  target.policyError = ''
  return scanMobileNetworks(lineID)
}

async function saveNetworkSelection(
  lineID: string,
  mode: NetworkSelectionMode,
  operator?: MobileNetwork
): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  const target = resourceFor(normalizedLineID)
  const policy = target.policy
  if (!normalizedLineID || !policy || target.saving) return false
  if (mode === 'manual' && (!operator || operator.status === 'forbidden')) return false

  const context = requestContext(normalizedLineID)
  const previousMode = target.mode
  target.mode = mode
  target.saving = true
  target.selectingOperatorCode = operator?.operator_code || ''
  target.policyError = ''
  try {
    const updated = await gateway.updateNetworkSelection(normalizedLineID, {
      mode,
      ...(mode === 'manual' && operator ? { operator_code: operator.operator_code } : {}),
      expected_revision: policy.revision
    })
    if (!isCurrentRequest(normalizedLineID, context)) return false
    if (updated.line_id !== normalizedLineID) {
      throw new Error('网络设置返回了错误的线路')
    }
    target.policy = updated
    target.mode = updated.mode
    target.policyStatus = 'ready'
    target.policyError = !updated.applied && updated.last_error ? updated.last_error : ''
    return true
  } catch (error) {
    if (!isCurrentRequest(normalizedLineID, context)) return false
    const saveError = errorText(error)
    try {
      const latest = await gateway.getNetworkSelection(normalizedLineID)
      if (!isCurrentRequest(normalizedLineID, context)) return false
      if (latest.line_id !== normalizedLineID) {
        throw new Error('网络设置返回了错误的线路')
      }
      target.policy = latest
      target.mode = latest.mode
      target.policyStatus = 'ready'
      target.policyError =
        !latest.applied && latest.last_error ? latest.last_error : saveError
    } catch {
      if (!isCurrentRequest(normalizedLineID, context)) return false
      target.mode = previousMode
      target.policyError = saveError
    }
    return false
  } finally {
    if (isCurrentRequest(normalizedLineID, context)) {
      target.saving = false
      target.selectingOperatorCode = ''
    }
  }
}

export function useAutomaticNetworkSelection(lineID: string): Promise<boolean> {
  return saveNetworkSelection(lineID, 'auto')
}

export function selectManualNetwork(
  lineID: string,
  network: MobileNetwork
): Promise<boolean> {
  return saveNetworkSelection(lineID, 'manual', network)
}
