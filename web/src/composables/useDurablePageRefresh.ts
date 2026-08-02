import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { runtimeEventState } from '../state/runtimeEvents'

type VisibilityTarget = Pick<
  Document,
  'visibilityState' | 'addEventListener' | 'removeEventListener'
>

type DurablePageRefreshOptions = {
  enabled?: () => boolean
  documentTarget?: VisibilityTarget
}

export function useDurablePageRefresh(
  refresh: () => Promise<unknown> | unknown,
  options: DurablePageRefreshOptions = {}
): { request: () => Promise<void> } {
  let stopped = false
  let pending = false
  let operation: Promise<void> | undefined
  const documentTarget =
    options.documentTarget ||
    (typeof document === 'undefined' ? undefined : document)

  const active = (): boolean =>
    options.enabled?.() !== false &&
    (!documentTarget || documentTarget.visibilityState === 'visible')

  const request = (): Promise<void> => {
    if (stopped) return Promise.resolve()
    pending = true
    if (!active()) return Promise.resolve()
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

  const resumeVisiblePage = () => {
    if (documentTarget?.visibilityState === 'visible' && pending) void request()
  }
  documentTarget?.addEventListener('visibilitychange', resumeVisiblePage)

  const dispose = () => {
    stopped = true
    pending = false
    stop()
    documentTarget?.removeEventListener('visibilitychange', resumeVisiblePage)
  }
  if (getCurrentScope()) onScopeDispose(dispose)

  return { request }
}
