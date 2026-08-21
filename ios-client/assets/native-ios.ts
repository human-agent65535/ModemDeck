import { nativeResponseBody } from './nativeResponse'

const nativeIOS = import.meta.env.VITE_MODEMDECK_NATIVE_IOS === '1'

type NativeStatus = {
  configured: boolean
  serverURL?: string
}

export type NativeIOSAppInfo = {
  version: string
  build: string
}

export type NativeIOSNotificationAuthorization =
  | 'notDetermined'
  | 'denied'
  | 'authorized'
  | 'provisional'
  | 'ephemeral'
  | 'unknown'

export type NativeIOSNotificationStatus = {
  authorization: NativeIOSNotificationAuthorization
  enabled: boolean
  canRequest: boolean
}

export type NativeIOSCallState = {
  state: 'idle' | 'ringing' | 'connecting' | 'active'
  callID?: string
  remoteNumber?: string
  displayName?: string
  direction?: 'incoming' | 'outgoing'
  testCall?: boolean
  muted?: boolean
  createdAt?: string
  activeAt?: string
}

type NativeHTTPResponse = {
  status: number
  headers: Record<string, string>
  body: string
  base64Encoded: boolean
}

type NativeStreamEvent = {
  streamID: string
  kind: 'open' | 'event' | 'error'
  event?: string
  data?: string
  status?: number
  message?: string
}

type NativeListenerHandle = {
  remove?: () => Promise<void>
}

export type NativeIOSContact = {
  identifier: string
  displayName: string
  phones: Array<{
    label: string
    number: string
    region?: string
  }>
}

type ModemDeckNativePlugin = {
  status(options?: Record<string, never>): Promise<NativeStatus>
  appInfo(options?: Record<string, never>): Promise<NativeIOSAppInfo>
  notificationStatus(
    options?: Record<string, never>
  ): Promise<NativeIOSNotificationStatus>
  requestNotificationPermission(
    options?: Record<string, never>
  ): Promise<NativeIOSNotificationStatus>
  openNotificationSettings(
    options?: Record<string, never>
  ): Promise<{ opened: boolean }>
  showNotification(options: {
    title: string
    body: string
    identifier: string
  }): Promise<{ scheduled: boolean }>
  currentCallState(options?: Record<string, never>): Promise<NativeIOSCallState>
  startOutgoingCall(options: {
    callID: string
    remoteNumber: string
    displayName: string
  }): Promise<{ uuid: string }>
  answerCall(options: { callID: string }): Promise<void>
  endCall(options: { callID: string }): Promise<void>
  playDTMF(options: { callID: string; digits: string }): Promise<void>
  setCallMuted(options: { muted: boolean }): Promise<{ muted: boolean }>
  scanPairingCode(options?: Record<string, never>): Promise<{
    value?: string
    cancelled: boolean
  }>
  readContacts(options?: Record<string, never>): Promise<{
    contacts: NativeIOSContact[]
  }>
  configure(options: { serverURL: string; token: string }): Promise<{ serverURL: string }>
  clearConfiguration(options?: Record<string, never>): Promise<void>
  disconnect(options?: Record<string, never>): Promise<{ revoked: boolean }>
  request(options: {
    requestID: string
    path: string
    method: string
    headers: Record<string, string>
    body?: string
    timeoutMilliseconds: number
  }): Promise<NativeHTTPResponse>
  cancelRequest(options: { requestID: string }): Promise<void>
  startEventStream(options: { streamID: string; path: string }): Promise<void>
  stopEventStream(options: { streamID: string }): Promise<void>
  addListener(
    eventName: 'streamEvent',
    listener: (event: NativeStreamEvent) => void
  ): Promise<NativeListenerHandle> | NativeListenerHandle
  addListener(
    eventName: 'callState',
    listener: (event: NativeIOSCallState) => void
  ): Promise<NativeListenerHandle> | NativeListenerHandle
}

type CapacitorWindow = Window & {
  Capacitor?: {
    Plugins?: {
      ModemDeckNative?: ModemDeckNativePlugin
    }
  }
}

function nativePlugin(): ModemDeckNativePlugin {
  const plugin = (window as CapacitorWindow).Capacitor?.Plugins?.ModemDeckNative
  if (!plugin) throw new Error('ModemDeck iOS bridge is unavailable')
  return plugin
}

export async function readNativeIOSAppInfo(): Promise<NativeIOSAppInfo> {
  if (!nativeIOS) return { version: '', build: '' }
  return nativePlugin().appInfo()
}

