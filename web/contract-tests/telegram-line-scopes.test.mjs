import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const form = readFileSync(
  new URL('../src/components/TelegramSettingsForm.vue', import.meta.url),
  'utf8'
)
const scopeList = readFileSync(
  new URL('../src/components/settings/SettingsLineScopeList.vue', import.meta.url),
  'utf8'
)
const lineIdentity = readFileSync(
  new URL('../src/components/LineIdentity.vue', import.meta.url),
  'utf8'
)

function functionBody(name, nextName) {
  const start = form.indexOf(`function ${name}`)
  const end = form.indexOf(`function ${nextName}`, start)
  assert.ok(start >= 0, `${name} must exist`)
  assert.ok(end > start, `${name} must precede ${nextName}`)
  return form.slice(start, end)
}

test('Telegram all-lines selection clears every concrete line scope', () => {
  const body = functionBody('setAllLines', 'selectAllLines')

  assert.match(body, /allLines\.value = true/)
  assert.match(body, /lineScopes\.value = \[\]/)
  assert.match(form, /@select-all="selectAllLines"/)
  assert.match(scopeList, /@click\.prevent="emit\('selectAll'\)"/)
})

test('Telegram concrete line selection is exclusive with all-lines', () => {
  const body = functionBody('toggleLineScope', 'normalizedLineScopes')
  const scopeFieldset = form.slice(
    form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'),
    form.indexOf('</fieldset>', form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'))
  )

  assert.match(body, /lineScopes\.value = scopes/)
  assert.match(body, /allLines\.value = scopes\.length === 0/)
  assert.match(scopeFieldset, /:selected-ids="lineScopes"/)
  assert.match(scopeFieldset, /@toggle-line="toggleLineScope"/)
  assert.match(scopeList, /:checked="selectedIds\.includes\(option\.id\)"/)
  assert.match(scopeList, /@change="onLineChange\(option\.id, \$event\)"/)
  assert.doesNotMatch(scopeFieldset, /:disabled="allLines"/)
})

test('Telegram save payload has one canonical line-scope representation', () => {
  const body = functionBody('normalizedLineScopes', 'selectUnit')

  assert.match(body, /if \(allLines\.value \|\| scopes\.length === 0\)/)
  assert.match(body, /setAllLines\(\)[\s\S]*return \[\]/)
  assert.match(body, /allLines\.value = false[\s\S]*return scopes/)
  assert.match(form, /line_scopes: normalizedLineScopes\(\)/)
  assert.doesNotMatch(form, /line_scopes: allLines\.value \? \[\]/)
})

test('Telegram line scopes remain local draft state until the bot is saved', () => {
  const selectAll = functionBody('selectAllLines', 'toggleLineScope')
  const toggle = functionBody('toggleLineScope', 'normalizedLineScopes')

  assert.match(selectAll, /setAllLines\(\)/)
  assert.match(toggle, /lineScopes\.value = scopes/)
  assert.doesNotMatch(selectAll, /saveTelegramUnit|persistLineScopes/)
  assert.doesNotMatch(toggle, /saveTelegramUnit|persistLineScopes/)
  assert.doesNotMatch(form, /useSettingsMutation|scopeSaving|scopeSaveError/)
  assert.match(form, /t\('telegram\.saveBot'\)/)
})

test('Telegram line scopes show the line label and reliable phone number without internal IDs', () => {
  const optionsStart = form.indexOf('const scopeOptions')
  const optionsEnd = form.indexOf('function scopedLine', optionsStart)
  assert.ok(optionsStart >= 0)
  assert.ok(optionsEnd > optionsStart)
  const options = form.slice(optionsStart, optionsEnd)

  assert.match(options, /label: lineLabel\(line\)/)
  assert.match(options, /phoneNumber: line\.phone_number\.trim\(\)/)
  assert.match(options, /\bline\b/)
  assert.match(options, /label: t\('telegram\.unknownLine'\)/)
  assert.match(options, /phoneNumber: ''/)
  assert.doesNotMatch(form, /unknownLine.*\$\{scope\}/)

  const scopeFieldset = form.slice(
    form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'),
    form.indexOf('</fieldset>', form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'))
  )
  assert.match(scopeFieldset, /<SettingsLineScopeList/)
  assert.match(scopeFieldset, /:options="lineScopeOptions"/)
  assert.match(scopeList, /<LineIdentity/)
  assert.match(lineIdentity, /<ListFilter v-if="all"/)
  assert.match(lineIdentity, /<CardSim v-else/)
  assert.match(options, /details: option\.phoneNumber \|\| t\('lines\.cellularLine'\)/)
  assert.match(lineIdentity, /const tone = lineTone\(props\.line\)/)
})
