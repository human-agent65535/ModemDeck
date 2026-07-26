import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseContact } from '../src/api/normalize.ts'

const editor = new URL('../src/components/ContactEditor.vue', import.meta.url)
const avatarPicker = new URL('../src/components/ContactAvatarPicker.vue', import.meta.url)
const numberActions = new URL('../src/components/ContactNumberActions.vue', import.meta.url)
const contactsView = new URL('../src/views/ContactsView.vue', import.meta.url)
const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)
const recordingsView = new URL('../src/views/RecordingsView.vue', import.meta.url)

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

test('message conversations reuse contact create and add actions', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /import ContactNumberActions/)
  assert.match(source, /<ContactHeaderIdentity/)
  assert.match(source, /class="conversation-header__contact-actions"[\s\S]*<ContactNumberActions/)
  assert.match(source, /<ContactNumberActions[\s\S]*:number="selectedThread\.peer"/)
  assert.match(source, /:contact="activeContact"/)
  assert.match(source, /compact/)
  assert.match(source, /@saved="contactSaved"/)
  assert.doesNotMatch(source, /class="conversation-contact-actions"/)
})

test('recordings reuse the compact contact identity and actions in the header', async () => {
  const source = await readFile(recordingsView, 'utf8')

  assert.match(source, /<ContactHeaderIdentity/)
  assert.match(source, /<BaseAvatar :name="displayName\(recording\)" :src="avatar\(recording\)"/)
  assert.match(source, /class="recording-list-item__avatar"/)
  assert.match(source, /:number="selected\.call\.remote_number"/)
  assert.match(source, /class="recording-header__contact-actions"/)
  assert.match(source, /<ContactNumberActions[\s\S]*:contact="selectedContact"[\s\S]*compact/)
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

test('contacts expose avatar upload in full and quick create flows', async () => {
  const [editorSource, pickerSource, numberActionsSource, viewSource] = await Promise.all([
    readFile(editor, 'utf8'),
    readFile(avatarPicker, 'utf8'),
    readFile(numberActions, 'utf8'),
    readFile(contactsView, 'utf8')
  ])

  assert.match(editorSource, /v-model="draft\.favorite"[^>]*role="switch"/)
  assert.match(editorSource, /favorite: draft\.favorite/)
  assert.match(editorSource, /avatar: draft\.avatar/)
  assert.match(editorSource, /<ContactAvatarPicker[\s\S]*v-model="draft\.avatar"/)
  assert.match(pickerSource, /createContactAvatar\(file\)/)
  assert.match(pickerSource, /@drop\.prevent="dropFile"/)
  assert.match(pickerSource, /@click="openPicker"/)
  assert.match(pickerSource, /@click="removeAvatar"/)
  assert.match(numberActionsSource, /<ContactEditor/)
  assert.match(numberActionsSource, /:initial-phone="number"/)
  assert.match(numberActionsSource, /@save="createContact"/)
  assert.doesNotMatch(numberActionsSource, /createName|createAvatar|ContactAvatarPicker/)
  assert.match(viewSource, /async function toggleFavorite\(contact: Contact\)/)
  assert.match(viewSource, /favorite: !contact\.favorite/)
  assert.match(viewSource, /:aria-pressed="selected\.favorite"/)
  assert.match(viewSource, /v-if="contact\.favorite"/)
})