export async function readNativeIOSNotificationStatus(): Promise<NativeIOSNotificationStatus> {
  if (!nativeIOS) {
    return { authorization: 'unknown', enabled: false, canRequest: false }
  }
  return nativePlugin().notificationStatus()
}

export async function requestNativeIOSNotificationPermission(): Promise<NativeIOSNotificationStatus> {
  if (!nativeIOS) {
    return { authorization: 'unknown', enabled: false, canRequest: false }
  }
  return nativePlugin().requestNotificationPermission()
}

export async function openNativeIOSNotificationSettings(): Promise<boolean> {
  if (!nativeIOS) return false
  return (await nativePlugin().openNotificationSettings()).opened
}

export async function showNativeIOSNotification(input: {
  title: string
  body: string
  identifier: string
}): Promise<boolean> {
  if (!nativeIOS) return false
  return (await nativePlugin().showNotification(input)).scheduled
}

export async function readNativeIOSCallState(): Promise<NativeIOSCallState> {
  if (!nativeIOS) return { state: 'idle' }
  return nativePlugin().currentCallState()
}

export async function listenNativeIOSCallState(
  listener: (state: NativeIOSCallState) => void
): Promise<() => void> {
  if (!nativeIOS) return () => undefined
  const handle = await nativePlugin().addListener('callState', listener)
  return () => {
    void handle.remove?.()
  }
}

export async function setNativeIOSCallMuted(muted: boolean): Promise<void> {
  if (!nativeIOS) return
  await nativePlugin().setCallMuted({ muted })
}

export async function startNativeIOSOutgoingCall(input: {
  callID: string
  remoteNumber: string
  displayName: string
}): Promise<void> {
  if (!nativeIOS) return
  await nativePlugin().startOutgoingCall(input)
}

export async function answerNativeIOSCall(callID: string): Promise<void> {
  if (!nativeIOS) return
  await nativePlugin().answerCall({ callID })
}

export async function endNativeIOSCall(callID: string): Promise<void> {
  if (!nativeIOS) return
  await nativePlugin().endCall({ callID })
}

export async function sendNativeIOSCallDTMF(
  callID: string,
  digits: string
): Promise<void> {
  if (!nativeIOS) return
  await nativePlugin().playDTMF({ callID, digits })
}

export async function readNativeIOSContacts(): Promise<NativeIOSContact[]> {
  if (!nativeIOS) return []
  const result = await nativePlugin().readContacts()
  return Array.isArray(result.contacts) ? result.contacts : []
}

