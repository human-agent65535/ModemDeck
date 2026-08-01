import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  settingsSectionGroup,
  visibleSettingsSectionIDs
} from '../src/components/settings/settingsNavigation.ts'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('settings navigation prioritizes audio and pairing before contact transfer', async () => {
  const view = await source('../src/views/SettingsView.vue')
  const audioIndex = view.indexOf("id: 'audio'")
  const pairingIndex = view.indexOf("id: 'pairing'")
  const contactsIndex = view.indexOf("id: 'contacts'")

  assert.ok(audioIndex >= 0)
  assert.ok(pairingIndex > audioIndex)
  assert.ok(contactsIndex > pairingIndex)
})

test('settings navigation groups personal and managed surfaces behind one visibility catalog', async () => {
  const view = await source('../src/views/SettingsView.vue')
  assert.deepEqual(
    visibleSettingsSectionIDs({ isAdmin: false, canPairIOS: false }),
    ['preferences', 'security', 'audio', 'contacts', 'devices', 'telegram']
  )
  assert.deepEqual(
    visibleSettingsSectionIDs({ isAdmin: false, canPairIOS: true }),
    ['preferences', 'security', 'audio', 'pairing', 'contacts', 'devices', 'telegram']
  )
  assert.deepEqual(
    visibleSettingsSectionIDs({ isAdmin: true, canPairIOS: true }),
    [
      'preferences',
      'security',
      'audio',
      'pairing',
      'contacts',
      'users',
      'devices',
      'telegram',
      'connectivity',
      'diagnostics',
      'about'
    ]
  )
  for (const section of ['preferences', 'security', 'audio', 'pairing', 'contacts']) {
    assert.equal(settingsSectionGroup(section), 'personal')
  }
  for (const section of [
    'users',
    'devices',
    'telegram',
    'connectivity',
    'diagnostics',
    'about'
  ]) {
    assert.equal(settingsSectionGroup(section), 'management')
  }

  assert.match(view, /v-for="group in sectionGroups"/)
  assert.match(view, /class="settings-nav-group"/)
  assert.match(view, /visibleSettingsSectionIDs\(/)
  assert.match(view, /settingsSectionGroup\(section\.id\)/)
})

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
    audio,
    profile
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
    source('../src/components/AudioSettingsForm.vue'),
    source('../src/components/AccountProfileSetting.vue')
  ])

  for (const preference of [defaultLine, language, profile]) {
    assert.match(preference, /SettingsPreferenceRow/)
  }
  assert.match(recording, /SettingsPreferenceRow/)
  assert.doesNotMatch(recording, /SettingsSection|SettingsControlRow/)
  for (const modulePage of [about, externalAccess, contactSync]) {
    assert.match(modulePage, /SettingsModuleCard/)
  }
  assert.match(
    contactSync,
    /\.contact-sync-direction \{[\s\S]*grid-template-columns: minmax\(112px, max-content\) minmax\(0, 1fr\);[\s\S]*text-align: left;/
  )
  assert.doesNotMatch(contactSync, /\.contact-sync-direction \{[^}]*0\.35fr/)
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

test('account preferences share one control width and call recording belongs to audio', async () => {
  const [style, row, account, profile, audio] = await Promise.all([
    source('../src/style.css'),
    source('../src/components/settings/SettingsPreferenceRow.vue'),
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/AccountProfileSetting.vue'),
    source('../src/components/AudioSettingsForm.vue')
  ])

  assert.match(style, /--settings-preference-content-max: 680px;/)
  assert.match(style, /--settings-control-column: min\(280px, 45%\);/)
  assert.match(row, /max-width: var\(--settings-preference-content-max\);/)
  assert.match(row, /width: var\(--settings-control-column\);/)
  assert.match(account, /max-width: var\(--settings-preference-content-max\);/)
  assert.doesNotMatch(account, /max-width: none;/)
  assert.match(profile, /control-size="wide"/)
  assert.doesNotMatch(account, /RecordingSettingsForm/)
  assert.match(audio, /import RecordingSettingsForm from/)
  assert.match(audio, /<RecordingSettingsForm \/>/)
})

test('preference rows and multi-control sections share the same content width', async () => {
  const [preferenceRow, section, audio] = await Promise.all([
    source('../src/components/settings/SettingsPreferenceRow.vue'),
    source('../src/components/settings/SettingsSection.vue'),
    source('../src/components/AudioSettingsForm.vue')
  ])

  for (const component of [preferenceRow, section]) {
    assert.match(component, /max-width: var\(--settings-preference-content-max\)/)
  }
  assert.doesNotMatch(audio, /recording-settings[\s\S]*max-width:\s*none/)
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

test('personal preferences and managed resource fields use separate settings panels', async () => {
  const [account, preferences, users, telegram, lineSelector, lineIdentity, lineScopeList] = await Promise.all([
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/AccountPreferencesPanel.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/LineSelector.vue'),
    source('../src/components/LineIdentity.vue'),
    source('../src/components/settings/SettingsLineScopeList.vue')
  ])

  assert.match(account, /showLanguage\?: boolean/)
  assert.match(account, /<SystemSettingsForm v-if="props\.showLanguage" \/>/)
  assert.doesNotMatch(users, /import SystemSettingsForm from/)
  assert.doesNotMatch(users, /import AccountProfileSetting from/)
  assert.doesNotMatch(users, /class="user-personal-settings"/)
  assert.doesNotMatch(users, /<AccountSettingsPanel/)
  assert.match(preferences, /<AccountSettingsPanel :show-identity="false" \/>/)
  assert.ok(
    account.indexOf('<SystemSettingsForm') <
      account.indexOf('<DefaultLineSettingsForm'),
    'language preference should precede default line'
  )
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

test('page-level async settings share one mutually exclusive skeleton boundary', async () => {
  const [
    statePanel,
    loadBoundary,
    asyncBoundary,
    listSkeleton,
    skeletonBlock,
    settingsSkeleton,
    settingsView,
    messages,
    ...settingsPanels
  ] = await Promise.all([
    source('../src/components/StatePanel.vue'),
    source('../src/components/settings/SettingsLoadBoundary.vue'),
    source('../src/components/settings/SettingsAsyncBoundary.vue'),
    source('../src/components/ListSkeleton.vue'),
    source('../src/components/SkeletonBlock.vue'),
    source('../src/components/settings/SettingsSkeleton.vue'),
    source('../src/views/SettingsView.vue'),
    source('../src/views/MessagesView.vue'),
    source('../src/components/AccountSettingsPanel.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/ContactSyncSettings.vue'),
    source('../src/components/AudioSettingsForm.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/WebCertificateSettingsPanel.vue'),
    source('../src/components/DiagnosticsPanel.vue'),
    source('../src/components/AboutSettingsPanel.vue')
  ])

  assert.match(statePanel, /import ListSkeleton from '\.\/ListSkeleton\.vue'/)
  assert.match(statePanel, /<ListSkeleton[\s\S]*variant="content"/)
  assert.doesNotMatch(statePanel, /LoaderCircle|state-panel__loading-mark/)
  assert.match(loadBoundary, /import StatePanel from '\.\.\/StatePanel\.vue'/)
  assert.match(loadBoundary, /import SettingsSkeleton from '\.\/SettingsSkeleton\.vue'/)
  assert.match(
    loadBoundary,
    /<SettingsSkeleton[\s\S]*v-if="loading"[\s\S]*<StatePanel[\s\S]*v-else-if="forbidden"[\s\S]*v-else-if="error"[\s\S]*<slot v-else \/>/
  )
  assert.match(asyncBoundary, /import SettingsSkeleton from '\.\/SettingsSkeleton\.vue'/)
  assert.match(asyncBoundary, /<Suspense>/)
  assert.match(asyncBoundary, /:shape="loadingShape"/)
  assert.match(asyncBoundary, /<PageContentFrame v-else :mode="frameMode">/)
  assert.match(settingsView, /:loading-shape="settingsLoadingShape"/)
  assert.match(listSkeleton, /variant\?: 'list' \| 'content'/)
  assert.match(listSkeleton, /import SkeletonBlock from '\.\/SkeletonBlock\.vue'/)
  assert.doesNotMatch(listSkeleton, /list-skeleton-reveal|opacity:\s*0/)
  assert.match(skeletonBlock, /animation: skeleton-block-shimmer/)
  assert.match(settingsSkeleton, /settings-skeleton__preferences/)
  assert.match(settingsSkeleton, /settings-skeleton__modules/)
  assert.match(settingsSkeleton, /settings-skeleton__master-detail/)
  assert.match(settingsSkeleton, /settings-skeleton__workbench/)
  assert.match(settingsSkeleton, /settings-skeleton__detail-form/)
  assert.match(settingsSkeleton, /settings-skeleton__diagnostics/)
  assert.doesNotMatch(settingsSkeleton, /settings-skeleton-reveal|opacity:\s*0/)
  assert.match(messages, /<ListSkeleton[\s\S]*threadsResource\.status === 'loading'/)
  const expectedShapes = [
    'preferences',
    'master-detail',
    'modules',
    'preferences',
    'master-detail',
    'workbench',
    'modules',
    'detail-form',
    'diagnostics',
    'modules'
  ]
  for (const [index, panel] of settingsPanels.entries()) {
    assert.match(panel, /import SettingsLoadBoundary from/)
    assert.match(panel, /<SettingsLoadBoundary/)
    assert.match(panel, new RegExp(`loading-shape="${expectedShapes[index]}"`))
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
  assert.doesNotMatch(
    masterDetail,
    /\.settings-master-detail \{[^}]*border-top:/
  )
  assert.match(
    masterDetail,
    /\.settings-master-detail__list \{[\s\S]*overflow-y: auto;/
  )
  assert.match(
    masterDetail,
    /\.settings-master-detail__detail-content \{[\s\S]*overflow-y: auto;[\s\S]*overscroll-behavior: contain;/
  )
  assert.match(
    masterDetail,
    /padding: 0 24px var\(--settings-page-end-gutter\);/
  )
  assert.match(
    masterDetail,
    /padding: 12px 16px[\s\S]*max\(var\(--settings-page-end-gutter\), env\(safe-area-inset-bottom\)\);/
  )
  assert.match(shell, /pairing: t\('settings\.iosApp'\)/)
  assert.match(shell, /connectivity: t\('settings\.tls'\)/)
  assert.match(shell, /about: t\('settings\.about'\)/)
})

test('master-detail resources use the template-owned search field', async () => {
  const [masterDetail, users, telegram] = await Promise.all([
    source('../src/components/settings/SettingsMasterDetail.vue'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue')
  ])

  assert.match(masterDetail, /searchQuery: string/)
  assert.match(masterDetail, /searchPlaceholder: string/)
  assert.match(masterDetail, /class="settings-master-detail__search"/)
  for (const resource of [users, telegram]) {
    assert.match(resource, /v-model:search-query="searchQuery"/)
    assert.doesNotMatch(resource, /#sidebar-toolbar|class="user-search"/)
  }
  assert.match(telegram, /v-for="unit in filteredTelegramUnits"/)
})

test('settings scroll owners share one page-end gutter', async () => {
  const [style, masterDetail, devices] = await Promise.all([
    source('../src/style.css'),
    source('../src/components/settings/SettingsMasterDetail.vue'),
    source('../src/components/DeviceConfigurationPanel.vue')
  ])

  assert.match(style, /--settings-page-end-gutter: 32px;/)
  assert.match(
    style,
    /\.settings-content \{\s*padding-bottom: var\(--settings-page-end-gutter\);\s*\}/
  )
  for (const template of [masterDetail, devices]) {
    assert.match(template, /var\(--settings-page-end-gutter\)/)
  }
  assert.doesNotMatch(style, /padding-bottom: 76px;/)
  assert.doesNotMatch(devices, /padding-bottom: (?:24|76)px;/)
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

  assert.match(
    deviceWorkspace,
    /\.device-workspace \{[\s\S]*display: block;[\s\S]*height: 100%;[\s\S]*min-height: 0;[\s\S]*overflow-y: auto;/
  )
  assert.doesNotMatch(
    deviceWorkspace,
    /\.device-workspace \{[^}]*grid-template-rows: auto auto;/
  )
  assert.doesNotMatch(
    deviceWorkspace,
    /\.device-workspace \{[^}]*border-top:/
  )
  assert.match(
    deviceWorkspace,
    /@media \(max-width: 860px\)[\s\S]*\.device-workspace \{[\s\S]*display: grid;[\s\S]*grid-template-rows: minmax\(0, 1fr\);[\s\S]*\.device-workspace__selector,[\s\S]*\.device-workspace__detail \{[\s\S]*overflow-y: auto;/
  )
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
  assert.match(
    view,
    /@media \(min-width: 861px\) and \(max-width: 1100px\)[\s\S]*\.settings-workspace\.has-selection > \.settings-list-pane,[\s\S]*display: none;/
  )
})

test('settings sections animate while resource and communication entities switch immediately', async () => {
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
  assert.doesNotMatch(masterDetail, /settings-master-detail-content-in|opacity:\s*0/)
  assert.doesNotMatch(masterDetail, /<Transition/)
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
    assert.doesNotMatch(immediate, /SettingsSaveStatus/)
  }
  assert.doesNotMatch(recording, /recording-settings__value/)
  assert.match(users, /function toggleLine/)
  assert.match(users, /const formChanged = computed/)
  assert.match(users, /v-if="creating \|\| selectedUser\?\.role === 'member'"/)
  assert.match(users, /t\('users\.saveUser'\)/)
  assert.doesNotMatch(users, /useSettingsMutation|lineSaving|lineSaveError/)
  assert.match(telegram, /const formChanged = computed/)
  assert.match(telegram, /t\('telegram\.saveBot'\)/)
  assert.doesNotMatch(telegram, /persistLineScopes|scopeSaving|scopeSaveError/)
  assert.doesNotMatch(users, /SettingsSaveStatus/)
  assert.doesNotMatch(telegram, /SettingsSaveStatus/)
})
