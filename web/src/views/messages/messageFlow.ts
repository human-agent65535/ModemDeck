import type { LineSummary, MessageThread } from '../../api/types'
import { normalizedPhoneIdentity } from '../../utils/lineIdentity'

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
  const threadPhone = normalizedPhoneIdentity(thread.local_phone)
  const linePhone = normalizedPhoneIdentity(line.phone_number)
  if (threadPhone && linePhone) return threadPhone === linePhone
  if (thread.imsi && line.imsi) return thread.imsi === line.imsi
  return Boolean(thread.iccid && line.iccid === thread.iccid)
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
