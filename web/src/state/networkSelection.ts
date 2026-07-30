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
import { translate } from '../i18n'

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
  policyRequestSequence: number
  scanRequestSequence: number
}

export const networkSelectionState = reactive<{
  activeLineID: string
  resources: Record<string, NetworkSelectionResource>
}>({
  activeLineID: '',
  resources: {}
})

type PendingNetworkScan = {
  controller: AbortController
  promise: Promise<boolean>
}

const pendingNetworkScans = new Map<string, PendingNetworkScan>()

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : translate('runtime.requestFailed')
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
      policyRequestSequence: 0,
      scanRequestSequence: 0
    }
  }
  return networkSelectionState.resources[lineID]
}

function policyRequestContext(lineID: string): number {
  const target = resourceFor(lineID)
  target.policyRequestSequence += 1
  return target.policyRequestSequence
}

function isCurrentPolicyRequest(lineID: string, requestSequence: number): boolean {
  return resourceFor(lineID).policyRequestSequence === requestSequence
}

function scanRequestContext(lineID: string): number {
  const target = resourceFor(lineID)
  target.scanRequestSequence += 1
  return target.scanRequestSequence
}

function isCurrentScanRequest(lineID: string, requestSequence: number): boolean {
  return resourceFor(lineID).scanRequestSequence === requestSequence
}

export function activateNetworkSelection(lineID: string): void {
  const normalizedLineID = lineID.trim()
  if (normalizedLineID === networkSelectionState.activeLineID) return
  networkSelectionState.activeLineID = normalizedLineID
  if (normalizedLineID) resourceFor(normalizedLineID)
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

  const requestSequence = policyRequestContext(normalizedLineID)
  target.policyStatus = 'loading'
  target.policyError = ''
  try {
    const policy = await gateway.getNetworkSelection(normalizedLineID)
    if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
    if (policy.line_id !== normalizedLineID) {
      throw new Error(translate('runtime.invalidNetworkLine'))
    }
    target.policy = policy
    target.mode = policy.mode
    target.policyStatus = 'ready'
    target.policyError = !policy.applied && policy.last_error ? policy.last_error : ''
    return true
  } catch (error) {
    if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
    target.policyStatus = errorStatus(error)
    target.policyError = errorText(error)
    return false
  }
}

export function scanMobileNetworks(lineID: string): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return Promise.resolve(false)
  const pendingScan = pendingNetworkScans.get(normalizedLineID)
  if (pendingScan) return pendingScan.promise

  const target = resourceFor(normalizedLineID)
  const requestSequence = scanRequestContext(normalizedLineID)
  const controller = new AbortController()
  target.scanStatus = 'loading'
  target.scan = null
  target.scanError = ''

  const request = (async () => {
    try {
      const scan = await gateway.scanMobileNetworks(normalizedLineID, controller.signal)
      if (!isCurrentScanRequest(normalizedLineID, requestSequence)) return false
      if (scan.line_id !== normalizedLineID) {
        throw new Error(translate('runtime.invalidNetworkScanLine'))
      }
      target.scan = scan
      target.scanStatus = 'ready'
      return true
    } catch (error) {
      if (!isCurrentScanRequest(normalizedLineID, requestSequence)) return false
      if (controller.signal.aborted) {
        target.scanStatus = target.scan ? 'ready' : 'idle'
        target.scanError = ''
        return false
      }
      target.scanStatus = 'error'
      target.scanError = errorText(error)
      return false
    } finally {
      if (pendingNetworkScans.get(normalizedLineID)?.controller === controller) {
        pendingNetworkScans.delete(normalizedLineID)
      }
    }
  })()
  pendingNetworkScans.set(normalizedLineID, { controller, promise: request })
  return request
}

async function cancelMobileNetworkScan(lineID: string): Promise<void> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return
  const pendingScan = pendingNetworkScans.get(normalizedLineID)
  if (!pendingScan) return
  pendingScan.controller.abort()
  await pendingScan.promise
}

export function enterManualNetworkSelection(lineID: string): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return Promise.resolve(false)
  const target = resourceFor(normalizedLineID)
  target.mode = 'manual'
  target.policyError = ''
  return scanMobileNetworks(normalizedLineID)
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

  const requestSequence = policyRequestContext(normalizedLineID)
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
    if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
    if (updated.line_id !== normalizedLineID) {
      throw new Error(translate('runtime.invalidNetworkLine'))
    }
    target.policy = updated
    target.mode = updated.mode
    target.policyStatus = 'ready'
    target.policyError = !updated.applied && updated.last_error ? updated.last_error : ''
    return true
  } catch (error) {
    if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
    const saveError = errorText(error)
    try {
      const latest = await gateway.getNetworkSelection(normalizedLineID)
      if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
      if (latest.line_id !== normalizedLineID) {
        throw new Error(translate('runtime.invalidNetworkLine'), { cause: error })
      }
      target.policy = latest
      target.mode = latest.mode
      target.policyStatus = 'ready'
      target.policyError =
        !latest.applied && latest.last_error ? latest.last_error : saveError
    } catch {
      if (!isCurrentPolicyRequest(normalizedLineID, requestSequence)) return false
      target.mode = previousMode
      target.policyError = saveError
    }
    return false
  } finally {
    if (isCurrentPolicyRequest(normalizedLineID, requestSequence)) {
      target.saving = false
      target.selectingOperatorCode = ''
    }
  }
}

export async function useAutomaticNetworkSelection(lineID: string): Promise<boolean> {
  const normalizedLineID = lineID.trim()
  if (!normalizedLineID) return false
  const target = resourceFor(normalizedLineID)
  if (target.saving) return false

  target.mode = 'auto'
  if (pendingNetworkScans.has(normalizedLineID)) {
    target.saving = true
    await cancelMobileNetworkScan(normalizedLineID)
    target.saving = false
  }
  if (target.policy?.mode === 'auto') {
    target.policyError = ''
    return true
  }
  return saveNetworkSelection(normalizedLineID, 'auto')
}

export function selectManualNetwork(
  lineID: string,
  network: MobileNetwork
): Promise<boolean> {
  return saveNetworkSelection(lineID, 'manual', network)
}
