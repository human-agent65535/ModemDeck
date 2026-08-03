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
import {
  maximumPasswordBytes,
  minimumPasswordCharacters,
  passwordByteCount,
  passwordCharacterCount
} from '../src/utils/password.ts'

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
  const [component, session, account, security, system, settings, login] = await Promise.all([
    source('../src/components/AccountSecurityForm.vue'),
    source('../src/state/session.ts'),
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/SecuritySettingsPanel.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/views/SettingsView.vue'),
    source('../src/views/LoginView.vue')
  ])

  assert.equal(minimumPasswordCharacters, 8)
  assert.equal(maximumPasswordBytes, 1024)
  assert.equal(passwordCharacterCount('密码密码密码密码'), 8)
  assert.equal(passwordByteCount('密码密码密码密码'), 24)
  assert.match(component, /passwordCharacterCount\(newPassword\.value\)/)
  assert.match(component, /newPassword\.value !== confirmation\.value/)
  assert.match(component, /await changePassword/)
  assert.match(
    session,
    /terminateSession\(\(\) => gateway\.changePassword\(input\)\)/
  )
  assert.match(
    session,
    /async function terminateSession[\s\S]*?await operation\(\)[\s\S]*?clearSession\(\)/
  )
  assert.match(component, /query: \{ passwordChanged: '1' \}/)
  assert.doesNotMatch(account, /<AccountSecurityForm \/>/)
  assert.match(security, /<AccountSecurityForm \/>/)
  assert.ok(
    security.indexOf('<AccountSecurityForm />') <
      security.indexOf('<AccountSessionsPanel')
  )
  assert.doesNotMatch(system, /<AccountSecurityForm \/>/)
  assert.match(settings, /id: 'security'/)
  assert.match(settings, /selectedSection === 'security'/)
  assert.match(system, /<SelectControl/)
  assert.doesNotMatch(system, /<select/)
  assert.match(login, /t\('auth\.passwordChanged'\)/)
})

test('signed-in devices support one-device and all-other-device logout', async () => {
  const [client, gateway, panel, security] = await Promise.all([
    source('../src/api/client.ts'),
    source('../src/api/gateway.ts'),
    source('../src/components/AccountSessionsPanel.vue'),
    source('../src/components/SecuritySettingsPanel.vue')
  ])

  assert.match(gateway, /listAccountSessions\(\): Promise<AccountSession\[\]>/)
  assert.match(gateway, /logoutAccountSession\(id: string\): Promise<void>/)
  assert.match(gateway, /logoutOtherAccountSessions\(\): Promise<void>/)
  assert.match(client, /get\(`\$\{API_ROOT\}\/account\/sessions`\)/)
  assert.match(client, /last_seen_at: lastSeenAt/)
  assert.match(client, /access_ip: stringProperty\(session, 'access_ip'\)/)
  assert.match(
    client,
    /`\$\{API_ROOT\}\/account\/sessions\/\$\{encodeURIComponent\(id\)\}`[\s\S]*?method: 'DELETE'/
  )
  assert.match(
    client,
    /`\$\{API_ROOT\}\/account\/sessions\/others`[\s\S]*?method: 'DELETE'/
  )
  assert.match(panel, /v-if="session\.current"/)
  assert.match(panel, /session\.access_ip\?\.trim\(\)/)
  assert.match(panel, /account\.lastActiveAt/)
  assert.match(panel, /@click="emit\('revoke', session\.id\)"/)
  assert.match(panel, /@click="emit\('revokeOthers'\)"/)
  assert.match(security, /sessions\.value = sessions\.value\.filter\(session => session\.id !== id\)/)
  assert.match(security, /sessions\.value = sessions\.value\.filter\(session => session\.current\)/)
})

test('first web visit exposes Quick Start and creates the administrator', async () => {
  const [client, gateway, session, login] = await Promise.all([
    source('../src/api/client.ts'),
    source('../src/api/gateway.ts'),
    source('../src/state/session.ts'),
    source('../src/views/LoginView.vue')
  ])

  assert.match(gateway, /setup\(input: SetupInput\): Promise<SessionResponse>/)
  assert.match(
    client,
    /writePublicJSON\(`\$\{API_ROOT\}\/setup`, 'POST', input, 201\)/
  )
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
  assert.equal(enUS.settings.account, 'Security')
  assert.equal(zhCN.settings.account, '安全')
})
