import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import { login, logout, sessionState } from '../src/state/session.ts'

function sessionResponse() {
  return new Response(
    JSON.stringify({
      authenticated: true,
      setup_required: false,
      user_id: 'user_member',
      username: 'member',
      role: 'member',
      csrf_token: 'csrf-token',
      ios_pairing_enabled: false,
      allowed_line_ids: ['line-1'],
      language: 'en-US'
    }),
    {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }
  )
}

function unauthorizedResponse() {
  return new Response(
    JSON.stringify({
      code: 'authentication_required',
      message: 'Authentication is required'
    }),
    {
      status: 401,
      headers: { 'Content-Type': 'application/json' }
    }
  )
}

test('an intentional logout suppresses concurrent session-expired feedback', async () => {
  const originalFetch = globalThis.fetch
  let releaseLogout
  let signalLogoutStarted
  const logoutStarted = new Promise(resolve => {
    signalLogoutStarted = resolve
  })

  globalThis.fetch = async (path, init = {}) => {
    const method = (init.method || 'GET').toUpperCase()
    if (method === 'POST' && String(path).endsWith('/session')) {
      return sessionResponse()
    }
    if (method === 'DELETE' && String(path).endsWith('/session')) {
      signalLogoutStarted()
      return new Promise(resolve => {
        releaseLogout = resolve
      })
    }
    return unauthorizedResponse()
  }

  try {
    await login('member', 'password')
    assert.equal(sessionState.status, 'authenticated')

    const logoutRequest = logout()
    await logoutStarted

    await assert.rejects(
      gateway.listContacts(),
      error => error?.status === 401 && error?.code === 'authentication_required'
    )
    assert.equal(sessionState.status, 'authenticated')
    assert.equal(sessionState.error, '')

    releaseLogout(new Response(null, { status: 204 }))
    await logoutRequest

    assert.equal(sessionState.status, 'anonymous')
    assert.equal(sessionState.error, '')

    globalThis.fetch = async (path, init = {}) => {
      const method = (init.method || 'GET').toUpperCase()
      if (method === 'POST' && String(path).endsWith('/session')) {
        return sessionResponse()
      }
      return unauthorizedResponse()
    }
    await login('member', 'password')
    await logout()

    assert.equal(sessionState.status, 'anonymous')
    assert.equal(sessionState.error, '')
  } finally {
    globalThis.fetch = originalFetch
  }
})
