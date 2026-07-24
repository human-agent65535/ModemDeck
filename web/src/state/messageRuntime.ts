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

const notificationPreferenceKey = 'modemdeck.messageNotifications'
const fallbackRefreshMilliseconds = 30_000

const state = reactive({
  supported: false,
  secureContext: false,
  permission: 'default' as NotificationPermission,
  enabled: false,
  connected: false
})

let closeStream: (() => void) | undefined
let fallbackTimer: number | undefined
let activeRouter: Router | undefined
let initializedStream = false
let eventQueue = Promise.resolve()
let generation = 0

export const messageNotificationState = readonly(state)

export function initializeMessageRuntime(router: Router): void {
  if (activeRouter || fixtureMode) return
  activeRouter = router
  generation += 1
  const currentGeneration = generation
  syncNotificationState()

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

export async function toggleMessageNotifications(): Promise<void> {
  syncNotificationState()
  if (!state.supported || !state.secureContext) return
  if (state.enabled) {
    writeNotificationPreference(false)
    state.enabled = false
    return
  }

  let permission = Notification.permission
  if (permission === 'default') {
    try {
      permission = await Notification.requestPermission()
    } catch {
      permission = 'denied'
    }
  }
  state.permission = permission
  const enabled = permission === 'granted'
  writeNotificationPreference(enabled)
  state.enabled = enabled
}

export function shouldDisplayBrowserNotification(
  live: boolean,
  enabled: boolean,
  permission: NotificationPermission,
  visibility: DocumentVisibilityState,
  focused: boolean
): boolean {
  return live && enabled && permission === 'granted' && (visibility !== 'visible' || !focused)
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

function syncNotificationState(): void {
  state.secureContext = typeof window !== 'undefined' && window.isSecureContext
  state.supported = typeof Notification !== 'undefined'
  const available = state.secureContext && state.supported
  state.permission = available ? Notification.permission : 'denied'
  state.enabled =
    available &&
    state.permission === 'granted' &&
    readNotificationPreference()
}

function readNotificationPreference(): boolean {
  try {
    return window.localStorage.getItem(notificationPreferenceKey) === 'enabled'
  } catch {
    return false
  }
}

function writeNotificationPreference(enabled: boolean): void {
  try {
    if (enabled) window.localStorage.setItem(notificationPreferenceKey, 'enabled')
    else window.localStorage.removeItem(notificationPreferenceKey)
  } catch {
    // Browser permission remains authoritative when storage is unavailable.
  }
}

function showIncomingMessageNotification(event: IncomingMessageEvent, router: Router): void {
  syncNotificationState()
  if (!shouldDisplayBrowserNotification(
    true,
    state.enabled,
    state.permission,
    document.visibilityState,
    document.hasFocus()
  )) return

  const contact = contactForNumber(event.peer)
  const line = lineForKey(event.line_id) || lineForKey(event.iccid)
  const sender = contact?.display_name || event.peer
  const title = line ? `${sender} · ${lineLabel(line)}` : sender
  let notification: Notification
  try {
    notification = new Notification(title, {
      body: event.content || '收到新短信',
      tag: `modemdeck-sms-${event.message_id}`
    })
  } catch {
    syncNotificationState()
    return
  }
  notification.onclick = () => {
    window.focus()
    void router.push(incomingMessageRoute(event))
    notification.close()
  }
}
