import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('account password changes use the authenticated CSRF-protected API', async () => {
  const [client, gateway] = await Promise.all([
    source('../src/api/client.ts'),
    source('../src/api/gateway.ts')
  ])

  assert.match(gateway, /changePassword\(input: ChangePasswordInput\): Promise<void>/)
  assert.match(
    client,
    /writeJSON\(`\$\{API_ROOT\}\/account\/password`, 'PUT', input, 204\)/
  )
})

test('system settings validate password replacement and end the current session', async () => {
  const [component, system, settings, login] = await Promise.all([
    source('../src/components/AccountSecurityForm.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/views/SettingsView.vue'),
    source('../src/views/LoginView.vue')
  ])

  assert.match(component, /const minimumPasswordBytes = 12/)
  assert.match(component, /newPassword\.value !== confirmation\.value/)
  assert.match(component, /await gateway\.changePassword/)
  assert.match(component, /clearSession\(\)/)
  assert.match(component, /query: \{ passwordChanged: '1' \}/)
  assert.match(system, /<AccountSecurityForm \/>/)
  assert.doesNotMatch(settings, /id: 'account'/)
  assert.match(system, /<select/)
  assert.match(login, /t\('auth\.passwordChanged'\)/)
})

test('first web visit exposes Quick Start and creates the administrator', async () => {
  const [client, gateway, session, login] = await Promise.all([
    source('../src/api/client.ts'),
    source('../src/api/gateway.ts'),
    source('../src/state/session.ts'),
    source('../src/views/LoginView.vue')
  ])

  assert.match(gateway, /setup\(input: SetupInput\): Promise<SessionResponse>/)
  assert.match(client, /writeJSON\(`\$\{API_ROOT\}\/setup`, 'POST', input, 201\)/)
  assert.match(client, /typeof source\.setup_required !== 'boolean'/)
  assert.match(session, /clearSession\('', session\.setup_required\)/)
  assert.match(session, /await gateway\.setup\(\{ username, password \}\)/)
  assert.match(login, /sessionState\.setupRequired/)
  assert.match(login, /t\('auth\.quickStart'\)/)
  assert.match(login, /v-model="confirmation"/)
})
