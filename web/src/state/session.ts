import { computed, reactive, readonly } from 'vue'
import { ApiError } from '../api/types'
import type { SessionResponse } from '../api/types'
import {
  fixtureMode,
  gateway,
  setAuthenticationRequiredHandler,
  setClientCSRFToken
} from '../api/client'
import { setSystemLanguage, translate } from '../i18n'

export type SessionStatus = 'unknown' | 'checking' | 'authenticated' | 'anonymous'

const state = reactive({
  status: (fixtureMode ? 'authenticated' : 'unknown') as SessionStatus,
  username: fixtureMode ? 'fixture' : '',
  setupRequired: false,
  error: ''
})

let inspection: Promise<boolean> | undefined

function applySession(session: SessionResponse): boolean {
  setSystemLanguage(session.language)
  if (!session.authenticated) {
    clearSession('', session.setup_required)
    return false
  }

  state.status = 'authenticated'
  state.username = session.username || ''
  state.setupRequired = false
  state.error = ''
  setClientCSRFToken(session.csrf_token)
  return true
}

export function clearSession(message = '', setupRequired = false): void {
  if (fixtureMode) return
  state.status = 'anonymous'
  state.username = ''
  state.setupRequired = setupRequired
  state.error = message
  setClientCSRFToken()
}

setAuthenticationRequiredHandler(() => {
  clearSession(translate('auth.sessionExpired'))
})

export const sessionState = readonly(state)
export const isAuthenticated = computed(() => state.status === 'authenticated')

export async function ensureSession(): Promise<boolean> {
  if (fixtureMode || state.status === 'authenticated') return true
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

  state.status = 'checking'
  state.error = ''
  try {
    const session = await gateway.login({ username, password })
    if (!applySession(session)) {
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

  state.status = 'checking'
  state.error = ''
  try {
    const session = await gateway.setup({ username, password })
    if (!applySession(session)) {
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

export async function logout(): Promise<void> {
  if (fixtureMode) return
  await gateway.logout()
  clearSession()
}
