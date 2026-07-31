import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('settings pages use the shared layout templates and the device workbench exception', async () => {
  const [
    defaultLine,
    language,
    recording,
    about,
    externalAccess,
    contactSync,
    users,
    telegram,
    devices,
    audio
  ] = await Promise.all([
    source('../src/components/DefaultLineSettingsForm.vue'),
    source('../src/components/SystemSettingsForm.vue'),
    source('../src/components/RecordingSettingsForm.vue'),
    source('../src/components/AboutSettingsPanel.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/ContactSyncSettings.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/AudioSettingsForm.vue')
  ])

  for (const preference of [defaultLine, language, recording]) {
    assert.match(preference, /SettingsPreferenceRow/)
  }
  for (const modulePage of [about, externalAccess, contactSync]) {
    assert.match(modulePage, /SettingsModuleCard/)
  }
  for (const resourceEditor of [users, telegram]) {
    assert.match(resourceEditor, /SettingsMasterDetail/)
  }
  assert.match(devices, /DeviceWorkspace/)
  assert.match(devices, /#selector/)
  assert.doesNotMatch(devices, /SettingsMasterDetail/)
  assert.match(audio, /SettingsSection/)
  assert.match(audio, /SettingsControlRow/)
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

test('settings drilldown aligns with the shell compact breakpoint', async () => {
  const [masterDetail, deviceWorkspace, users, telegram, devices, style] =
    await Promise.all([
      source('../src/components/settings/SettingsMasterDetail.vue'),
      source('../src/components/settings/DeviceWorkspace.vue'),
      source('../src/components/UserSettingsPanel.vue'),
      source('../src/components/TelegramSettingsForm.vue'),
      source('../src/components/DeviceConfigurationPanel.vue'),
      source('../src/style.css')
    ])

  for (const component of [
    masterDetail,
    deviceWorkspace,
    users,
    telegram,
    devices
  ]) {
    assert.match(component, /@media \(max-width: 860px\)/)
  }
  assert.match(masterDetail, /mobile-drilldown__list/)
  assert.match(masterDetail, /mobile-drilldown__detail/)
  assert.match(deviceWorkspace, /mobile-drilldown__list/)
  assert.match(deviceWorkspace, /mobile-drilldown__detail/)
  assert.match(style, /\.mobile-drilldown\.is-detail-open/)
  assert.match(style, /@keyframes mobile-drilldown-forward/)
})

test('save behavior distinguishes immediate preferences from dirty resource forms', async () => {
  const account = await source('../src/components/AccountSettingsPanel.vue')
  const profile = await source('../src/components/AccountProfileSetting.vue')
  const defaultLine = await source('../src/components/DefaultLineSettingsForm.vue')
  const language = await source('../src/components/SystemSettingsForm.vue')
  const recording = await source('../src/components/RecordingSettingsForm.vue')
  const device = await source('../src/components/DeviceConfigurationPanel.vue')
  const users = await source('../src/components/UserSettingsPanel.vue')
  const telegram = await source('../src/components/TelegramSettingsForm.vue')

  assert.match(account, /<AccountProfileSetting/)
  assert.match(profile, /@change="changeProfile"/)
  for (const immediate of [profile, defaultLine, language, recording, device]) {
    assert.match(immediate, /useSettingsMutation/)
    assert.match(immediate, /SettingsSaveStatus/)
  }
  assert.match(users, /async function toggleLine/)
  assert.match(users, /const formChanged = computed/)
  assert.match(users, /v-if="creating \|\| selectedUser\?\.role === 'member'"/)
  assert.match(telegram, /async function persistLineScopes/)
  assert.match(telegram, /const formChanged = computed/)
})
