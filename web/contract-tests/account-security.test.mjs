import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import deDE from '../src/i18n/locales/de-DE.ts'
import enUS from '../src/i18n/locales/en-US.ts'
import esES from '../src/i18n/locales/es-ES.ts'
import frFR from '../src/i18n/locales/fr-FR.ts'
import jaJP from '../src/i18n/locales/ja-JP.ts'
import ptBR from '../src/i18n/locales/pt-BR.ts'
import viVN from '../src/i18n/locales/vi-VN.ts'
import zhCN from '../src/i18n/locales/zh-CN.ts'
import zhTW from '../src/i18n/locales/zh-TW.ts'

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

test('account settings validate password replacement and end the current session', async () => {
  const [component, account, system, settings, login] = await Promise.all([
    source('../src/components/AccountSecurityForm.vue'),
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/views/SettingsView.vue'),
    source('../src/views/LoginView.vue')
  ])

  assert.match(component, /const minimumPasswordBytes = 12/)
  assert.match(component, /newPassword\.value !== confirmation\.value/)
  assert.match(component, /await gateway\.changePassword/)
  assert.match(component, /clearSession\(\)/)
  assert.match(component, /query: \{ passwordChanged: '1' \}/)
  assert.match(account, /<AccountSecurityForm \/>/)
  assert.doesNotMatch(system, /<AccountSecurityForm \/>/)
  assert.match(settings, /id: 'account'/)
  assert.match(settings, /selectedSection === 'account'/)
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

test('normal sign-in and account identity copy is role-neutral', async () => {
  const settings = await source('../src/views/SettingsView.vue')
  const locales = [deDE, enUS, esES, frFR, jaJP, ptBR, viVN, zhCN, zhTW]
  const administrativeCopy =
    /administrator|administrador|administrateur|administration|administrativa|admin-|管理员|管理員|管理者|管理インターフェイス|quản trị/i

  for (const locale of locales) {
    assert.doesNotMatch(locale.auth.title, administrativeCopy)
    assert.doesNotMatch(locale.settings.systemLanguageDescription, administrativeCopy)
  }
  assert.match(settings, /sessionState\.username \|\| t\('settings\.account'\)/)
  assert.doesNotMatch(settings, /sessionState\.username \|\| t\('settings\.administrator'\)/)
})
