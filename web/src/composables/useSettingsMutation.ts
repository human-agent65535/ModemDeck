import { computed, onBeforeUnmount, ref } from 'vue'
import { showError, showSuccess } from '../state/feedback'

export type SettingsMutationStatus = 'idle' | 'saving' | 'saved' | 'error'
export type SettingsMutationToast = 'none' | 'success' | 'error' | 'both'

type MessageSource = string | (() => string)

type SettingsMutationOptions = {
  errorMessage: (cause: unknown) => string
  successMessage?: MessageSource
  toast?: SettingsMutationToast
  savedDuration?: number
}

export type SettingsMutationResult<T> =
  | { ok: true; value: T }
  | { ok: false; error: string }

function messageValue(source?: MessageSource): string {
  return typeof source === 'function' ? source() : source || ''
}

export function useSettingsMutation(options: SettingsMutationOptions) {
  const status = ref<SettingsMutationStatus>('idle')
  const error = ref('')
  const toast = options.toast || 'error'
  const savedDuration = options.savedDuration ?? 2200
  let generation = 0
  let resetTimer: ReturnType<typeof globalThis.setTimeout> | undefined

  function clearResetTimer(): void {
    if (!resetTimer) return
    globalThis.clearTimeout(resetTimer)
    resetTimer = undefined
  }

  function reset(): void {
    generation += 1
    clearResetTimer()
    status.value = 'idle'
    error.value = ''
  }

  function markSaving(): number {
    generation += 1
    clearResetTimer()
    status.value = 'saving'
    error.value = ''
    return generation
  }

  function markSaved(currentGeneration = generation): void {
    if (currentGeneration !== generation) return
    status.value = 'saved'
    error.value = ''
    const successMessage = messageValue(options.successMessage)
    if ((toast === 'success' || toast === 'both') && successMessage) {
      showSuccess(successMessage)
    }
    resetTimer = globalThis.setTimeout(() => {
      if (currentGeneration === generation) status.value = 'idle'
      resetTimer = undefined
    }, savedDuration)
  }

  function markError(cause: unknown, currentGeneration = generation): string {
    if (currentGeneration !== generation) return error.value
    const message = options.errorMessage(cause)
    status.value = 'error'
    error.value = message
    if (toast === 'error' || toast === 'both') showError(message)
    return message
  }

  async function run<T>(operation: () => Promise<T>): Promise<SettingsMutationResult<T>> {
    const currentGeneration = markSaving()
    try {
      const value = await operation()
      markSaved(currentGeneration)
      return { ok: true, value }
    } catch (cause) {
      return {
        ok: false,
        error: markError(cause, currentGeneration)
      }
    }
  }

  onBeforeUnmount(() => {
    generation += 1
    clearResetTimer()
  })

  return {
    status,
    error,
    saving: computed(() => status.value === 'saving'),
    saved: computed(() => status.value === 'saved'),
    run,
    reset,
    markSaving,
    markSaved,
    markError
  }
}
