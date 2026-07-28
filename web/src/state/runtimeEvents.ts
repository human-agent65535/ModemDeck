import { reactive, readonly } from 'vue'
import { fixtureMode, gateway } from '../api/client'
import type { RuntimeResource } from '../api/types'
import { requestActiveCallRefresh } from './call'
import { loadNetwork } from './network'
import {
  refreshCalls,
  refreshDeviceWorkspace,
  refreshMessageWorkspace
} from './workspace'

const ALL_RESOURCES: RuntimeResource[] = ['lines', 'network', 'calls', 'messages']
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
  lastObservedAt: ''
})

let closeStream: (() => void) | undefined
let fallbackTimer: number | undefined
let refreshQueue: RuntimeRefreshQueue | undefined
let generation = 0

export const runtimeEventState = readonly(state)

async function refreshResource(resource: RuntimeResource): Promise<void> {
  switch (resource) {
    case 'lines':
      await refreshDeviceWorkspace()
      break
    case 'network':
      await loadNetwork(true, true)
      break
    case 'calls':
      await requestActiveCallRefresh()
      await refreshCalls()
      break
    case 'messages':
      await refreshMessageWorkspace()
      break
  }
}

export function initializeRuntimeEvents(): void {
  if (closeStream || fixtureMode) return

  generation += 1
  const currentGeneration = generation
  let lastEventID = 0
  refreshQueue = createRuntimeRefreshQueue(refreshResource)
  closeStream = gateway.subscribeRuntimeEvents({
    onOpen: () => {
      if (currentGeneration === generation) state.connected = true
    },
    onReady: newestID => {
      if (currentGeneration !== generation) return
      lastEventID = Math.max(lastEventID, newestID)
      // Reconcile once at the snapshot-to-stream boundary so events emitted
      // before this subscription cannot leave the workspace stale.
      void refreshQueue?.enqueue(ALL_RESOURCES)
    },
    onEvent: event => {
      if (currentGeneration !== generation || event.id <= lastEventID) return
      lastEventID = event.id
      state.lastObservedAt = event.observed_at
      void refreshQueue?.enqueue(event.resources)
    },
    onReset: (_oldestID, newestID) => {
      if (currentGeneration !== generation) return
      lastEventID = newestID
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
  state.lastObservedAt = ''
}
