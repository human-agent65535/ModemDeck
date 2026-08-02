import { translate } from '../i18n'
import { showFeedback } from './feedback'
import {
  callsResource,
  refreshCalls,
  refreshThreads,
  threadsResource
} from './workspace'

type VisibilityTarget = Pick<
  Document,
  'visibilityState' | 'addEventListener' | 'removeEventListener'
>

export type CommunicationAttentionSnapshot = {
  unreadMessages?: number
  missedCalls?: number
}

type ForegroundCommunicationOptions = {
  documentTarget?: VisibilityTarget
  refresh?: () => Promise<unknown>
  snapshot?: () => CommunicationAttentionSnapshot
  notify?: (message: string) => void
}

export function currentCommunicationAttention(): CommunicationAttentionSnapshot {
  return {
    unreadMessages:
      threadsResource.status === 'ready'
        ? threadsResource.data.reduce(
            (total, thread) => total + Math.max(0, thread.unread_count),
            0
          )
        : undefined,
    missedCalls:
      callsResource.status === 'ready'
        ? callsResource.data.filter(call => call.missed && !call.read).length
        : undefined
  }
}

export function communicationAttentionChanges(
  before: CommunicationAttentionSnapshot,
  after: CommunicationAttentionSnapshot
): Required<CommunicationAttentionSnapshot> {
  return {
    unreadMessages:
      before.unreadMessages === undefined || after.unreadMessages === undefined
        ? 0
        : Math.max(0, after.unreadMessages - before.unreadMessages),
    missedCalls:
      before.missedCalls === undefined || after.missedCalls === undefined
        ? 0
        : Math.max(0, after.missedCalls - before.missedCalls)
  }
}

export function communicationAttentionMessage(
  changes: Required<CommunicationAttentionSnapshot>
): string {
  const parts: string[] = []
  if (changes.unreadMessages > 0) {
    parts.push(`${translate('dashboard.unreadMessages')}: ${changes.unreadMessages}`)
  }
  if (changes.missedCalls > 0) {
    parts.push(`${translate('dashboard.missedCalls')}: ${changes.missedCalls}`)
  }
  return parts.join(' · ')
}

export function initializeForegroundCommunicationRefresh(
  options: ForegroundCommunicationOptions = {}
): () => void {
  const documentTarget =
    options.documentTarget ||
    (typeof document === 'undefined' ? undefined : document)
  if (!documentTarget) return () => undefined

  const snapshot = options.snapshot || currentCommunicationAttention
  const refresh =
    options.refresh ||
    (() => Promise.all([refreshThreads(), refreshCalls()]))
  const notify =
    options.notify ||
    (message => {
      showFeedback(message, 'info', 6_000)
    })
  let hidden = documentTarget.visibilityState !== 'visible'
  let hiddenSnapshot = hidden ? snapshot() : undefined
  let resumeOperation: Promise<void> | undefined
  let stopped = false

  const resume = (
    before: CommunicationAttentionSnapshot,
    observedOnResume: CommunicationAttentionSnapshot
  ) => {
    if (resumeOperation) return
    resumeOperation = Promise.resolve(refresh())
      .then(() => {
        if (stopped) return
        const afterRefresh = snapshot()
        const observed = {
          unreadMessages: Math.max(
            observedOnResume.unreadMessages || 0,
            afterRefresh.unreadMessages || 0
          ),
          missedCalls: Math.max(
            observedOnResume.missedCalls || 0,
            afterRefresh.missedCalls || 0
          )
        }
        const message = communicationAttentionMessage(
          communicationAttentionChanges(before, observed)
        )
        if (message) notify(message)
      })
      .finally(() => {
        resumeOperation = undefined
      })
  }

  const visibilityChanged = () => {
    if (stopped) return
    if (documentTarget.visibilityState !== 'visible') {
      if (!hidden) hiddenSnapshot = snapshot()
      hidden = true
      return
    }
    if (!hidden) return
    hidden = false
    const before = hiddenSnapshot || {}
    hiddenSnapshot = undefined
    resume(before, snapshot())
  }

  documentTarget.addEventListener('visibilitychange', visibilityChanged)
  return () => {
    stopped = true
    documentTarget.removeEventListener('visibilitychange', visibilityChanged)
    hiddenSnapshot = undefined
  }
}
