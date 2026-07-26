import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import {
  MAX_DIAL_TARGET_DIGITS,
  MAX_DIAL_TARGET_RUNES,
  normalizeDialTarget
} from '../src/utils/dialTarget.ts'

const dialer = await readFile(
  new URL('../src/components/DialerPanel.vue', import.meta.url),
  'utf8'
)
const suggest = await readFile(
  new URL('../src/components/ContactSuggestInput.vue', import.meta.url),
  'utf8'
)

test('dial targets preserve local and short numbers without guessing a country', () => {
  const cases = new Map([
    ['090-1234-5678', '09012345678'],
    ['110', '110'],
    ['+81 (80) 1234-5678', '+818012345678'],
    ['+49 30/1234.5678', '+493012345678']
  ])

  for (const [input, expected] of cases) {
    assert.deepEqual(normalizeDialTarget(input), {
      original: input,
      normalized: expected,
      error: ''
    })
  }
})

test('dial targets reject unsafe syntax but leave number existence to the network', () => {
  const cases = new Map([
    ['', 'required'],
    ['Aiko', 'invalid_character'],
    ['*123#', 'invalid_character'],
    ['81+8012345678', 'invalid_character'],
    ['+()', 'invalid_length'],
    ['1'.repeat(MAX_DIAL_TARGET_DIGITS + 1), 'invalid_length'],
    ['1'.repeat(MAX_DIAL_TARGET_RUNES + 1), 'too_long']
  ])

  for (const [input, expected] of cases) {
    assert.equal(normalizeDialTarget(input).error, expected)
  }
})

test('fixture applies the same local dial-target contract as the real gateway', async () => {
  const fixture = createFixtureGateway()
  const call = await fixture.startCall('line-fixture-main', '090-1234-5678')
  assert.equal(call.remote_number, '09012345678')

  await assert.rejects(
    createFixtureGateway().startCall('line-fixture-main', '*123#'),
    error =>
      error instanceof Error &&
      error.message === 'Dial target is invalid'
  )
})

test('dialer shows validation inside the input without shifting the keypad', () => {
  assert.match(dialer, /import \{ normalizeDialTarget \} from/)
  assert.match(dialer, /const validationVisible = computed/)
  assert.match(dialer, /class="dialer-number-trailing"/)
  assert.match(dialer, /class="dialer-inline-validation"/)
  assert.match(dialer, /t\('dialer\.invalidShort'\)/)
  assert.match(dialer, /:invalid="validationVisible"/)
  assert.match(dialer, /:described-by="validationVisible \? validationMessageId : ''"/)
  assert.match(dialer, /@focus="focusNumberInput"/)
  assert.match(dialer, /@blur="blurNumberInput"/)
  assert.match(
    dialer,
    /\.dialer-number-trailing\s*\{[\s\S]*?position: absolute[\s\S]*?right: 7px/
  )
  assert.doesNotMatch(
    dialer,
    /v-else-if="validationError" class="field-error"/
  )
  assert.doesNotMatch(dialer, /\^\\\+\?/)

  assert.match(suggest, /:aria-invalid="invalid \|\| undefined"/)
  assert.match(suggest, /:aria-describedby="describedBy \|\| undefined"/)
  assert.match(suggest, /emit\('focus', event\)/)
  assert.match(suggest, /emit\('blur', event\)/)
})
