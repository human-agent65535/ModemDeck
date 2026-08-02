import { reactive, readonly } from 'vue'
import { fixtureMode, gateway } from '../api/client'
import type { RuntimeState } from '../api/types'
import { acceptRuntimeActiveCalls } from './call'
import { refreshUnconfirmedDeviceConfigurations } from './deviceConfiguration'
import { acceptNetworkSnapshot } from './network'
import { acceptRuntimeCallRecordings } from './recording'
import { requestApplicationVersionCheck } from './staleAssetRecovery'
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

export function initializeRuntimeEvents(): void {
  if (closeStream || fixtureMode) return

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
    onError: () => {
      if (currentGeneration !== generation) return
      state.connected = false
    }
  })
}

export function shutdownRuntimeEvents(): void {
  generation += 1
  closeStream?.()
  closeStream = undefined
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
