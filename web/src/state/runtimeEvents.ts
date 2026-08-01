import { reactive, readonly } from 'vue'
import type { Router } from 'vue-router'
import { fixtureMode, gateway } from '../api/client'
import type { RuntimeResource } from '../api/types'
import { visibleMessageThreadKey } from '../router/messageRoute'
import { requestActiveCallRefresh } from './call'
import { refreshUnconfirmedDeviceConfigurations } from './deviceConfiguration'
import { loadNetwork } from './network'
import { refreshRecordingWorkspace } from './recording'
import { refreshSession } from './session'
import { requestApplicationVersionCheck } from './staleAssetRecovery'
import {
  refreshContacts,
  refreshCalls,
  refreshDeviceWorkspace,
  refreshMessageWorkspace
} from './workspace'

const ALL_RESOURCES: RuntimeResource[] = [
  'session',
  'lines',
  'network',
  'calls',
  'messages',
  'contacts',
  'recordings'
]
const FALLBACK_REFRESH_MS = 30_000

type RuntimeResourceRefresher = (resource: RuntimeResource) => Promise<void>

export type RuntimeRefreshQueue = {
  enqueue: (resources: Iterable<RuntimeResource>) => Promise<void>
  stop: () => void
}

export function createRuntimeRefreshQueue(
  refresh: RuntimeResourceRefresher
): RuntimeRefreshQueue {
  const queued = new Set<RuntimeResource>()
  let stopped = false
  let scheduled = false
  let operation = Promise.resolve()

  const drain = async () => {
    try {
      while (!stopped && queued.size > 0) {
        const resources = Array.from(queued)
        queued.clear()
        await Promise.allSettled(resources.map(resource => refresh(resource)))
      }
    } finally {
      scheduled = false
    }
  }

  return {
    enqueue(resources) {
      if (stopped) return operation
      for (const resource of resources) queued.add(resource)
      if (!scheduled && queued.size > 0) {
        scheduled = true
        operation = operation.then(drain, drain)
      }
      return operation
    },
    stop() {
      stopped = true
      queued.clear()
    }
  }
}

const state = reactive({
  connected: false,
  lastHeartbeatAt: '',
  lastObservedAt: ''
})

let closeStream: (() => void) | undefined
let fallbackTimer: number | undefined
let refreshQueue: RuntimeRefreshQueue | undefined
let generation = 0

export const runtimeEventState = readonly(state)

async function refreshResource(
  resource: RuntimeResource,
  router: Router
): Promise<void> {
  switch (resource) {
    case 'session':
      await refreshSession()
      break
    case 'lines':
      await refreshDeviceWorkspace()
      await refreshUnconfirmedDeviceConfigurations()
      break
    case 'network':
      await loadNetwork(true, true)
      break
    case 'calls':
      await requestActiveCallRefresh()
      await refreshCalls()
      break
    case 'messages':
      await refreshMessageWorkspace(
        visibleMessageThreadKey(router.currentRoute.value)
      )
      break
    case 'contacts':
      await refreshContacts()
      break
    case 'recordings':
      await refreshRecordingWorkspace()
      break
  }
}

export function initializeRuntimeEvents(router: Router): void {
  if (closeStream || fixtureMode) return

  generation += 1
  const currentGeneration = generation
  let initialized = false
  refreshQueue = createRuntimeRefreshQueue(resource =>
    refreshResource(resource, router)
  )
  closeStream = gateway.subscribeRuntimeEvents({
    onOpen: () => {
      if (currentGeneration !== generation) return
      state.connected = true
      void refreshQueue?.enqueue(['calls'])
    },
    onHeartbeat: observedAt => {
      if (currentGeneration !== generation) return
      state.connected = true
      state.lastHeartbeatAt = observedAt
    },
    onReady: () => {
      if (currentGeneration !== generation) return
      // Reconcile once at the snapshot-to-stream boundary so events emitted
      // before this subscription cannot leave the workspace stale.
      if (!initialized) {
        initialized = true
        void refreshQueue?.enqueue(ALL_RESOURCES)
      }
      requestApplicationVersionCheck()
    },
    onEvent: event => {
      if (currentGeneration !== generation) return
      state.lastObservedAt = event.observed_at
      void refreshQueue?.enqueue(event.resources)
    },
    onReset: () => {
      if (currentGeneration !== generation) return
      void refreshQueue?.enqueue(ALL_RESOURCES)
    },
    onError: () => {
      if (currentGeneration !== generation) return
      state.connected = false
    }
  })

  fallbackTimer = window.setInterval(() => {
    if (currentGeneration !== generation || state.connected) return
    void refreshQueue?.enqueue(ALL_RESOURCES)
  }, FALLBACK_REFRESH_MS)
}

export function shutdownRuntimeEvents(): void {
  generation += 1
  closeStream?.()
  closeStream = undefined
  if (fallbackTimer !== undefined) window.clearInterval(fallbackTimer)
  fallbackTimer = undefined
  refreshQueue?.stop()
  refreshQueue = undefined
  state.connected = false
  state.lastHeartbeatAt = ''
  state.lastObservedAt = ''
}
