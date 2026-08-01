import type { Router } from 'vue-router'
import { fixtureMode, gateway } from '../api/client'
import type { IncomingMessageEvent } from '../api/types'
import { translate } from '../i18n'
import { messageThreadRoute } from '../router/messageRoute'
import {
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineLabel,
  noteIncomingMessageArrival
} from './workspace'
import { showBrowserNotification } from './browserNotifications'
import { playIncomingMessageSound } from './browserSounds'

const incomingMessageAlertWindowMilliseconds = 60_000

let closeStream: (() => void) | undefined
let activeRouter: Router | undefined

export function initializeMessageRuntime(router: Router): void {
  if (activeRouter || fixtureMode) return
  activeRouter = router
  closeStream = gateway.subscribeMessageEvents({
    onMessage: event => {
      if (activeRouter !== router || !shouldAlertIncomingMessage(event)) return
      noteIncomingMessageArrival(event)
      playIncomingMessageSound(event.message_id)
      showIncomingMessageNotification(event, router)
    }
  })
}

export function shutdownMessageRuntime(): void {
  closeStream?.()
  closeStream = undefined
  activeRouter = undefined
}

export function shouldAlertIncomingMessage(
  event: IncomingMessageEvent,
  now = Date.now()
): boolean {
  const observedAt = Date.parse(event.observed_at)
  return (
    Number.isFinite(observedAt) &&
    Math.abs(now - observedAt) <= incomingMessageAlertWindowMilliseconds
  )
}

export function incomingMessageRoute(event: IncomingMessageEvent): {
  name: 'messages'
  params: { threadRef: string }
} {
  return messageThreadRoute(event.thread_key) as {
    name: 'messages'
    params: { threadRef: string }
  }
}

function showIncomingMessageNotification(event: IncomingMessageEvent, router: Router): void {
  const contact = contactForNumber(event.peer)
  const line = lineForKey(event.line_id)
  const sender =
    contact?.display_name ||
    displayPhoneNumber(event.peer, event.line_id)
  const title = line ? `${sender} · ${lineLabel(line)}` : sender
  showBrowserNotification({
    title,
    body: event.content || translate('runtime.newMessage'),
    tag: `modemdeck-sms-${event.message_id}`,
    onClick: () => {
      window.focus()
      void router.push(incomingMessageRoute(event))
    }
  })
}
