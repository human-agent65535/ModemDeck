const messageViewportBottomTolerance = 32

export type MessageViewportMetrics = {
  scrollHeight: number
  scrollTop: number
  clientHeight: number
}

export type MessageReadEligibility = {
  selected: boolean
  messagesReady: boolean
  unread: boolean
  composing: boolean
  manuallyUnread: boolean
  documentVisible: boolean
  windowFocused: boolean
  atBottom: boolean
}

export function messageViewportIsAtBottom(
  viewport: MessageViewportMetrics,
  tolerance = messageViewportBottomTolerance
): boolean {
  return (
    viewport.scrollHeight -
      viewport.scrollTop -
      viewport.clientHeight <=
    tolerance
  )
}

export function canAcknowledgeMessageThread(
  state: MessageReadEligibility
): boolean {
  return (
    state.selected &&
    state.messagesReady &&
    state.unread &&
    !state.composing &&
    !state.manuallyUnread &&
    state.documentVisible &&
    state.windowFocused &&
    state.atBottom
  )
}
