import { reactive, readonly } from 'vue'

export type ConfirmationTone = 'default' | 'danger'

export type ConfirmationOptions = {
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: ConfirmationTone
}

type ConfirmationRequest = Required<
  Pick<ConfirmationOptions, 'title' | 'confirmLabel' | 'cancelLabel' | 'tone'>
> &
  Pick<ConfirmationOptions, 'message'> & {
    id: number
  }

const state = reactive<{ request: ConfirmationRequest | null }>({
  request: null
})

let nextID = 1
let resolveRequest: ((confirmed: boolean) => void) | null = null

export const confirmationState = readonly(state)

export function requestConfirmation(options: ConfirmationOptions): Promise<boolean> {
  if (state.request) return Promise.resolve(false)

  state.request = {
    id: nextID,
    title: options.title,
    message: options.message,
    confirmLabel: options.confirmLabel || '确认',
    cancelLabel: options.cancelLabel || '取消',
    tone: options.tone || 'default'
  }
  nextID += 1

  return new Promise(resolve => {
    resolveRequest = resolve
  })
}

export function answerConfirmation(confirmed: boolean): void {
  const resolve = resolveRequest
  if (!state.request || !resolve) return

  state.request = null
  resolveRequest = null
  resolve(confirmed)
}
