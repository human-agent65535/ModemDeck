import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  parseVCard,
  planContactImport,
  serializeContactsToVCard
} from '../src/utils/contactTransfer.ts'
import {
  GOOGLE_CONTACTS_SCOPE,
  transferContactFromGooglePerson,
  validGoogleClientID
} from '../src/utils/googleContacts.ts'

test('vCard transfer preserves contact identity, phones, notes, and embedded avatars', () => {
  const avatar = 'data:image/png;base64,iVBORw0KGgo='
  const source = [
    'BEGIN:VCARD',
    'VERSION:3.0',
    'FN:田中 愛子',
    'TEL;TYPE=CELL:090-1234-5678',
    'NOTE:Tokyo\\nPrimary contact',
    'PHOTO;ENCODING=b;TYPE=PNG:iVBORw0KGgo=',
    'END:VCARD'
  ].join('\r\n')

  const contacts = parseVCard(source, 'JP')
  assert.equal(contacts.length, 1)
  assert.equal(contacts[0]?.display_name, '田中 愛子')
  assert.equal(contacts[0]?.phones[0]?.number, '+819012345678')
  assert.equal(contacts[0]?.notes, 'Tokyo\nPrimary contact')
  assert.equal(contacts[0]?.avatar, avatar)

  const exported = serializeContactsToVCard([{
    id: 'contact-1',
    display_name: contacts[0].display_name,
    avatar,
    favorite: false,
    notes: contacts[0].notes,
    phones: [{
      id: 'phone-1',
      label: 'Mobile',
      number: '+1 202 555 0103',
      normalized_number: '+819012345678',
      region: 'JP',
      primary: true
    }]
  }])
  assert.match(exported, /FN:田中 愛子/)
  assert.match(exported, /TEL;TYPE=CELL:\+819012345678/)
  assert.match(exported, /PHOTO;ENCODING=b;TYPE=PNG:iVBORw0KGgo=/)
})

test('vCard transfer falls back to the structured Apple contact name', () => {
  const contacts = parseVCard([
    'BEGIN:VCARD',
    'VERSION:3.0',
    'N:Tanaka;Aiko;;;',
    'TEL;TYPE=CELL:+819012345678',
    'END:VCARD'
  ].join('\r\n'))

  assert.equal(contacts[0]?.display_name, 'Alex Rowan')
})

test('imports update a single phone owner and preserve ModemDeck-only preferences', () => {
  const existing = {
    id: 'contact-1',
    display_name: 'Aiko',
    avatar: 'data:image/png;base64,AAAA',
    favorite: true,
    notes: 'Local note',
    preferred_line_id: 'line-main',
    revision: 4,
    phones: [{
      id: 'phone-1',
      label: 'Mobile',
      number: '090-1234-5678',
      normalized_number: '+819012345678',
      region: 'JP',
      primary: true
    }]
  }
  const plan = planContactImport({
    display_name: 'Alex Rowan',
    phones: [
      { label: 'Mobile', number: '+819012345678' },
      { label: 'Work', number: '+81312345678' }
    ]
  }, [existing])

  assert.equal(plan.conflict, false)
  assert.equal(plan.existing?.id, 'contact-1')
  assert.equal(plan.input?.display_name, 'Alex Rowan')
  assert.equal(plan.input?.favorite, true)
  assert.equal(plan.input?.preferred_line_id, 'line-main')
  assert.equal(plan.input?.revision, 4)
  assert.equal(plan.input?.phones.length, 2)
})

test('Google contact conversion uses read-only scope and canonical phone numbers', () => {
  assert.equal(
    GOOGLE_CONTACTS_SCOPE,
    'https://www.googleapis.com/auth/contacts.readonly'
  )
  assert.equal(
    validGoogleClientID('123456789-example.apps.googleusercontent.com'),
    true
  )
  const contact = transferContactFromGooglePerson({
    names: [{ displayName: 'Alex Rowan' }],
    phoneNumbers: [{
      canonicalForm: '+819012345678',
      value: '090-1234-5678',
      formattedType: 'Mobile'
    }]
  }, 'JP')
  assert.equal(contact?.display_name, 'Alex Rowan')
  assert.equal(contact?.phones[0]?.number, '+819012345678')
})

test('contact transfer exists only in settings, not on the contacts page', async () => {
  const [settings, panel, contacts] = await Promise.all([
    readFile(new URL('../src/views/SettingsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/ContactSyncSettings.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/ContactsView.vue', import.meta.url), 'utf8')
  ])

  assert.match(settings, /id: 'contacts'/)
  assert.match(settings, /<ContactSyncSettings/)
  assert.match(panel, /requestGoogleContactsToken/)
  assert.match(panel, /parseVCard/)
  assert.match(panel, /serializeContactsToVCard/)
  assert.doesNotMatch(contacts, /ContactSyncSettings|parseVCard|importVCard|exportVCard/)
})
