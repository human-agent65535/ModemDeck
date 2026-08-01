import { reactive, readonly } from 'vue'
import type { Router } from 'vue-router'
import { fixtureMode, gateway } from '../api/client'
import type { RuntimeState } from '../api/types'
import { visibleMessageThreadKey } from '../router/messageRoute'
import { acceptRuntimeActiveCalls } from './call'
import { refreshUnconfirmedDeviceConfigurations } from './deviceConfiguration'
import { acceptNetworkSnapshot } from './network'
import {
  acceptRuntimeCallRecordings,
  refreshRecordingWorkspace
} from './recording'
import { refreshSession } from './session'
import { requestApplicationVersionCheck } from './staleAssetRecovery'
import {
  acceptRuntimeCommunicationState,
  refreshCalls,
  refreshContacts,
  refreshDeviceWorkspace,
  refreshMessageWorkspace
} from './workspace'

const state = reactive({
  connected: false,
  lastHeartbeatAt: '',
  lastObservedAt: ''
})

let closeStream: (() => void) | undefined
let generation = 0
let initialized = false
let lastEpoch = ''
let lastRevision = -1
let lastDataRevision = 0
let lastCommunication = ''
let lastCalls = ''
let durableRefreshPending = false
let durableRefreshOperation: Promise<void> | undefined

export const runtimeEventState = readonly(state)

function stateSignature(value: unknown): string {
  return value === undefined ? '' : JSON.stringify(value)
}

function requestDurableRefresh(router: Router): Promise<void> {
  durableRefreshPending = true
  if (!durableRefreshOperation) {
    durableRefreshOperation = (async () => {
      while (durableRefreshPending) {
        durableRefreshPending = false
        await Promise.allSettled([
          refreshSession(),
          refreshDeviceWorkspace(),
          refreshContacts(),
          refreshCalls(),
          refreshMessageWorkspace(
            visibleMessageThreadKey(router.currentRoute.value)
          ),
          refreshRecordingWorkspace(false)
        ])
      }
    })().finally(() => {
      durableRefreshOperation = undefined
      if (durableRefreshPending) void requestDurableRefresh(router)
    })
  }
  return durableRefreshOperation
}

export function acceptRuntimeState(runtime: RuntimeState, router: Router): void {
  if (runtime.epoch === lastEpoch && runtime.revision < lastRevision) return

  const wasInitialized = initialized
  const processChanged = wasInitialized && runtime.epoch !== lastEpoch
  const durableChanged =
    wasInitialized &&
    (processChanged ||
      (runtime.epoch === lastEpoch && runtime.data_revision > lastDataRevision))

  initialized = true
  lastEpoch = runtime.epoch
  lastRevision = runtime.revision
  lastDataRevision = runtime.data_revision
  state.connected = true
  state.lastObservedAt = runtime.observed_at

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
  if (durableChanged) void requestDurableRefresh(router)
  if (!wasInitialized || processChanged) requestApplicationVersionCheck()
}

export function initializeRuntimeEvents(router: Router): void {
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
      acceptRuntimeState(runtime, router)
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
  lastDataRevision = 0
  lastCommunication = ''
  lastCalls = ''
  durableRefreshPending = false
  durableRefreshOperation = undefined
  state.connected = false
  state.lastHeartbeatAt = ''
  state.lastObservedAt = ''
}
