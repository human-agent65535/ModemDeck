import { reactive, readonly } from 'vue'

const versionEndpoint = '/modemdeck-build.json'
const updateReadyEvent = 'modemdeck:update-ready'
const clientVersion =
  typeof import.meta.env?.VITE_MODEMDECK_BUILD_ID === 'string'
    ? import.meta.env.VITE_MODEMDECK_BUILD_ID.trim()
    : ''

type RecoveryWindow = Pick<
  Window,
  'addEventListener' | 'removeEventListener'
>
type RecoveryDocument = Pick<
  Document,
  'addEventListener' | 'removeEventListener' | 'visibilityState'
>
type ServerVersionReader = () => Promise<string>

const state = reactive({
  available: false,
  currentVersion: '',
  serverVersion: ''
})

let requestInstalledVersionCheck: (() => void) | undefined
let updateNoticeSuppressed = false
let pendingUpdate:
  | { currentVersion: string; serverVersion: string }
  | undefined

export const applicationUpdateState = readonly(state)

export function announceApplicationUpdate(
  currentVersion: string,
  serverVersion: string,
  force = false
): boolean {
  const current = currentVersion.trim() || 'unknown'
  const server = serverVersion.trim()
  if (!server) return false
  if (!force && server === current) {
    pendingUpdate = undefined
    return false
  }

  if (updateNoticeSuppressed) {
    pendingUpdate = { currentVersion: current, serverVersion: server }
    return true
  }

  state.available = true
  state.currentVersion = current
  state.serverVersion = server
  return true
}

export function setApplicationUpdateNoticeSuppressed(
  suppressed: boolean
): void {
  if (suppressed === updateNoticeSuppressed) return
  updateNoticeSuppressed = suppressed
  if (suppressed) {
    if (state.available) {
      pendingUpdate = {
        currentVersion: state.currentVersion,
        serverVersion: state.serverVersion
      }
    }
    state.available = false
    return
  }

  const pending = pendingUpdate
  pendingUpdate = undefined
  if (!pending) return
  state.available = true
  state.currentVersion = pending.currentVersion
  state.serverVersion = pending.serverVersion
}

export function clearApplicationUpdateNotice(): void {
  pendingUpdate = undefined
  state.available = false
  state.currentVersion = ''
  state.serverVersion = ''
}

export function refreshApplication(
  location: Pick<Location, 'reload'> = window.location
): void {
  location.reload()
}

export async function readServerVersion(
  fetcher: typeof fetch = globalThis.fetch
): Promise<string> {
  const response = await fetcher(versionEndpoint, {
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    signal: AbortSignal.timeout(3_000)
  })
  if (!response.ok) {
    throw new Error(`version endpoint returned ${response.status}`)
  }
  const value = (await response.json()) as unknown
  if (!value || typeof value !== 'object') {
    throw new Error('invalid version response')
  }
  const version = (value as { version?: unknown }).version
  if (typeof version !== 'string' || !version.trim()) {
    throw new Error('version response has no application version')
  }
  return version.trim()
}

export async function recoverAfterStaleAsset(
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): Promise<boolean> {
  try {
    return announceApplicationUpdate(
      currentVersion,
      await readVersion(),
      true
    )
  } catch {
    return false
  }
}

export async function checkForApplicationUpdate(
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): Promise<boolean> {
  let serverVersion: string
  try {
    serverVersion = await readVersion()
  } catch {
    return false
  }
  return announceApplicationUpdate(currentVersion, serverVersion)
}

export function installStaleAssetRecovery(
  target: Pick<RecoveryWindow, 'addEventListener'> = window,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): void {
  target.addEventListener('vite:preloadError', async event => {
    event.preventDefault()
    await recoverAfterStaleAsset(currentVersion, readVersion)
  })
  target.addEventListener(updateReadyEvent, event => {
    const detail = (event as CustomEvent<unknown>).detail
    if (!detail || typeof detail !== 'object') return
    const version = (detail as { version?: unknown }).version
    if (typeof version !== 'string') return
    announceApplicationUpdate(currentVersion, version, true)
  })
}

export function initializeApplicationVersionChecks(
  target: RecoveryWindow = window,
  documentTarget: RecoveryDocument = document,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): () => void {
  let stopped = false
  let inFlight: Promise<boolean> | undefined
  const check = () => {
    if (stopped || inFlight) return
    inFlight = checkForApplicationUpdate(currentVersion, readVersion).finally(
      () => {
        inFlight = undefined
      }
    )
  }
  const checkVisible = () => {
    if (documentTarget.visibilityState === 'visible') check()
  }

  target.addEventListener('online', check)
  target.addEventListener('pageshow', check)
  documentTarget.addEventListener('visibilitychange', checkVisible)
  requestInstalledVersionCheck = check
  check()

  return () => {
    if (stopped) return
    stopped = true
    target.removeEventListener('online', check)
    target.removeEventListener('pageshow', check)
    documentTarget.removeEventListener('visibilitychange', checkVisible)
    if (requestInstalledVersionCheck === check) {
      requestInstalledVersionCheck = undefined
    }
  }
}

export function requestApplicationVersionCheck(): void {
  requestInstalledVersionCheck?.()
}
