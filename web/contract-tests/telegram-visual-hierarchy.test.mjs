import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const form = readFileSync(
  new URL('../src/components/TelegramSettingsForm.vue', import.meta.url),
  'utf8'
)

test('Telegram bot list exposes channel identity, status, and line scope', () => {
  const listStart = form.indexOf('<aside class="telegram-unit-list"')
  const listEnd = form.indexOf('</aside>', listStart)
  const list = form.slice(listStart, listEnd)

  assert.match(list, /telegram-unit-list__title[\s\S]*t\('telegram\.notifications'\)/)
  assert.match(list, /<small>Telegram<\/small>/)
  assert.match(list, /telegram-unit-row__icon[\s\S]*<Send/)
  assert.match(list, /unit\.enabled \? t\('lines\.enabled'\) : t\('lines\.disabled'\)/)
  assert.match(list, /unitScopeSummary\(unit\)/)
  assert.match(list, /<ListFilter v-if="unit\.line_scopes\.length === 0"/)
  assert.match(list, /<CardSim v-else/)
  assert.doesNotMatch(list, /telegram-unit-row__status/)
})

test('Telegram editor uses distinct identity, event, and line sections', () => {
  assert.match(
    form,
    /telegram-form-heading__copy[\s\S]*t\('telegram\.notifications'\)[\s\S]*<small>Telegram Bot<\/small>/
  )
  assert.match(
    form,
    /telegram-bot-identity[\s\S]*<KeyRound[\s\S]*t\('telegram\.botSettings'\)/
  )
  assert.match(
    form,
    /telegram-event-options[\s\S]*<MessageSquareText[\s\S]*<PhoneMissed/
  )
  assert.match(
    form,
    /telegram-scope-options[\s\S]*telegram-scope-option__icon[\s\S]*<LineTag/
  )
})

test('Telegram component owns responsive, overflow-safe layout styles', () => {
  const styleStart = form.indexOf('<style scoped>')
  const styleEnd = form.indexOf('</style>', styleStart)
  const style = form.slice(styleStart, styleEnd)

  assert.match(
    style,
    /\.telegram-settings-container\s*\{[\s\S]*container-type: inline-size/
  )
  assert.match(style, /\.telegram-settings\s*\{[\s\S]*grid-template-columns: 280px minmax\(0, 1fr\)/)
  assert.match(style, /\.telegram-unit-row__copy\s*\{[\s\S]*min-width: 0/)
  assert.match(style, /text-overflow: ellipsis/)
  assert.match(
    style,
    /@media \(max-width: 720px\)[\s\S]*grid-template-columns: minmax\(0, 1fr\)/
  )
  assert.match(
    style,
    /\.telegram-event-options,[\s\S]*\.telegram-scope-options[\s\S]*grid-template-columns: minmax\(0, 1fr\)/
  )
  assert.match(
    style,
    /@container \(max-width: 700px\)[\s\S]*grid-template-columns: minmax\(0, 1fr\)/
  )
})
