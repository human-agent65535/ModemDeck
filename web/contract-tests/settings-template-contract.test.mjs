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

  for (const preference of [defaultLine, language]) {
    assert.match(preference, /SettingsPreferenceRow/)
  }
  assert.match(recording, /SettingsPreferenceRow/)
  assert.doesNotMatch(recording, /SettingsSection|SettingsControlRow/)
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
  assert.match(audio, /<RecordingSettingsForm \/>/)
})

test('account preferences share one control edge and call recording belongs to audio', async () => {
  const [account, audio] = await Promise.all([
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/AudioSettingsForm.vue')
  ])

  assert.match(
    account,
    /\.account-preferences :deep\(\.settings-preference-row\) \{[\s\S]*max-width: none;/
  )
  assert.doesNotMatch(account, /RecordingSettingsForm/)
  assert.match(audio, /import RecordingSettingsForm from/)
  assert.match(audio, /<RecordingSettingsForm \/>/)
})

test('binary preferences share one switch primitive with semantic variants', async () => {
  const [style, users, telegram, devices, audio, recording, contacts] = await Promise.all([
    source('../src/style.css'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/AudioSettingsForm.vue'),
    source('../src/components/RecordingSettingsForm.vue'),
    source('../src/components/ContactEditor.vue')
  ])

  assert.match(style, /\.ui-switch\s*\{/)
  assert.match(style, /\.ui-switch--compact\s*\{/)
  assert.match(style, /\.ui-switch--danger:checked\s*\{/)
  for (const component of [users, telegram, devices, audio, recording, contacts]) {
    assert.match(component, /class="ui-switch/)
  }
  assert.match(telegram, /class="ui-switch ui-switch--compact"/)
  assert.match(recording, /class="ui-switch ui-switch--danger"/)
  assert.doesNotMatch(users, /\.user-account-access input\s*\{/)
  assert.doesNotMatch(devices, /\.configuration-toggle input\s*\{/)
  assert.doesNotMatch(recording, /\.recording-settings__control input\s*\{/)
})

test('the current user hierarchy keeps personal language near identity and line scope readable', async () => {
  const [account, users, telegram, lineSelector, lineIdentity, lineScopeList] = await Promise.all([
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/LineSelector.vue'),
    source('../src/components/LineIdentity.vue'),
    source('../src/components/settings/SettingsLineScopeList.vue')
  ])

  assert.match(account, /showLanguage\?: boolean/)
  assert.match(account, /<SystemSettingsForm v-if="props\.showLanguage" \/>/)
  assert.match(users, /import SystemSettingsForm from/)
  assert.match(users, /class="user-system-language"/)
  assert.match(users, /:show-language="false"/)
  assert.ok(
    users.indexOf('class="user-system-language"') <
      users.indexOf('class="user-account-access"'),
    'language preference should precede account access'
  )
  const systemLanguageStyle = users.match(/\.user-system-language \{[^}]*\}/)?.[0]
  assert.ok(systemLanguageStyle)
  assert.match(systemLanguageStyle, /max-width: none;/)
  assert.match(users, /<SettingsLineScopeList/)
  assert.match(telegram, /<SettingsLineScopeList/)
  assert.match(lineSelector, /import LineIdentity from/)
  assert.match(lineSelector, /<LineIdentity/)
  assert.match(lineScopeList, /import LineIdentity from/)
  assert.match(lineScopeList, /<LineIdentity/)
  assert.match(lineIdentity, /line-identity__icon/)
  assert.match(
    lineScopeList,
    /grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/
  )
})

test('page-level async settings reuse the message-list skeleton through the shared state panel', async () => {
  const [statePanel, listSkeleton, messages, externalAccess, certificate] = await Promise.all([
    source('../src/components/StatePanel.vue'),
    source('../src/components/ListSkeleton.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/WebCertificateSettingsPanel.vue')
  ])

  assert.match(statePanel, /import ListSkeleton from '\.\/ListSkeleton\.vue'/)
  assert.match(statePanel, /<ListSkeleton[\s\S]*variant="content"/)
  assert.doesNotMatch(statePanel, /LoaderCircle|state-panel__loading-mark/)
  assert.match(listSkeleton, /variant\?: 'list' \| 'content'/)
  assert.match(listSkeleton, /list-skeleton-shimmer/)
  assert.match(messages, /<ListSkeleton[\s\S]*threadsResource\.status === 'loading'/)
  for (const panel of [externalAccess, certificate]) {
    assert.match(panel, /import StatePanel from '\.\/StatePanel\.vue'/)
    assert.match(panel, /<StatePanel v-if="loading" state="loading"/)
  }
})

test('master-detail settings are flush and mobile page titles cover every section', async () => {
  const [view, masterDetail, shell] = await Promise.all([
    source('../src/views/SettingsView.vue'),
    source('../src/components/settings/SettingsMasterDetail.vue'),
    source('../src/components/AppShell.vue')
  ])

  assert.match(view, /settings-content--master-detail/)
  assert.match(
    view,
    /\.settings-content\.settings-content--master-detail \{[\s\S]*min-height: 0;[\s\S]*padding: 0;[\s\S]*overflow: hidden;/
  )
  assert.match(
    masterDetail,
    /\.settings-master-detail \{[\s\S]*height: 100%;[\s\S]*min-height: 0;[\s\S]*overflow: hidden;/
  )
  assert.match(
    masterDetail,
    /\.settings-master-detail__list \{[\s\S]*overflow-y: auto;/
  )
  assert.match(
    masterDetail,
    /\.settings-master-detail__detail-content \{[\s\S]*overflow-y: auto;[\s\S]*overscroll-behavior: contain;/
  )
  assert.match(shell, /'external-access': t\('settings\.iosApp'\)/)
  assert.match(shell, /'web-certificate': t\('settings\.tls'\)/)
  assert.match(shell, /about: t\('settings\.about'\)/)
})

test('settings drilldown aligns with the shell compact breakpoint', async () => {
  const [masterDetail, deviceWorkspace, users, telegram, devices, style, shell, view] =
    await Promise.all([
      source('../src/components/settings/SettingsMasterDetail.vue'),
      source('../src/components/settings/DeviceWorkspace.vue'),
      source('../src/components/UserSettingsPanel.vue'),
      source('../src/components/TelegramSettingsForm.vue'),
      source('../src/components/DeviceConfigurationPanel.vue'),
      source('../src/style.css'),
      source('../src/components/AppShell.vue'),
      source('../src/views/SettingsView.vue')
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
  assert.doesNotMatch(masterDetail, /mobileMode|--stack/)
  assert.match(users, /class="settings-resource-row user-row/)
  assert.match(telegram, /class="settings-resource-row telegram-unit-row/)
  assert.match(telegram, /route\.query\.bot/)
  assert.match(telegram, /route\.query\.newBot/)
  assert.match(shell, /const settingsTelegramDetailOpen = computed/)
  assert.match(view, /selectedSection\.value === 'telegram'/)
})

test('settings section and resource changes use the shared stable transition', async () => {
  const [transition, view, masterDetail, users, telegram] = await Promise.all([
    source('../src/components/settings/SettingsContentTransition.vue'),
    source('../src/views/SettingsView.vue'),
    source('../src/components/settings/SettingsMasterDetail.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue')
  ])

  assert.match(view, /<SettingsContentTransition :content-key="selectedSection">/)
  assert.match(transition, /@media \(min-width: 861px\)/)
  assert.match(transition, /animation: settings-content-in var\(--motion-base\)/)
  assert.match(masterDetail, /detailKey\?: string \| number \| null/)
  assert.match(masterDetail, /settings-master-detail__detail-content/)
  assert.match(
    masterDetail,
    /animation: settings-master-detail-content-in var\(--motion-base\)/
  )
  assert.match(users, /:detail-key="creating \? '__new_member__' : selectedID"/)
  assert.match(
    telegram,
    /:detail-key="creating \? '__telegram_bot_draft__' : selectedID"/
  )
})

test('page width modes are shared instead of owned by business panels', async () => {
  const [frame, view, traffic, style, account, audio, contacts, externalAccess] =
    await Promise.all([
      source('../src/components/PageContentFrame.vue'),
      source('../src/views/SettingsView.vue'),
      source('../src/views/TrafficView.vue'),
      source('../src/style.css'),
      source('../src/components/AccountSettingsPanel.vue'),
      source('../src/components/AudioSettingsForm.vue'),
      source('../src/components/ContactSyncSettings.vue'),
      source('../src/components/ExternalAccessSettingsPanel.vue')
    ])

  assert.match(frame, /'reading' \| 'dashboard' \| 'fluid'/)
  assert.match(frame, /max-width: var\(--page-content-reading-max\)/)
  assert.match(frame, /max-width: var\(--page-content-dashboard-max\)/)
  assert.match(style, /--page-content-reading-max: 960px/)
  assert.match(style, /--page-content-dashboard-max: 1440px/)
  assert.match(view, /<PageContentFrame mode="reading">[\s\S]*<AboutSettingsPanel/)
  assert.match(view, /<PageContentFrame mode="fluid">[\s\S]*<DiagnosticsPanel/)
  assert.match(traffic, /<PageContentFrame mode="dashboard" class="traffic-page__content">/)
  for (const panel of [account, audio, contacts, externalAccess]) {
    assert.doesNotMatch(panel, /max-width:\s*(760|880)px/)
  }
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
