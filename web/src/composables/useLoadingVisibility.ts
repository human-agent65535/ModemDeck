import {
  onBeforeUnmount,
  readonly,
  ref,
  toValue,
  watch,
  type MaybeRefOrGetter
} from 'vue'

export const SKELETON_REVEAL_DELAY_MS = 120
export const SKELETON_MIN_VISIBLE_MS = 180

type LoadingVisibilityOptions = {
  revealDelay?: number
  minimumVisible?: number
}

export function useLoadingVisibility(
  loading: MaybeRefOrGetter<boolean>,
  options: LoadingVisibilityOptions = {}
) {
  const active = ref(false)
  const visible = ref(false)
  const revealDelay = options.revealDelay ?? SKELETON_REVEAL_DELAY_MS
  const minimumVisible = options.minimumVisible ?? SKELETON_MIN_VISIBLE_MS
  let visibleSince = 0
  let generation = 0
  let revealTimer: ReturnType<typeof globalThis.setTimeout> | undefined
  let releaseTimer: ReturnType<typeof globalThis.setTimeout> | undefined

  function clearRevealTimer(): void {
    if (revealTimer === undefined) return
    globalThis.clearTimeout(revealTimer)
    revealTimer = undefined
  }

  function clearReleaseTimer(): void {
    if (releaseTimer === undefined) return
    globalThis.clearTimeout(releaseTimer)
    releaseTimer = undefined
  }

  function reveal(currentGeneration: number): void {
    if (currentGeneration !== generation || !active.value) return
    visibleSince = Date.now()
    visible.value = true
    revealTimer = undefined
  }

  function start(): void {
    generation += 1
    clearReleaseTimer()
    active.value = true
    if (visible.value) return
    clearRevealTimer()
    if (revealDelay <= 0) {
      reveal(generation)
      return
    }
    const currentGeneration = generation
    revealTimer = globalThis.setTimeout(
      () => reveal(currentGeneration),
      revealDelay
    )
  }

  function finish(): void {
    generation += 1
    clearRevealTimer()
    if (!visible.value) {
      active.value = false
      return
    }

    const elapsed = Date.now() - visibleSince
    const remaining = Math.max(0, minimumVisible - elapsed)
    if (remaining === 0) {
      active.value = false
      visible.value = false
      return
    }

    const currentGeneration = generation
    releaseTimer = globalThis.setTimeout(() => {
      if (currentGeneration !== generation) return
      active.value = false
      visible.value = false
      releaseTimer = undefined
    }, remaining)
  }

  watch(
    () => Boolean(toValue(loading)),
    value => {
      if (value) start()
      else finish()
    },
    { immediate: true }
  )

  onBeforeUnmount(() => {
    generation += 1
    clearRevealTimer()
    clearReleaseTimer()
  })

  return {
    active: readonly(active),
    visible: readonly(visible)
  }
}
