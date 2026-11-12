import type { LineSummary, MessageThread } from '../../api/types'
import {
  messageThreadRoute,
  type MessageRouteLocation
} from '../../router/messageRoute'
import { normalizedPhoneIdentity } from '../../utils/lineIdentity'

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
  const normalizedRecipient = normalizedPhoneIdentity(recipient)
  if (!normalizedRecipient || !line) return undefined

  return threads.find(
    thread =>
      normalizedPhoneIdentity(thread.peer) === normalizedRecipient &&
      messageThreadUsesLine(thread, line)
  )
}

export function messageReturnRoute(threadKey: string): MessageRouteLocation {
  return messageThreadRoute(threadKey)
}
