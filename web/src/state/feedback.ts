import { reactive } from 'vue'

export type FeedbackTone = 'success' | 'error' | 'info'

export type FeedbackItem = {
  id: number
  message: string
  tone: FeedbackTone
}

export const feedbackState = reactive<{ items: FeedbackItem[] }>({
  items: []
})

let nextFeedbackID = 1
const feedbackTimers = new Map<number, ReturnType<typeof globalThis.setTimeout>>()

export function dismissFeedback(id: number): void {
  const timer = feedbackTimers.get(id)
  if (timer) globalThis.clearTimeout(timer)
  feedbackTimers.delete(id)
  feedbackState.items = feedbackState.items.filter(item => item.id !== id)
}

export function showFeedback(
  message: string,
  tone: FeedbackTone = 'success',
  duration = tone === 'error' ? 5200 : 4200
): number {
  const normalized = message.trim()
  if (!normalized) return 0

  const duplicate = feedbackState.items.find(
    item => item.message === normalized && item.tone === tone
  )
  if (duplicate) dismissFeedback(duplicate.id)

  while (feedbackState.items.length >= 3) {
    const oldest = feedbackState.items[0]
    if (!oldest) break
    dismissFeedback(oldest.id)
  }

  const id = nextFeedbackID++
  feedbackState.items.push({ id, message: normalized, tone })
  feedbackTimers.set(
    id,
    globalThis.setTimeout(() => dismissFeedback(id), Math.max(1200, duration))
  )
  return id
}

export function showSuccess(message: string): number {
  return showFeedback(message, 'success')
}

export function showError(message: string): number {
  return showFeedback(message, 'error')
}

export function resetFeedback(): void {
  for (const timer of feedbackTimers.values()) globalThis.clearTimeout(timer)
  feedbackTimers.clear()
  feedbackState.items = []
}
