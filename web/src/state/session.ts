import { computed, reactive, readonly } from 'vue'
import { ApiError } from '../api/types'
import type { SessionResponse } from '../api/types'
import {
  fixtureMode,
  gateway,
  setAuthenticationRequiredHandler,
  setClientCSRFToken
} from '../api/client'

export type SessionStatus = 'unknown' | 'checking' | 'authenticated' | 'anonymous'

const state = reactive({
  status: (fixtureMode ? 'authenticated' : 'unknown') as SessionStatus,
  username: fixtureMode ? 'fixture' : '',
  error: ''
})

let inspection: Promise<boolean> | undefined

function applySession(session: SessionResponse): boolean {
  if (!session.authenticated) {
    clearSession()
    return false
  }

  state.status = 'authenticated'
  state.username = session.username || ''
  state.error = ''
  setClientCSRFToken(session.csrf_token)
  return true
}

export function clearSession(message = ''): void {
  if (fixtureMode) return
  state.status = 'anonymous'
  state.username = ''
  state.error = message
  setClientCSRFToken()
}

setAuthenticationRequiredHandler(() => {
  clearSession('登录状态已失效，请重新登录')
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
      const message = error instanceof Error ? error.message : '无法确认登录状态'
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
      throw new ApiError('用户名或密码不正确', 401, 'authentication_required')
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '登录失败'
    clearSession(message)
    throw error
  }
}

export async function logout(): Promise<void> {
  if (fixtureMode) return
  await gateway.logout()
  clearSession()
}
