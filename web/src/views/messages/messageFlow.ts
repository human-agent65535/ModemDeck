import type { LineSummary, MessageThread } from '../../api/types'

export type MessageRouteLocation = {
  name: 'messages'
  params?: {
    threadKey: string
  }
}

function normalizedAddress(value: string): string {
  const trimmed = value.trim()
  const digits = trimmed.replace(/\D/g, '')
  return digits || trimmed.toLocaleLowerCase()
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
  return threadKey
    ? { name: 'messages', params: { threadKey } }
    : { name: 'messages' }
}
