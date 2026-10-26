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
type FeedbackTimer = {
  handle?: ReturnType<typeof globalThis.setTimeout>
  remaining: number
  startedAt: number
}
const feedbackTimers = new Map<number, FeedbackTimer>()

function startTimer(id: number, timer: FeedbackTimer): void {
  timer.startedAt = Date.now()
  timer.handle = globalThis.setTimeout(() => dismissFeedback(id), timer.remaining)
}

export function dismissFeedback(id: number): void {
  const timer = feedbackTimers.get(id)
  if (timer?.handle) globalThis.clearTimeout(timer.handle)
  feedbackTimers.delete(id)
  feedbackState.items = feedbackState.items.filter(item => item.id !== id)
}

/** Hover or focus holds every toast so it can be read (WCAG 2.2.1); leaving resumes the countdown. */
export function pauseFeedback(): void {
  for (const timer of feedbackTimers.values()) {
    if (!timer.handle) continue
    globalThis.clearTimeout(timer.handle)
    timer.handle = undefined
    timer.remaining = Math.max(1200, timer.remaining - (Date.now() - timer.startedAt))
  }
}

export function resumeFeedback(): void {
  for (const [id, timer] of feedbackTimers) {
    if (!timer.handle) startTimer(id, timer)
  }
}

// Errors stay until dismissed: they usually need reading and acting on.
export function showFeedback(
  message: string,
  tone: FeedbackTone = 'success',
  duration = tone === 'error' ? 0 : 4200
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
  if (duration > 0) {
    const timer: FeedbackTimer = { remaining: Math.max(1200, duration), startedAt: 0 }
    feedbackTimers.set(id, timer)
    startTimer(id, timer)
  }
  return id
}

export function showSuccess(message: string): number {
  return showFeedback(message, 'success')
}

export function showError(message: string): number {
  return showFeedback(message, 'error')
}

export function resetFeedback(): void {
  for (const timer of feedbackTimers.values()) {
    if (timer.handle) globalThis.clearTimeout(timer.handle)
  }
  feedbackTimers.clear()
  feedbackState.items = []
}
