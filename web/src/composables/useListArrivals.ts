import {
  getCurrentScope,
  onScopeDispose,
  reactive,
  watch,
  type WatchSource
} from 'vue'

export type ListArrivalSnapshot<T> = {
  items: readonly T[]
  ready: boolean
  animate?: boolean
}

type ListArrivalOptions = {
  holdMilliseconds?: number
}

export function useListArrivals<T>(
  source: WatchSource<ListArrivalSnapshot<T>>,
  keyOf: (item: T) => string,
  options: ListArrivalOptions = {}
) {
  const holdMilliseconds = options.holdMilliseconds ?? 1_000
  const arriving = reactive<Record<string, boolean>>({})
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  let knownKeys = new Set<string>()
  let initialized = false

  function clearArrivals(): void {
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    for (const key of Object.keys(arriving)) delete arriving[key]
  }

  function markArriving(key: string): void {
    arriving[key] = true
    const existing = timers.get(key)
    if (existing) clearTimeout(existing)
    timers.set(key, setTimeout(() => {
      delete arriving[key]
      timers.delete(key)
    }, holdMilliseconds))
  }

  const stop = watch(
    source,
    snapshot => {
      const nextKeys = new Set(snapshot.items.map(keyOf).filter(Boolean))
      if (!snapshot.ready) {
        initialized = false
        knownKeys = nextKeys
        clearArrivals()
        return
      }

      if (!initialized) {
        initialized = true
        knownKeys = nextKeys
        return
      }

      if (snapshot.animate !== false) {
        for (const key of nextKeys) {
          if (!knownKeys.has(key)) markArriving(key)
        }
      }
      knownKeys = nextKeys
    },
    { immediate: true, flush: 'sync' }
  )

  if (getCurrentScope()) {
    onScopeDispose(() => {
      stop()
      clearArrivals()
    })
  }

  return {
    isArriving: (key: string): boolean => Boolean(arriving[key])
  }
}
