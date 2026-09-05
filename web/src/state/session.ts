import { computed, reactive, readonly } from 'vue'
import { ApiError } from '../api/types'
import type { ChangePasswordInput, SessionResponse } from '../api/types'
import {
  fixtureMode,
  gateway,
  rotateAuthenticationRequestScope,
  setAuthenticationRequiredHandler,
  setClientCSRFToken
} from '../api/client'
import { setSystemLanguage, translate } from '../i18n'
import { requestActiveCallRefresh } from './call'
import { releaseCallMediaForSessionEnd } from './callMedia'
import { resetDeviceConfigurationState } from './deviceConfiguration'
import { resetNetworkState } from './network'
import { resetRecordingState } from './recording'
import { resetUIState } from './ui'
import { resetWorkspaceState } from './workspace'

export type SessionStatus = 'unknown' | 'checking' | 'authenticated' | 'anonymous'

const state = reactive({
  status: (fixtureMode ? 'authenticated' : 'unknown') as SessionStatus,
  userID: fixtureMode ? 'user_admin' : '',
  username: fixtureMode ? 'fixture' : '',
  role: (fixtureMode ? 'admin' : '') as '' | 'admin' | 'member',
  profileContactID: '',
  iosPairingEnabled: fixtureMode,
  allowedLineIDs: [] as string[],
  setupRequired: false,
  error: ''
})

let inspection: Promise<boolean> | undefined
let sessionTerminationInProgress = false
let sessionInvalidatedDuringTermination = false

type AuthorizationScope = {
  userID: string
  role: string
  allowedLineIDs: readonly string[]
}

export function authorizationScopeChanged(
  current: AuthorizationScope,
  next: AuthorizationScope
): boolean {
  return authorizationScopeSignature(current) !== authorizationScopeSignature(next)
}

function authorizationScopeSignature(scope: AuthorizationScope): string {
  const lineIDs = Array.from(
    new Set(scope.allowedLineIDs.map(lineID => lineID.trim()).filter(Boolean))
  ).sort()
  return JSON.stringify([scope.userID.trim(), scope.role.trim(), lineIDs])
}

async function applySession(session: SessionResponse): Promise<boolean> {
  if (!session.authenticated) {
    clearSession('', session.setup_required)
    return false
  }

  await setSystemLanguage(session.language)

  const nextScope = {
    userID: session.user_id || '',
    role: session.role || 'admin',
    allowedLineIDs: session.allowed_line_ids || []
  }
  const reloadForAuthorizationChange =
    state.status === 'authenticated' &&
    authorizationScopeChanged(
      {
        userID: state.userID,
        role: state.role,
        allowedLineIDs: state.allowedLineIDs
      },
      nextScope
    )

  if (
    state.status !== 'authenticated' ||
    state.userID !== nextScope.userID ||
    reloadForAuthorizationChange
  ) {
    rotateAuthenticationRequestScope()
    resetDeviceConfigurationState()
  }
  state.status = 'authenticated'
  state.userID = nextScope.userID
  state.username = session.username || ''
  state.role = nextScope.role as 'admin' | 'member'
  state.profileContactID = session.profile_contact_id || ''
  state.iosPairingEnabled = session.ios_pairing_enabled
  state.allowedLineIDs = [...(session.allowed_line_ids || [])]
  state.setupRequired = false
  state.error = ''
  setClientCSRFToken(session.csrf_token)
  if (reloadForAuthorizationChange) window.location.reload()
  return true
}

export function clearSession(message = '', setupRequired = false): void {
  if (fixtureMode) return
  void setSystemLanguage('en-US')
  if (state.status === 'anonymous') {
    state.setupRequired = setupRequired
    state.error = message
    return
  }
  rotateAuthenticationRequestScope()
  state.status = 'anonymous'
  state.userID = ''
  state.username = ''
  state.role = ''
  state.profileContactID = ''
  state.iosPairingEnabled = false
  state.allowedLineIDs = []
  state.setupRequired = setupRequired
  state.error = message
  setClientCSRFToken()
  resetWorkspaceState()
  resetDeviceConfigurationState()
  resetNetworkState()
  resetRecordingState()
  resetUIState()
}

setAuthenticationRequiredHandler(() => {
  if (sessionTerminationInProgress) {
    sessionInvalidatedDuringTermination = true
    return
  }
  if (state.status !== 'authenticated') return
  clearSession(translate('auth.sessionExpired'))
})

export const sessionState = readonly(state)
export const isAuthenticated = computed(() => state.status === 'authenticated')
export const isAdministrator = computed(() => state.role === 'admin')

export async function refreshSession(): Promise<boolean> {
  if (fixtureMode) return true
  return applySession(await gateway.getSession())
}

export function setSessionProfileContact(contactID: string): void {
  state.profileContactID = contactID.trim()
}

export async function ensureSession(): Promise<boolean> {
  if (fixtureMode) {
    await setSystemLanguage('auto')
    return true
  }
  if (state.status === 'authenticated') return true
  if (state.status === 'anonymous') return false
  if (inspection) return inspection

  state.status = 'checking'
  state.error = ''
  inspection = gateway
    .getSession()
    .then(applySession)
    .catch(error => {
      const message = error instanceof Error ? error.message : translate('auth.checkFailed')
      clearSession(message)
      return false
    })
    .finally(() => {
      inspection = undefined
    })
  return inspection
}

export async function login(username: string, password: string): Promise<void> {
  if (fixtureMode) return

  rotateAuthenticationRequestScope()
  state.status = 'checking'
  state.error = ''
  try {
    const session = await gateway.login({ username, password })
    if (!(await applySession(session))) {
      throw new ApiError(
        translate('auth.invalidCredentials'),
        401,
        'authentication_required'
      )
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : translate('auth.loginFailed')
    clearSession(message)
    throw error
  }
}

export async function setup(username: string, password: string): Promise<void> {
  if (fixtureMode) return

  rotateAuthenticationRequestScope()
  state.status = 'checking'
  state.error = ''
  try {
    const session = await gateway.setup({ username, password })
    if (!(await applySession(session))) {
      throw new ApiError(
        translate('auth.setupFailed'),
        409,
        'setup_complete'
      )
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : translate('auth.setupFailed')
    clearSession(message, true)
    throw error
  }
}

async function terminateSession(operation: () => Promise<void>): Promise<void> {
  sessionTerminationInProgress = true
  sessionInvalidatedDuringTermination = false
  rotateAuthenticationRequestScope()
  try {
    await releaseCallMediaForSessionEnd()
    await operation()
    clearSession()
  } catch (error) {
    if (
      sessionInvalidatedDuringTermination ||
      (error instanceof ApiError && error.status === 401)
    ) {
      clearSession()
      return
    }
    void requestActiveCallRefresh()
    throw error
  } finally {
    sessionTerminationInProgress = false
    sessionInvalidatedDuringTermination = false
  }
}

export async function logout(): Promise<void> {
  if (fixtureMode) return
  await terminateSession(() => gateway.logout())
}

export async function changePassword(input: ChangePasswordInput): Promise<void> {
  if (fixtureMode) return
  await terminateSession(() => gateway.changePassword(input))
}