function requestID(): string {
  if (typeof globalThis.crypto.randomUUID === 'function') {
    return globalThis.crypto.randomUUID()
  }
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

function pairingPayload(value: string): { serverURL: string; token: string } {
  let parsed: unknown
  try {
    parsed = JSON.parse(value.trim()) as unknown
  } catch {
    throw new Error('配对码不是有效的 JSON')
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('配对码格式不正确')
  }
  const payload = parsed as Record<string, unknown>
  if (
    payload.version !== 1 ||
    payload.type !== 'modemdeck.ios.pairing' ||
    typeof payload.server_url !== 'string' ||
    typeof payload.token !== 'string'
  ) {
    throw new Error('这不是 ModemDeck iOS 配对码')
  }
  return {
    serverURL: payload.server_url,
    token: payload.token
  }
}

function onboardingRoot(): HTMLElement {
  document.getElementById('modemdeck-native-onboarding')?.remove()
  const root = document.createElement('main')
  root.id = 'modemdeck-native-onboarding'
  root.className = 'native-onboarding'
  root.setAttribute('aria-live', 'polite')
  document.body.append(root)
  return root
}

function pairingScreen(): Promise<void> {
  return new Promise(resolve => {
    const root = onboardingRoot()
    root.innerHTML = `
      <section class="native-onboarding__card" aria-labelledby="native-pairing-title">
        <div class="native-onboarding__brand" aria-hidden="true">M</div>
        <div class="native-onboarding__heading">
          <p>ModemDeck for iPhone</p>
          <h1 id="native-pairing-title">连接你的 ModemDeck</h1>
          <span>在 Web 端打开“设置 → 配对”，生成二维码后直接扫描。</span>
        </div>
        <button class="native-onboarding__scan" type="button">
          <svg aria-hidden="true" viewBox="0 0 24 24">
            <path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3M7 12h10" />
          </svg>
          <span>
            <strong>扫描二维码</strong>
            <small>使用相机快速完成配对</small>
          </span>
        </button>
        <button class="native-onboarding__manual-toggle" type="button" aria-expanded="false" aria-controls="native-pairing-manual">
          手动粘贴配对码
        </button>
        <div class="native-onboarding__manual" id="native-pairing-manual" hidden>
          <label class="native-onboarding__field">
            <span>配对码</span>
            <textarea rows="6" spellcheck="false" autocomplete="off" autocapitalize="off" placeholder='{"version":1,"type":"modemdeck.ios.pairing",…}'></textarea>
          </label>
          <div class="native-onboarding__actions">
            <button class="native-onboarding__secondary native-onboarding__paste" type="button">从剪贴板粘贴</button>
            <button class="native-onboarding__primary native-onboarding__connect" type="button">连接</button>
          </div>
        </div>
        <p class="native-onboarding__error" role="alert"></p>
      </section>
    `
    const textarea = root.querySelector('textarea') as HTMLTextAreaElement
    const error = root.querySelector('.native-onboarding__error') as HTMLParagraphElement
    const scanButton = root.querySelector(
      '.native-onboarding__scan'
    ) as HTMLButtonElement
    const scanLabel = scanButton.querySelector('strong') as HTMLElement
    const manualToggle = root.querySelector(
      '.native-onboarding__manual-toggle'
    ) as HTMLButtonElement
    const manualPanel = root.querySelector(
      '.native-onboarding__manual'
    ) as HTMLDivElement
    const pasteButton = root.querySelector(
      '.native-onboarding__paste'
    ) as HTMLButtonElement
    const connectButton = root.querySelector(
      '.native-onboarding__connect'
    ) as HTMLButtonElement

    const setBusy = (busy: boolean) => {
      scanButton.disabled = busy
      manualToggle.disabled = busy
      pasteButton.disabled = busy
      connectButton.disabled = busy
    }

    const configureCode = async (value: string) => {
      const payload = pairingPayload(value)
      await nativePlugin().configure(payload)
      root.remove()
      resolve()
    }

    const showManualEntry = () => {
      manualPanel.hidden = false
      manualToggle.setAttribute('aria-expanded', 'true')
      manualToggle.textContent = '收起手动配对'
    }

    manualToggle?.addEventListener('click', () => {
      error.textContent = ''
      if (manualPanel.hidden) {
        showManualEntry()
        return
      }
      manualPanel.hidden = true
      manualToggle.setAttribute('aria-expanded', 'false')
      manualToggle.textContent = '手动粘贴配对码'
    })

    scanButton?.addEventListener('click', async () => {
      error.textContent = ''
      setBusy(true)
      scanLabel.textContent = '正在打开相机…'
      try {
        const result = await nativePlugin().scanPairingCode()
        if (result.cancelled || !result.value) return
        scanLabel.textContent = '正在验证…'
        await configureCode(result.value)
      } catch (cause) {
        const code = cause && typeof cause === 'object' && 'code' in cause
          ? String((cause as { code?: unknown }).code || '')
          : ''
        if (code === 'CAMERA_UNAVAILABLE') {
          error.textContent = '此设备没有可用相机，请手动粘贴配对码。'
          showManualEntry()
        } else if (code === 'CAMERA_PERMISSION_DENIED') {
          error.textContent = '相机权限未开启，请在系统设置中允许相机访问，或手动粘贴配对码。'
          showManualEntry()
        } else {
          error.textContent = cause instanceof Error ? cause.message : '无法扫描配对二维码'
        }
      } finally {
        if (root.isConnected) {
          setBusy(false)
          scanLabel.textContent = '扫描二维码'
        }
      }
    })

    pasteButton?.addEventListener('click', async () => {
      error.textContent = ''
      try {
        textarea.value = await navigator.clipboard.readText()
        textarea.focus()
      } catch {
        error.textContent = '无法读取剪贴板，请长按输入框后粘贴。'
        textarea.focus()
      }
    })

    connectButton?.addEventListener('click', async () => {
      error.textContent = ''
      setBusy(true)
      connectButton.textContent = '正在验证…'
      try {
        await configureCode(textarea.value)
      } catch (cause) {
        error.textContent = cause instanceof Error ? cause.message : '无法完成配对'
      } finally {
        if (root.isConnected) {
          setBusy(false)
          connectButton.textContent = '连接'
        }
      }
    })
  })
}

function recoveryScreen(serverURL: string, detail: string): Promise<'retry' | 'forget'> {
  return new Promise(resolve => {
    const root = onboardingRoot()
    root.innerHTML = `
      <section class="native-onboarding__card" aria-labelledby="native-recovery-title">
        <div class="native-onboarding__brand" aria-hidden="true">M</div>
        <div class="native-onboarding__heading">
          <p>ModemDeck for iPhone</p>
          <h1 id="native-recovery-title">暂时无法连接</h1>
          <span>配对仍保留在此 iPhone 上。检查网络或服务器状态后重试。</span>
        </div>
        <dl class="native-onboarding__facts">
          <div><dt>服务器</dt><dd></dd></div>
        </dl>
        <p class="native-onboarding__error" role="alert"></p>
        <div class="native-onboarding__actions">
          <button class="native-onboarding__secondary" type="button">移除此配对</button>
          <button class="native-onboarding__primary" type="button">重试</button>
        </div>
      </section>
    `
    const server = root.querySelector('dd') as HTMLElement
    const error = root.querySelector('.native-onboarding__error') as HTMLElement
    const forgetButton = root.querySelector(
      '.native-onboarding__secondary'
    ) as HTMLButtonElement
    const retryButton = root.querySelector(
      '.native-onboarding__primary'
    ) as HTMLButtonElement
    server.textContent = serverURL
    error.textContent = detail
    forgetButton?.addEventListener('click', async () => {
      forgetButton.disabled = true
      retryButton.disabled = true
      await nativePlugin().clearConfiguration().catch(() => undefined)
      root.remove()
      resolve('forget')
    })
    retryButton?.addEventListener('click', () => {
      root.remove()
      resolve('retry')
    })
  })
}

async function probeConfiguration(): Promise<NativeHTTPResponse> {
  return nativePlugin().request({
    requestID: requestID(),
    path: '/api/v1/mobile/session',
    method: 'GET',
    headers: { Accept: 'application/json' },
    timeoutMilliseconds: 15_000
  })
}

export async function ensureNativeIOSConfiguration(): Promise<void> {
  if (!nativeIOS) return
  for (;;) {
    const status = await nativePlugin().status()
    if (!status.configured) {
      await pairingScreen()
      continue
    }
    try {
      const response = await probeConfiguration()
      if (response.status === 200) return
      if (response.status === 401) {
        await nativePlugin().clearConfiguration()
        continue
      }
      const choice = await recoveryScreen(
        status.serverURL || '',
        `服务器返回 HTTP ${response.status}`
      )
      if (choice === 'forget') continue
    } catch (cause) {
      const choice = await recoveryScreen(
        status.serverURL || '',
        cause instanceof Error ? cause.message : '无法连接 ModemDeck'
      )
      if (choice === 'forget') continue
    }
  }
}

let unauthorizedResetInProgress = false

function resetAfterUnauthorized(): void {
  if (unauthorizedResetInProgress) return
  unauthorizedResetInProgress = true
  void nativePlugin()
    .clearConfiguration()
    .catch(() => undefined)
    .finally(() => window.location.reload())
}

function decodeBase64(value: string): ArrayBuffer {
  const decoded = atob(value)
  const bytes = new Uint8Array(decoded.length)
  for (let index = 0; index < decoded.length; index += 1) {
    bytes[index] = decoded.charCodeAt(index)
  }
  return bytes.buffer as ArrayBuffer
}

export async function nativeIOSFetch(
  path: string,
  init: RequestInit,
  timeoutMilliseconds: number
): Promise<Response> {
  if (!nativeIOS) return fetch(path, init)
  const method = (init.method || 'GET').toUpperCase()
  if (path === '/api/v1/session' && method === 'DELETE') {
    await nativePlugin().disconnect()
    globalThis.setTimeout(() => window.location.reload(), 0)
    return new Response(null, { status: 204 })
  }
  const requestPath = path === '/api/v1/session' && method === 'GET'
    ? '/api/v1/mobile/session'
    : path
  if (init.body !== undefined && init.body !== null && typeof init.body !== 'string') {
    throw new Error('ModemDeck iOS transport requires a text request body')
  }
  const headers: Record<string, string> = {}
  new Headers(init.headers).forEach((value, name) => {
    headers[name] = value
  })
  const id = requestID()
  const signal = init.signal
  if (signal?.aborted) throw new DOMException('The request was aborted', 'AbortError')

  const response = await new Promise<NativeHTTPResponse>((resolve, reject) => {
    let settled = false
    const finish = (callback: () => void) => {
      if (settled) return
      settled = true
      signal?.removeEventListener('abort', abort)
      callback()
    }
    const abort = () => {
      void nativePlugin().cancelRequest({ requestID: id })
      finish(() => reject(new DOMException('The request was aborted', 'AbortError')))
    }
    signal?.addEventListener('abort', abort, { once: true })
    void nativePlugin()
      .request({
        requestID: id,
        path: requestPath,
        method,
        headers,
        ...(typeof init.body === 'string' ? { body: init.body } : {}),
        timeoutMilliseconds
      })
      .then(value => finish(() => resolve(value)))
      .catch(cause => finish(() => reject(cause)))
  })
  if (response.status === 401) resetAfterUnauthorized()
  const decodedBody = response.base64Encoded
    ? decodeBase64(response.body)
    : response.body
  return new Response(nativeResponseBody(response.status, method, decodedBody), {
    status: response.status,
    headers: response.headers
  })
}

export function nativeIOSAuthenticatedResourceURL(path: string): string {
  if (!nativeIOS) return path
  const base = new URL('https://modemdeck.invalid')
  const parsed = new URL(path, base)
  if (parsed.origin !== base.origin || !parsed.pathname.startsWith('/api/v1/')) {
    throw new Error('Authenticated ModemDeck resource path is invalid')
  }
  return `modemdeck-api://localhost${parsed.pathname}${parsed.search}`
}

type NativeEventSourceTarget = {
  onNativeEvent(event: NativeStreamEvent): void
}

const nativeStreams = new Map<string, NativeEventSourceTarget>()
let streamListenerInstalled = false

function ensureStreamListener(): void {
  if (streamListenerInstalled) return
  streamListenerInstalled = true
  void Promise.resolve(
    nativePlugin().addListener('streamEvent', event => {
      nativeStreams.get(event.streamID)?.onNativeEvent(event)
    })
  ).catch(() => {
    streamListenerInstalled = false
  })
}

class NativeEventSource implements NativeEventSourceTarget {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null

  private readonly streamID = requestID()
  private readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  private stopped = false
  private reconnectTimer: ReturnType<typeof globalThis.setTimeout> | undefined

  constructor(private readonly path: string) {
    ensureStreamListener()
    nativeStreams.set(this.streamID, this)
    this.connect()
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject): void {
    let listeners = this.listeners.get(type)
    if (!listeners) {
      listeners = new Set()
      this.listeners.set(type, listeners)
    }
    const callback = typeof listener === 'function'
      ? (listener as (event: MessageEvent<string>) => void)
      : (event: MessageEvent<string>) => listener.handleEvent(event)
    listeners.add(callback)
  }

  close(): void {
    if (this.stopped) return
    this.stopped = true
    if (this.reconnectTimer !== undefined) {
      globalThis.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = undefined
    }
    nativeStreams.delete(this.streamID)
    void nativePlugin().stopEventStream({ streamID: this.streamID })
  }

  onNativeEvent(event: NativeStreamEvent): void {
    if (this.stopped) return
    if (event.kind === 'open') {
      this.onopen?.(new Event('open'))
      return
    }
    if (event.kind === 'event') {
      const message = new MessageEvent(event.event || 'message', {
        data: event.data || ''
      })
      for (const listener of this.listeners.get(event.event || 'message') || []) {
        listener(message)
      }
      return
    }
    this.onerror?.(new Event('error'))
    if (event.status === 401) {
      resetAfterUnauthorized()
      return
    }
    this.scheduleReconnect()
  }

  private connect(): void {
    if (this.stopped) return
    void nativePlugin()
      .startEventStream({ streamID: this.streamID, path: this.path })
      .catch(() => {
        if (this.stopped) return
        this.onerror?.(new Event('error'))
        this.scheduleReconnect()
      })
  }

  private scheduleReconnect(): void {
    if (this.stopped || this.reconnectTimer !== undefined) return
    this.reconnectTimer = globalThis.setTimeout(() => {
      this.reconnectTimer = undefined
      this.connect()
    }, 2_000)
  }
}

export function createModemDeckEventSource(path: string): EventSource {
  if (!nativeIOS) return new EventSource(path, { withCredentials: true })
  return new NativeEventSource(path) as unknown as EventSource
}

export const nativeIOSMode = nativeIOS
