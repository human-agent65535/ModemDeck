import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const form = readFileSync(
  new URL('../src/components/TelegramSettingsForm.vue', import.meta.url),
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
  const body = functionBody('selectAllLines', 'toggleLineScope')

  assert.match(body, /allLines\.value = true/)
  assert.match(body, /lineScopes\.value = \[\]/)
  assert.match(form, /@click\.prevent="selectAllLines"/)
})

test('Telegram concrete line selection is exclusive with all-lines', () => {
  const body = functionBody('toggleLineScope', 'normalizedLineScopes')
  const scopeFieldset = form.slice(
    form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'),
    form.indexOf('</fieldset>', form.indexOf('<fieldset class="telegram-options telegram-line-scopes">'))
  )

  assert.match(body, /lineScopes\.value = scopes/)
  assert.match(body, /allLines\.value = scopes\.length === 0/)
  assert.match(scopeFieldset, /:checked="lineScopes\.includes\(line\.id\)"/)
  assert.match(scopeFieldset, /@change="toggleLineScope\(line\.id, \$event\)"/)
  assert.doesNotMatch(scopeFieldset, /:disabled="allLines"/)
})

test('Telegram save payload has one canonical line-scope representation', () => {
  const body = functionBody('normalizedLineScopes', 'selectUnit')

  assert.match(body, /if \(allLines\.value \|\| scopes\.length === 0\)/)
  assert.match(body, /selectAllLines\(\)[\s\S]*return \[\]/)
  assert.match(body, /allLines\.value = false[\s\S]*return scopes/)
  assert.match(form, /line_scopes: normalizedLineScopes\(\)/)
  assert.doesNotMatch(form, /line_scopes: allLines\.value \? \[\]/)
})

test('Telegram line scopes show the alias and reliable phone number without internal IDs', () => {
  const identityStart = form.indexOf('function telegramLineIdentity')
  const identityEnd = form.indexOf('const scopeOptions', identityStart)
  assert.ok(identityStart >= 0)
  assert.ok(identityEnd > identityStart)
  const identity = form.slice(identityStart, identityEnd)

  assert.match(identity, /const alias = lineLabel\(line\)/)
  assert.match(identity, /const phoneNumber = line\.phone_number\.trim\(\)/)
  assert.match(identity, /return phoneNumber \? `\$\{alias\} · \$\{phoneNumber\}` : alias/)
  assert.match(form, /label: telegramLineIdentity\(line\)/)
  assert.match(form, /options\.push\(\{ id: scope, label: t\('telegram\.unknownLine'\) \}\)/)
  assert.doesNotMatch(form, /unknownLine.*\$\{scope\}/)
})
