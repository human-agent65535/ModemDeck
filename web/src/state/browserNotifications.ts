import { reactive, readonly } from 'vue'

const notificationPreferenceKey = 'modemdeck.browserNotifications'

const state = reactive({
  supported: false,
  secureContext: false,
  permission: 'default' as NotificationPermission,
  preferenceEnabled: false,
  active: false,
  requesting: false,
  error: ''
})

let initialized = false
let volatilePreference: boolean | undefined

export const browserNotificationState = readonly(state)

export function browserNotificationsActive(
  preferenceEnabled: boolean,
  supported: boolean,
  secureContext: boolean,
  permission: NotificationPermission
): boolean {
  return preferenceEnabled && supported && secureContext && permission === 'granted'
}

export function shouldDisplayBrowserNotification(
  live: boolean,
  active: boolean,
  visibility: DocumentVisibilityState,
  focused: boolean
): boolean {
  return live && active && (visibility !== 'visible' || !focused)
}

export function initializeBrowserNotifications(): void {
  if (initialized || typeof window === 'undefined') return
  initialized = true
  syncBrowserNotificationState()
  window.addEventListener('focus', syncBrowserNotificationState)
  window.addEventListener('storage', onPreferenceStorage)
  document.addEventListener('visibilitychange', syncBrowserNotificationState)
}

export function shutdownBrowserNotifications(): void {
  if (!initialized || typeof window === 'undefined') return
  initialized = false
  window.removeEventListener('focus', syncBrowserNotificationState)
  window.removeEventListener('storage', onPreferenceStorage)
  document.removeEventListener('visibilitychange', syncBrowserNotificationState)
}

export function syncBrowserNotificationState(): void {
  state.secureContext = typeof window !== 'undefined' && window.isSecureContext
  state.supported = typeof Notification !== 'undefined'
  state.permission =
    state.secureContext && state.supported ? Notification.permission : 'default'
  state.preferenceEnabled = readNotificationPreference()
  state.active = browserNotificationsActive(
    state.preferenceEnabled,
    state.supported,
    state.secureContext,
    state.permission
  )
  if (state.permission === 'granted') state.error = ''
}

export async function toggleBrowserNotifications(): Promise<void> {
  syncBrowserNotificationState()

  if (state.preferenceEnabled) {
    writeNotificationPreference(false)
    state.error = ''
    syncBrowserNotificationState()
    return
  }
  if (!state.secureContext) {
    state.error = '浏览器通知需要 HTTPS'
    return
  }
  if (!state.supported) {
    state.error = '当前浏览器不支持通知'
    return
  }
  if (state.permission === 'denied') {
    state.error = '通知已被浏览器阻止，请在浏览器设置中允许'
    return
  }

  state.requesting = true
  state.error = ''
  try {
    const permission =
      Notification.permission === 'default'
        ? await Notification.requestPermission()
        : Notification.permission
    state.permission = permission
    if (permission === 'granted') {
      writeNotificationPreference(true)
    } else {
      writeNotificationPreference(false)
      state.error = '未获得浏览器通知权限'
    }
  } catch (error) {
    state.error = error instanceof Error ? error.message : '无法请求浏览器通知权限'
  } finally {
    state.requesting = false
    syncBrowserNotificationState()
  }
}

export function showBrowserNotification(input: {
  title: string
  body: string
  tag: string
  onClick: () => void
}): boolean {
  syncBrowserNotificationState()
  if (
    !shouldDisplayBrowserNotification(
      true,
      state.active,
      document.visibilityState,
      document.hasFocus()
    )
  ) {
    return false
  }

  let notification: Notification
  try {
    notification = new Notification(input.title, {
      body: input.body,
      tag: input.tag
    })
  } catch {
    syncBrowserNotificationState()
    return false
  }
  notification.onclick = () => {
    input.onClick()
    notification.close()
  }
  return true
}

function readNotificationPreference(): boolean {
  if (volatilePreference !== undefined) return volatilePreference
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
    volatilePreference = undefined
  } catch {
    volatilePreference = enabled
  }
}

function onPreferenceStorage(event: StorageEvent): void {
  if (event.key !== null && event.key !== notificationPreferenceKey) return
  volatilePreference = undefined
  syncBrowserNotificationState()
}
