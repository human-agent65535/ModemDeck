import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseContact } from '../src/api/normalize.ts'

const editor = new URL('../src/components/ContactEditor.vue', import.meta.url)
const contactsView = new URL('../src/views/ContactsView.vue', import.meta.url)

test('contact parsing requires a real favorite value', () => {
  const contact = parseContact({
    id: 'contact-1',
    display_name: 'Aiko',
    avatar: 'data:image/png;base64,iVBORw0KGgo=',
    favorite: true,
    phones: [
      {
        id: 'phone-1',
        label: 'mobile',
        original_number: '+1 202 555 0103',
        canonical_e164: '+819012345678',
        primary: true
      }
    ]
  })

  assert.equal(contact.favorite, true)
  assert.equal(contact.avatar, 'data:image/png;base64,iVBORw0KGgo=')
  assert.throws(
    () =>
      parseContact({
        id: 'contact-2',
        display_name: 'Minh',
        phones: []
      }),
    /favorite/
  )
})

test('fixture persists favorite and preferred line through contact writes', async () => {
  const gateway = createFixtureGateway()
  const created = await gateway.createContact?.({
    display_name: 'Favorite',
    avatar: 'data:image/png;base64,iVBORw0KGgo=',
    favorite: true,
    preferred_device_imei: 'fixture-001',
    phones: [{ label: 'mobile', number: '+81 80 9999 0000', primary: true }]
  })

  assert.equal(created?.favorite, true)
  assert.equal(created?.avatar, 'data:image/png;base64,iVBORw0KGgo=')
  assert.equal(created?.preferred_device_imei, 'fixture-001')

  const updated = await gateway.updateContact?.(created?.id || '', {
    display_name: 'Favorite',
    avatar: created?.avatar,
    favorite: false,
    preferred_device_imei: 'fixture-002',
    revision: created?.revision,
    phones:
      created?.phones.map(phone => ({
        id: phone.id,
        label: phone.label,
        number: phone.number,
        primary: phone.primary
      })) || []
  })

  assert.equal(updated?.favorite, false)
  assert.equal(updated?.avatar, created?.avatar)
  assert.equal(updated?.preferred_device_imei, 'fixture-002')
})

test('contacts expose editor and quick favorite controls', async () => {
  const [editorSource, viewSource] = await Promise.all([
    readFile(editor, 'utf8'),
    readFile(contactsView, 'utf8')
  ])

  assert.match(editorSource, /v-model="draft\.favorite"[^>]*role="switch"/)
  assert.match(editorSource, /favorite: draft\.favorite/)
  assert.match(editorSource, /createContactAvatar\(file\)/)
  assert.match(editorSource, /avatar: draft\.avatar/)
  assert.match(viewSource, /async function toggleFavorite\(contact: Contact\)/)
  assert.match(viewSource, /favorite: !contact\.favorite/)
  assert.match(viewSource, /:aria-pressed="selected\.favorite"/)
  assert.match(viewSource, /v-if="contact\.favorite"/)
})
