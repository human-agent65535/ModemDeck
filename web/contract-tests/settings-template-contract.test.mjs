import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('settings pages use the three shared layout templates', async () => {
  const [
    defaultLine,
    language,
    recording,
    about,
    externalAccess,
    users,
    telegram
  ] = await Promise.all([
    source('../src/components/DefaultLineSettingsForm.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/components/RecordingSettingsForm.vue'),
    source('../src/components/AboutSettingsPanel.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue')
  ])

  for (const preference of [defaultLine, language, recording]) {
    assert.match(preference, /SettingsPreferenceRow/)
  }
  for (const modulePage of [about, externalAccess]) {
    assert.match(modulePage, /SettingsModuleCard/)
  }
  for (const resourceEditor of [users, telegram]) {
    assert.match(resourceEditor, /SettingsMasterDetail/)
  }
})

test('master-detail settings are flush and mobile page titles cover every section', async () => {
  const view = await source('../src/views/SettingsView.vue')
  const shell = await source('../src/components/AppShell.vue')

  assert.match(view, /settings-content--master-detail/)
  assert.match(
    view,
    /\.settings-content\.settings-content--master-detail \{\s*padding: 0;/
  )
  assert.match(shell, /'external-access': t\('settings\.iosApp'\)/)
  assert.match(shell, /'web-certificate': t\('settings\.tls'\)/)
  assert.match(shell, /about: t\('settings\.about'\)/)
})

test('save behavior distinguishes immediate preferences from dirty resource forms', async () => {
  const account = await source('../src/components/AccountSettingsPanel.vue')
  const users = await source('../src/components/UserSettingsPanel.vue')
  const telegram = await source('../src/components/TelegramSettingsForm.vue')

  assert.match(account, /@change="changeProfile"/)
  assert.match(users, /async function toggleLine/)
  assert.match(users, /const formChanged = computed/)
  assert.match(users, /v-if="creating \|\| selectedUser\?\.role === 'member'"/)
  assert.match(telegram, /async function persistLineScopes/)
  assert.match(telegram, /const formChanged = computed/)
})
