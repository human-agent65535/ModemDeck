import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const form = readFileSync(
  new URL('../src/components/TelegramSettingsForm.vue', import.meta.url),
  'utf8'
)
const masterDetail = readFileSync(
  new URL('../src/components/settings/SettingsMasterDetail.vue', import.meta.url),
  'utf8'
)
const lineScopeList = readFileSync(
  new URL('../src/components/settings/SettingsLineScopeList.vue', import.meta.url),
  'utf8'
)
const resourceStatus = readFileSync(
  new URL('../src/components/settings/SettingsResourceStatus.vue', import.meta.url),
  'utf8'
)

test('Telegram bot list exposes channel identity, status, and line scope', () => {
  const listStart = form.indexOf('<template #sidebar>')
  const listEnd = form.indexOf('</template>', listStart)
  const list = form.slice(listStart, listEnd)

  assert.match(form, /:sidebar-title="t\('telegram\.bots'\)"/)
  assert.match(form, /:search-placeholder="t\('telegram\.searchBots'\)"/)
  assert.match(
    form,
    /:sidebar-description="t\('telegram\.count', \{ count: telegramResource\.data\.length \}\)"/
  )
  assert.match(list, /telegram-unit-row__icon[\s\S]*<Send/)
  assert.match(
    list,
    /<SettingsResourceStatus[\s\S]*:disabled="!unit\.effective_enabled"[\s\S]*:label="t\('lines\.disabled'\)"/
  )
  assert.match(resourceStatus, /v-if="disabled"/)
  assert.match(resourceStatus, /<CircleOff/)
  assert.doesNotMatch(resourceStatus, /CircleCheck/)
  assert.match(list, /unitScopeSummary\(unit\)/)
  assert.match(list, /<UsersRound :size="13"/)
  assert.doesNotMatch(list, /unit\.scope_source|<CardSim v-else/)
  assert.doesNotMatch(list, /telegram-unit-row__status/)
})

test('Telegram editor uses distinct identity, owner, event, and line sections', () => {
  assert.match(
    form,
    /telegram-form-heading__copy[\s\S]*displayName\.trim\(\)[\s\S]*<small>Telegram Bot<\/small>/
  )
  assert.match(
    form,
    /telegram-bot-identity[\s\S]*<KeyRound[\s\S]*t\('telegram\.botSettings'\)/
  )
  assert.match(
    form,
    /telegram-access-source[\s\S]*t\('telegram\.botOwner'\)[\s\S]*availableUsers/
  )
  assert.doesNotMatch(form, /t\('telegram\.manualScope'\)|scopeSource/)
  assert.match(
    form,
    /telegram-event-options[\s\S]*<MessageSquareText[\s\S]*<PhoneMissed/
  )
  assert.match(
    form,
    /<SettingsLineScopeList[\s\S]*:options="lineScopeOptions"/
  )
})

test('Telegram uses the shared resource rail and owns only resource-specific layout', () => {
  const styleStart = form.indexOf('<style scoped>')
  const styleEnd = form.indexOf('</style>', styleStart)
  const style = form.slice(styleStart, styleEnd)

  assert.match(form, /<SettingsMasterDetail[\s\S]*:detail-open="mobileDetailOpen"/)
  assert.match(form, /class="settings-resource-row telegram-unit-row/)
  assert.doesNotMatch(style, /--settings-master-sidebar|max-width:\s*1120px/)
  assert.match(
    masterDetail,
    /grid-template-columns: var\(--settings-master-sidebar\) minmax\(0, 1fr\)/
  )
  assert.match(masterDetail, /--settings-master-sidebar: clamp\(240px, 25%, 280px\)/)
  assert.match(masterDetail, /settings-master-detail__sidebar-header/)
  assert.match(masterDetail, /settings-resource-row\.is-selected/)
  assert.match(style, /\.telegram-unit-row__copy\s*\{[\s\S]*min-width: 0/)
  assert.match(style, /text-overflow: ellipsis/)
  assert.match(
    masterDetail,
    /@media \(max-width: 860px\)[\s\S]*grid-template-columns: minmax\(0, 1fr\)/
  )
  assert.match(style, /\.telegram-event-options[\s\S]*grid-template-columns: minmax\(0, 1fr\)/)
  assert.match(
    lineScopeList,
    /@container \(max-width: 700px\)[\s\S]*grid-template-columns: minmax\(0, 1fr\)/
  )
})
