const staleAssetReloadKey = 'modemdeck:stale-asset-transitions'
const versionEndpoint = '/api/v1/version'
const versionQueryParameter = 'v'
const clientVersion =
  typeof import.meta.env?.VITE_MODEMDECK_BUILD_ID === 'string'
    ? import.meta.env.VITE_MODEMDECK_BUILD_ID.trim()
    : ''

type RecoveryLocation = Pick<Location, 'href' | 'replace'>
type RecoveryStorage = Pick<Storage, 'getItem' | 'setItem'>
type RecoveryWindow = Pick<
  Window,
  'addEventListener' | 'removeEventListener'
> & {
  location: RecoveryLocation
  sessionStorage: RecoveryStorage
}
type RecoveryDocument = Pick<
  Document,
  'addEventListener' | 'removeEventListener' | 'visibilityState'
>
type ServerVersionReader = () => Promise<string>
type EntryURLTarget = {
  location: Pick<Location, 'href'>
  history: Pick<History, 'replaceState' | 'state'>
}

let requestInstalledVersionCheck: (() => void) | undefined

function recordedTransitions(storage: RecoveryStorage): string[] {
  const value = storage.getItem(staleAssetReloadKey)
  if (!value) return []
  const parsed = JSON.parse(value) as unknown
  if (!parsed || typeof parsed !== 'object') return []
  const fields = parsed as { identities?: unknown; identity?: unknown }
  if (Array.isArray(fields.identities)) {
    return fields.identities.filter(
      (identity): identity is string =>
        typeof identity === 'string' && Boolean(identity)
    )
  }
  return typeof fields.identity === 'string' && fields.identity
    ? [fields.identity]
    : []
}

function recordTransition(
  storage: RecoveryStorage,
  identity: string
): 'recorded' | 'seen' | 'unavailable' {
  try {
    const identities = recordedTransitions(storage)
    if (identities.includes(identity)) return 'seen'
    identities.push(identity)
    storage.setItem(staleAssetReloadKey, JSON.stringify({ identities }))
    return 'recorded'
  } catch {
    return 'unavailable'
  }
}

export function versionedEntryURL(href: string, version: string): string {
  const target = new URL(href)
  target.searchParams.set(versionQueryParameter, version)
  return target.toString()
}

export function unversionedEntryURL(href: string): string {
  const target = new URL(href)
  target.searchParams.delete(versionQueryParameter)
  return target.toString()
}

export function hideApplicationVersion(
  target: EntryURLTarget = window
): boolean {
  const cleanURL = unversionedEntryURL(target.location.href)
  if (cleanURL === target.location.href) return false
  target.history.replaceState(target.history.state, '', cleanURL)
  return true
}

export function switchApplicationVersion(
  target: Pick<RecoveryWindow, 'location' | 'sessionStorage'>,
  currentVersion: string,
  serverVersion: string
): boolean {
  const normalizedServerVersion = serverVersion.trim()
  if (!normalizedServerVersion) return false

  const identity = `${currentVersion.trim() || 'unknown'}->${normalizedServerVersion}`
  const targetURL = versionedEntryURL(
    target.location.href,
    normalizedServerVersion
  )
  const changesEntryURL = targetURL !== target.location.href
  const transition = recordTransition(target.sessionStorage, identity)
  if (transition === 'seen') return false
  if (transition === 'unavailable' && !changesEntryURL) return false

  target.location.replace(targetURL)
  return true
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
  target: Pick<RecoveryWindow, 'location' | 'sessionStorage'> = window,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): Promise<boolean> {
  try {
    return switchApplicationVersion(
      target,
      currentVersion,
      await readVersion()
    )
  } catch {
    return false
  }
}

export async function checkForApplicationUpdate(
  target: Pick<RecoveryWindow, 'location' | 'sessionStorage'> = window,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion,
  canSwitch: () => boolean = () => true
): Promise<boolean> {
  let serverVersion: string
  try {
    serverVersion = await readVersion()
  } catch {
    return false
  }
  const entryVersion = new URL(target.location.href).searchParams.get(
    versionQueryParameter
  )
  if (
    (serverVersion === currentVersion && entryVersion === serverVersion) ||
    !canSwitch()
  ) {
    return false
  }
  return switchApplicationVersion(target, currentVersion, serverVersion)
}

export function installStaleAssetRecovery(
  target: Pick<RecoveryWindow, 'addEventListener' | 'location' | 'sessionStorage'> = window,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): void {
  target.addEventListener('vite:preloadError', async event => {
    event.preventDefault()
    await recoverAfterStaleAsset(target, currentVersion, readVersion)
  })
}

export function initializeApplicationVersionChecks(
  target: RecoveryWindow = window,
  documentTarget: RecoveryDocument = document,
  canSwitch: () => boolean = () => true,
  currentVersion = clientVersion || 'dev',
  readVersion: ServerVersionReader = readServerVersion
): () => void {
  let stopped = false
  let inFlight: Promise<boolean> | undefined
  const check = () => {
    if (stopped || inFlight) return
    inFlight = checkForApplicationUpdate(
      target,
      currentVersion,
      readVersion,
      canSwitch
    ).finally(() => {
      inFlight = undefined
    })
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
