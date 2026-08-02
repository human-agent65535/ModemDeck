import { computed } from 'vue'
import type { CallRecord, MessageThread, Resource } from '../api/types'
import { messageThreadReference } from '../router/messageRoute'
import { useListArrivals } from './useListArrivals'

export type CommunicationActivity =
  | {
      key: string
      kind: 'call'
      timestamp: string
      call: CallRecord
    }
  | {
      key: string
      kind: 'message'
      timestamp: string
      thread: MessageThread
    }

type CommunicationActivitySources = {
  calls: Pick<Resource<CallRecord[]>, 'data' | 'status'>
  threads: Pick<Resource<MessageThread[]>, 'data' | 'status'>
  recentIncomingThreadKeys: Readonly<Record<string, boolean>>
  limit?: number
}

export function communicationActivities(
  calls: readonly CallRecord[],
  threads: readonly MessageThread[],
  limit = 30
): CommunicationActivity[] {
  const callActivities: CommunicationActivity[] = calls.map(call => ({
    key: `call:${call.id}`,
    kind: 'call',
    timestamp: call.started_at,
    call
  }))
  const messageActivities: CommunicationActivity[] = threads.map(thread => ({
    key: `message:${messageThreadReference(thread.key)}`,
    kind: 'message',
    timestamp: thread.last_timestamp,
    thread
  }))

  return callActivities
    .concat(messageActivities)
    .sort((left, right) => Date.parse(right.timestamp) - Date.parse(left.timestamp))
    .slice(0, Math.max(0, Math.floor(limit)))
}

export function useCommunicationActivity(sources: CommunicationActivitySources) {
  const activities = computed(() =>
    communicationActivities(sources.calls.data, sources.threads.data, sources.limit)
  )
  const ready = computed(
    () => sources.calls.status === 'ready' && sources.threads.status === 'ready'
  )
  const arrivals = useListArrivals(
    () => ({ items: activities.value, ready: ready.value }),
    activity => activity.key
  )

  return {
    activities,
    ready,
    isArriving(activity: CommunicationActivity): boolean {
      return (
        arrivals.isArriving(activity.key) ||
        (activity.kind === 'message' &&
          Boolean(sources.recentIncomingThreadKeys[activity.thread.key]))
      )
    }
  }
}
