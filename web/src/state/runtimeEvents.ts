import { reactive, readonly } from 'vue'
import { gateway } from '../api/client'
import type { RuntimeState, UpdateOperation } from '../api/types'
import { acceptRuntimeActiveCalls } from './call'
import { refreshUnconfirmedDeviceConfigurations } from './deviceConfiguration'
import { acceptNetworkSnapshot } from './network'
import { acceptRuntimeCallRecordings } from './recording'
import {
  requestApplicationVersionCheck,
  setApplicationUpdateNoticeSuppressed
} from './staleAssetRecovery'
import { acceptRuntimeCommunicationState } from './workspace'

const state = reactive({
  connected: false,
  lastHeartbeatAt: '',
  lastObservedAt: '',
  epoch: '',
  dataRevision: 0,
  dataWatermark: ''
})

let closeStream: (() => void) | undefined
let generation = 0
let initialized = false
let lastEpoch = ''
let lastRevision = -1
let lastCommunication = ''
let lastCalls = ''
let monitoredUpdateOperationID = ''
const updateOperationListeners = new Set<(operation: UpdateOperation) => void>()

export const runtimeEventState = readonly(state)

function stateSignature(value: unknown): string {
  return value === undefined ? '' : JSON.stringify(value)
}

export function acceptRuntimeState(runtime: RuntimeState): void {
  if (runtime.epoch === lastEpoch && runtime.revision < lastRevision) return

  const wasInitialized = initialized
  const processChanged = wasInitialized && runtime.epoch !== lastEpoch

  initialized = true
  lastEpoch = runtime.epoch
  lastRevision = runtime.revision
  state.connected = true
  state.lastObservedAt = runtime.observed_at
  state.epoch = runtime.epoch
  state.dataRevision = runtime.data_revision
  state.dataWatermark = `${runtime.epoch}:${runtime.data_revision}`

  if (runtime.communication) {
    const signature = stateSignature(runtime.communication)
    if (signature !== lastCommunication) {
      lastCommunication = signature
      acceptRuntimeCommunicationState(runtime.communication)
      void refreshUnconfirmedDeviceConfigurations()
    }
  }
  if (runtime.network) {
    acceptNetworkSnapshot(
      runtime.network.status,
      runtime.network.proxies,
      true
    )
  }
  if (runtime.calls) {
    const signature = stateSignature(runtime.calls)
    if (signature !== lastCalls) {
      lastCalls = signature
      acceptRuntimeActiveCalls(runtime.calls)
    }
  }
  if (runtime.recordings) {
    acceptRuntimeCallRecordings(runtime.recordings)
  }
  if (!wasInitialized || processChanged) requestApplicationVersionCheck()
}

function openRuntimeEvents(): void {
  generation += 1
  const currentGeneration = generation
  closeStream = gateway.subscribeRuntimeEvents({
    onOpen: () => {
      if (currentGeneration !== generation) return
      state.connected = true
    },
    onHeartbeat: observedAt => {
      if (currentGeneration !== generation) return
      state.connected = true
      state.lastHeartbeatAt = observedAt
    },
    onState: runtime => {
      if (currentGeneration !== generation) return
      acceptRuntimeState(runtime)
    },
    onUpdateOperation: operation => {
      if (currentGeneration !== generation) return
      for (const listener of updateOperationListeners) listener(operation)
      if (
        operation.id === monitoredUpdateOperationID &&
        (operation.state === 'succeeded' || operation.state === 'failed')
      ) {
        monitorRuntimeUpdateOperation()
      }
    },
    onError: () => {
      if (currentGeneration !== generation) return
      state.connected = false
    }
  }, monitoredUpdateOperationID)
}

export function initializeRuntimeEvents(): void {
  if (closeStream) return
  openRuntimeEvents()
}

export function monitorRuntimeUpdateOperation(operationID = ''): void {
  const normalizedOperationID = operationID.trim()
  if (normalizedOperationID === monitoredUpdateOperationID) {
    setApplicationUpdateNoticeSuppressed(Boolean(normalizedOperationID))
    return
  }
  monitoredUpdateOperationID = normalizedOperationID
  setApplicationUpdateNoticeSuppressed(Boolean(normalizedOperationID))
  if (!closeStream) return
  generation += 1
  closeStream()
  closeStream = undefined
  openRuntimeEvents()
}

export function subscribeRuntimeUpdateOperations(
  listener: (operation: UpdateOperation) => void
): () => void {
  updateOperationListeners.add(listener)
  return () => updateOperationListeners.delete(listener)
}

export function shutdownRuntimeEvents(): void {
  generation += 1
  closeStream?.()
  closeStream = undefined
  monitoredUpdateOperationID = ''
  setApplicationUpdateNoticeSuppressed(false)
  updateOperationListeners.clear()
  initialized = false
  lastEpoch = ''
  lastRevision = -1
  lastCommunication = ''
  lastCalls = ''
  state.connected = false
  state.lastHeartbeatAt = ''
  state.lastObservedAt = ''
  state.epoch = ''
  state.dataRevision = 0
  state.dataWatermark = ''
}
