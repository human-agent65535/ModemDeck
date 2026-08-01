import type { LineSummary, MessageThread } from '../../api/types'
import {
  messageThreadRoute,
  type MessageRouteLocation
} from '../../router/messageRoute'

function normalizedAddress(value: string): string {
  const trimmed = value.trim()
  const compact = trimmed.replace(/[\s().\-/]/g, '')
  return /^\+[1-9]\d{7,14}$/.test(compact)
    ? compact
    : trimmed.toLocaleLowerCase()
}

export function messageThreadUsesLine(
  thread: MessageThread,
  line: LineSummary
): boolean {
  const stableLineID = line.id.trim()
  return Boolean(stableLineID && thread.line_id.trim() === stableLineID)
}

export function findRecipientThread(
  threads: MessageThread[],
  recipient: string,
  line?: LineSummary
): MessageThread | undefined {
  const normalizedRecipient = normalizedAddress(recipient)
  if (!normalizedRecipient || !line) return undefined

  return threads.find(
    thread =>
      normalizedAddress(thread.peer) === normalizedRecipient &&
      messageThreadUsesLine(thread, line)
  )
}

export function messageReturnRoute(threadKey: string): MessageRouteLocation {
  return messageThreadRoute(threadKey)
}
