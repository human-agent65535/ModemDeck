import { readonly, ref } from 'vue'

type InitialLoader = () => Promise<unknown>

export function useInitialLoadBarrier() {
  const loading = ref(true)
  let generation = 0

  async function waitFor(loaders: InitialLoader[]): Promise<void> {
    const current = ++generation
    loading.value = true
    try {
      await Promise.allSettled(
        loaders.map(loader => Promise.resolve().then(loader))
      )
    } finally {
      if (current === generation) loading.value = false
    }
  }

  return {
    loading: readonly(loading),
    waitFor
  }
}
