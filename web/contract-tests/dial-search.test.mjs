import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { dialSuggestions, nameSearchKeys } from '../src/utils/dialSearch.ts'

const contacts = [
  { display_name: '示例联系人', phones: [{ number: '+1 202 555 0101' }] },
  { display_name: 'John Smith', phones: [{ number: '+1 202 555 0177' }, { number: '+44 20 7946 0000' }] },
  { display_name: 'Zoë Ödegaard', phones: [{ number: '+47 22 00 00 00' }] }
]

const names = (value, options = {}) =>
  dialSuggestions(contacts, value, {
    spellsNames: true,
    minimumNumberDigits: 3,
    limit: 20,
    ...options
  }).map(match => `${match.contact.display_name} ${match.phone.number}`)

test('Chinese names are searched as pinyin syllables and initials', () => {
  assert.deepEqual(nameSearchKeys('示例联系人').initials, 'sllxr')
  for (const query of ['示例', 'shili', 'lianxi', 'sl', 'sllxr', 'lxr', 'ShiLi']) {
    assert.deepEqual(names(query), ['示例联系人 +1 202 555 0101'], query)
  }
  assert.deepEqual(names('xyz'), [])
})

test('keypad digits spell names with the letters on the keys', () => {
  // s=7, l=5 (initials); 5646 spells "john"; 76484 spells "smith".
  assert.deepEqual(names('75'), ['示例联系人 +1 202 555 0101'])
  assert.deepEqual(names('5646'), ['John Smith +1 202 555 0177', 'John Smith +44 20 7946 0000'])
  assert.deepEqual(names('76484').length, 2)
  // Diacritics fold onto the plain letter: "zoe" is 963.
  assert.deepEqual(names('963'), ['Zoë Ödegaard +47 22 00 00 00'])
  assert.deepEqual(names('zoe'), ['Zoë Ödegaard +47 22 00 00 00'])
})

test('number matches follow name matches and keep their digit threshold', () => {
  assert.deepEqual(names('0177'), ['John Smith +1 202 555 0177'])
  assert.deepEqual(names('20'), [])
  // Outside a dial pad digits never spell names, and one digit is enough for a number.
  assert.deepEqual(names('5646', { spellsNames: false, minimumNumberDigits: 1 }), [])
  assert.equal(names('9', { spellsNames: false, minimumNumberDigits: 1 }).length, 1)
  // A service code is dialled, not spelled.
  assert.deepEqual(names('*75#'), [])
})

test('the dialer opts in to spelled names and keeps the touch keyboard behind a toggle', async () => {
  const dialer = await readFile(new URL('../src/components/DialerPanel.vue', import.meta.url), 'utf8')
  assert.match(dialer, /spells-names\s*:input-mode="numberInputMode"/)
  assert.match(dialer, /touchInput\.value && !systemKeyboard\.value \? 'none' : 'text'/)
  assert.match(dialer, /@mousedown\.prevent\s*@click="handleKeyClick/)
})
