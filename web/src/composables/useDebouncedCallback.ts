import { getCurrentScope, onScopeDispose } from 'vue'

/**
 * Runs `callback` once `delayMs` after the last `schedule` call, with that call's
 * arguments. A pending run is dropped by `cancel` and when the owning scope ends.
 */
export function useDebouncedCallback<Args extends unknown[]>(
  callback: (...args: Args) => void,
  delayMs: number
) {
  let timer: ReturnType<typeof globalThis.setTimeout> | undefined

  function cancel(): void {
    if (timer === undefined) return
    globalThis.clearTimeout(timer)
    timer = undefined
  }

  function schedule(...args: Args): void {
    cancel()
    timer = globalThis.setTimeout(() => {
      timer = undefined
      callback(...args)
    }, delayMs)
  }

  if (getCurrentScope()) onScopeDispose(cancel)
  return { schedule, cancel }
}
