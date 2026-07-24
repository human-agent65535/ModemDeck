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
  const lineIdentifiers = [
    line.id,
    line.iccid,
    line.imsi,
    line.device_imei
  ].filter(Boolean)

  return Boolean(
    (thread.line_id && lineIdentifiers.includes(thread.line_id)) ||
      (thread.iccid && line.iccid === thread.iccid)
  )
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
