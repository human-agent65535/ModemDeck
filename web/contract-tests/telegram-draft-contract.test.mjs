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

test('new Telegram Bot is represented by one selected local draft row', () => {
  const listStart = form.indexOf('<aside class="telegram-unit-list"')
  const listEnd = form.indexOf('</aside>', listStart)
  const list = form.slice(listStart, listEnd)

  assert.match(list, /v-if="creating"[\s\S]*class="telegram-unit-row is-selected"/)
  assert.match(list, /displayName\.trim\(\) \|\| t\('telegram\.unnamed'\)/)
  assert.match(
    list,
    /class="telegram-unit-row__state is-draft"[\s\S]*t\('telegram\.unsaved'\)/
  )
  assert.match(list, /telegram-unit-row__icon is-enabled[\s\S]*<Send/)
  assert.match(list, /<ListFilter[\s\S]*t\('telegram\.allLines'\)/)
  assert.equal((list.match(/t\('telegram\.unnamed'\)/g) || []).length, 1)
  assert.match(list, /telegramResource\.data\.length === 0 && !creating/)
})

test('starting again resets the same draft and switching Bot discards it', () => {
  const select = functionBody('selectUnit', 'startCreate')
  const start = functionBody('startCreate', 'discardDraft')
  const discard = functionBody('discardDraft', 'cancelCreate')

  assert.match(select, /discardDraft\(id\)/)
  assert.match(start, /if \(!creating\.value\)/)
  assert.match(start, /draftReturnID\.value = selectedUnit\.value\?\.id/)
  assert.match(start, /creating\.value = true/)
  assert.match(start, /selectedID\.value = '__telegram-bot-draft__'/)
  assert.match(start, /applyUnit\(\)/)
  assert.match(discard, /creating\.value = false/)
  assert.match(discard, /selectedID\.value = returnID/)
  assert.match(discard, /applyUnit\(telegramResource\.data\.find/)
})

test('new Bot has a local cancel action and never uses backend deletion', () => {
  const cancel = functionBody('cancelCreate', 'submit')
  const footerStart = form.indexOf('<footer class="settings-form-actions">')
  const footerEnd = form.indexOf('</footer>', footerStart)
  const footer = form.slice(footerStart, footerEnd)

  assert.match(cancel, /discardDraft\(\)/)
  assert.doesNotMatch(cancel, /deleteTelegramUnit/)
  assert.match(
    footer,
    /v-if="creating"[\s\S]*@click="cancelCreate"[\s\S]*<span>\{\{ t\('common\.cancel'\) \}\}<\/span>/
  )
  assert.match(footer, /v-else-if="selectedUnit"[\s\S]*@click="remove"/)
})

test('successful create replaces the draft with the persisted Bot', () => {
  const submit = functionBody('submit', 'remove')

  assert.match(submit, /const unit = await saveTelegramUnit/)
  assert.match(submit, /creating\.value = false/)
  assert.match(submit, /draftReturnID\.value = ''/)
  assert.match(submit, /selectedID\.value = unit\.id/)
  assert.match(submit, /applyUnit\(unit\)/)
})
