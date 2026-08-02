import { onBeforeUnmount, watch } from 'vue'
import { runtimeEventState } from '../state/runtimeEvents'

type DurablePageRefreshOptions = {
  enabled?: () => boolean
}

export function useDurablePageRefresh(
  refresh: () => Promise<unknown> | unknown,
  options: DurablePageRefreshOptions = {}
): { request: () => Promise<void> } {
  let stopped = false
  let pending = false
  let operation: Promise<void> | undefined

  const request = (): Promise<void> => {
    if (stopped || options.enabled?.() === false) return Promise.resolve()
    pending = true
    if (!operation) {
      operation = (async () => {
        while (!stopped && pending) {
          pending = false
          try {
            await refresh()
          } catch {
            // Resource owners expose their own error state. A later watermark
            // or foreground resume is the retry signal.
          }
        }
      })().finally(() => {
        operation = undefined
      })
    }
    return operation
  }

  const stop = watch(
    () => runtimeEventState.dataWatermark,
    (watermark, previous) => {
      if (!watermark || !previous || watermark === previous) return
      void request()
    }
  )

  onBeforeUnmount(() => {
    stopped = true
    pending = false
    stop()
  })

  return { request }
}
