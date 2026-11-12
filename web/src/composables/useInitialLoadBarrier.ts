import { readonly, ref } from 'vue'

type InitialLoader = () => Promise<unknown>

// Every read request already ends within the client's read timeout. The barrier
// keeps its own, slightly longer limit so a loader that never settles for any
// other reason cannot hold the skeleton forever; once it passes, the page shows
// each resource's own loading or error state.
export const INITIAL_LOAD_BARRIER_TIMEOUT_MS = 20_000

export function useInitialLoadBarrier(timeoutMilliseconds = INITIAL_LOAD_BARRIER_TIMEOUT_MS) {
  const loading = ref(true)
  let generation = 0

  async function waitFor(loaders: InitialLoader[]): Promise<void> {
    const current = ++generation
    loading.value = true
    let timer: ReturnType<typeof setTimeout> | undefined
    try {
      await Promise.race([
        Promise.allSettled(
          loaders.map(loader => Promise.resolve().then(loader))
        ),
        new Promise<void>(resolve => {
          timer = setTimeout(resolve, timeoutMilliseconds)
        })
      ])
    } finally {
      clearTimeout(timer)
      if (current === generation) loading.value = false
    }
  }

  return {
    loading: readonly(loading),
    waitFor
  }
}
