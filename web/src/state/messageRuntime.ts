import { reactive, readonly } from 'vue'
import type { Router } from 'vue-router'
import { fixtureMode, gateway } from '../api/client'
import type { IncomingMessageEvent } from '../api/types'
import {
  contactForNumber,
  lineForKey,
  lineLabel,
  refreshIncomingMessage,
  refreshMessageWorkspace
} from './workspace'
import { showBrowserNotification } from './browserNotifications'

const fallbackRefreshMilliseconds = 30_000

const state = reactive({
  connected: false
})

let closeStream: (() => void) | undefined
let fallbackTimer: number | undefined
let activeRouter: Router | undefined
let initializedStream = false
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
      if (currentGeneration === generation) state.connected = true
    },
    onReady: () => {
      if (currentGeneration === generation) initializedStream = true
    },
    onMessage: event => {
      const allowNotification = initializedStream
      enqueue(async () => {
        if (currentGeneration !== generation) return
        await refreshIncomingMessage(event, activeThreadKey(router))
        if (allowNotification) showIncomingMessageNotification(event, router)
      })
    },
    onReset: () => {
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
  initializedStream = false
  state.connected = false
}

export function shouldRunMessageFallback(connected: boolean): boolean {
  return !connected
}

export function incomingMessageRoute(event: IncomingMessageEvent): {
  name: 'messages'
  params: { threadKey: string }
} {
  return { name: 'messages', params: { threadKey: event.thread_key } }
}

function activeThreadKey(router: Router): string {
  const route = router.currentRoute.value
  if (route.name !== 'messages') return ''
  return typeof route.params.threadKey === 'string' ? route.params.threadKey : ''
}

function enqueue(operation: () => Promise<void>): void {
  eventQueue = eventQueue.then(operation, operation)
}

function showIncomingMessageNotification(event: IncomingMessageEvent, router: Router): void {
  const contact = contactForNumber(event.peer)
  const line = lineForKey(event.line_id) || lineForKey(event.iccid)
  const sender = contact?.display_name || event.peer
  const title = line ? `${sender} · ${lineLabel(line)}` : sender
  showBrowserNotification({
    title,
    body: event.content || '收到新短信',
    tag: `modemdeck-sms-${event.message_id}`,
    onClick: () => {
      window.focus()
      void router.push(incomingMessageRoute(event))
    }
  })
}
