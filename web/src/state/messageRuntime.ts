import { reactive, readonly } from 'vue'
import type { Router } from 'vue-router'
import { fixtureMode, gateway } from '../api/client'
import type { IncomingMessageEvent, MessageEventDelivery } from '../api/types'
import { translate } from '../i18n'
import {
  messageThreadKeyFromReference,
  messageThreadRoute
} from '../router/messageRoute'
import {
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineLabel,
  refreshIncomingMessage,
  refreshMessageWorkspace
} from './workspace'
import { showBrowserNotification } from './browserNotifications'
import { playIncomingMessageSound } from './browserSounds'

const fallbackRefreshMilliseconds = 30_000
const incomingMessageAlertWindowMilliseconds = 60_000

const state = reactive({
  connected: false
})

let closeStream: (() => void) | undefined
let fallbackTimer: number | undefined
let activeRouter: Router | undefined
let eventQueue = Promise.resolve()
let generation = 0

export const messageRuntimeState = readonly(state)

export function initializeMessageRuntime(router: Router): void {
  if (activeRouter || fixtureMode) return
  activeRouter = router
  generation += 1
  const currentGeneration = generation
  closeStream = gateway.subscribeMessageEvents({
    onOpen: () => {
      if (currentGeneration !== generation) return
      state.connected = true
    },
    onReady: () => undefined,
    onMessage: (event, delivery) => {
      enqueue(async () => {
        if (currentGeneration !== generation) return
        await refreshIncomingMessage(event, activeThreadKey(router))
        if (shouldAlertIncomingMessage(event, delivery)) {
          playIncomingMessageSound(event.message_id)
          showIncomingMessageNotification(event, router)
        }
      })
    },
    onReset: () => {
      if (currentGeneration !== generation) return
      enqueue(async () => {
        if (currentGeneration !== generation) return
        await refreshMessageWorkspace(activeThreadKey(router))
      })
    },
    onError: () => {
      if (currentGeneration !== generation) return
      const wasConnected = state.connected
      state.connected = false
      if (wasConnected) {
        enqueue(async () => {
          if (currentGeneration !== generation) return
          await refreshMessageWorkspace(activeThreadKey(router))
        })
      }
    }
  })

  fallbackTimer = window.setInterval(() => {
    if (!shouldRunMessageFallback(state.connected)) return
    enqueue(async () => {
      if (currentGeneration !== generation) return
      await refreshMessageWorkspace(activeThreadKey(router))
    })
  }, fallbackRefreshMilliseconds)
}

export function shutdownMessageRuntime(): void {
  generation += 1
  closeStream?.()
  closeStream = undefined
  if (fallbackTimer !== undefined) window.clearInterval(fallbackTimer)
  fallbackTimer = undefined
  activeRouter = undefined
  state.connected = false
}

export function shouldRunMessageFallback(connected: boolean): boolean {
  return !connected
}

export function shouldAlertIncomingMessage(
  event: IncomingMessageEvent,
  delivery: MessageEventDelivery,
  now = Date.now()
): boolean {
  if (delivery !== 'live') return false
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

function activeThreadKey(router: Router): string {
  const route = router.currentRoute.value
  if (route.name !== 'messages') return ''
  return messageThreadKeyFromReference(route.params.threadRef)
}

function enqueue(operation: () => Promise<void>): void {
  eventQueue = eventQueue.then(operation, operation)
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
