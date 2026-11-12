import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  contactImportFailureStopsImport,
  parseVCard,
  planContactImport,
  previewContactImport,
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
    'FN:Alex Rowan',
    'TEL;TYPE=CELL:(202) 555-0103',
    'NOTE:Demo contact\\nPrimary contact',
    'PHOTO;ENCODING=b;TYPE=PNG:iVBORw0KGgo=',
    'END:VCARD'
  ].join('\r\n')

  const contacts = parseVCard(source, 'US')
  assert.equal(contacts.length, 1)
  assert.equal(contacts[0]?.display_name, 'Alex Rowan')
  assert.equal(contacts[0]?.phones[0]?.number, '+12025550103')
  assert.equal(contacts[0]?.notes, 'Demo contact\nPrimary contact')
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
      normalized_number: '+12025550103',
      region: 'US',
      primary: true
    }]
  }])
  assert.match(exported, /FN:Alex Rowan/)
  assert.match(exported, /TEL;TYPE=CELL:\+12025550103/)
  assert.match(exported, /PHOTO;ENCODING=b;TYPE=PNG:iVBORw0KGgo=/)
})

test('vCard transfer falls back to the structured Apple contact name', () => {
  const contacts = parseVCard([
    'BEGIN:VCARD',
    'VERSION:3.0',
    'N:Rowan;Alex;;;',
    'TEL;TYPE=CELL:+12025550103',
    'END:VCARD'
  ].join('\r\n'))

  assert.equal(contacts[0]?.display_name, 'Alex Rowan')
})

test('imports update a single phone owner and preserve ModemDeck-only preferences', () => {
  const existing = {
    id: 'contact-1',
    display_name: 'Alex',
    avatar: 'data:image/png;base64,AAAA',
    favorite: true,
    notes: 'Local note',
    preferred_line_id: 'line-main',
    revision: 4,
    phones: [{
      id: 'phone-1',
      label: 'Mobile',
      number: '(202) 555-0103',
      normalized_number: '+12025550103',
      region: 'US',
      primary: true
    }]
  }
  const plan = planContactImport({
    display_name: 'Alex Rowan',
    phones: [
      { label: 'Mobile', number: '+12025550103' },
      { label: 'Work', number: '+12025550105' }
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
      canonicalForm: '+12025550103',
      value: '(202) 555-0103',
      formattedType: 'Mobile'
    }]
  }, 'US')
  assert.equal(contact?.display_name, 'Alex Rowan')
  assert.equal(contact?.phones[0]?.number, '+12025550103')
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

test('an import is counted before anything is written and waits for confirmation', async () => {
  const existing = {
    id: 'contact-1',
    display_name: 'Alex',
    favorite: false,
    revision: 1,
    phones: [{ id: 'phone-1', label: 'Mobile', number: '+12025550103', normalized_number: '+12025550103', region: 'US', primary: true }]
  }
  const other = {
    id: 'contact-2',
    display_name: 'Sam',
    favorite: false,
    revision: 1,
    phones: [{ id: 'phone-2', label: 'Mobile', number: '+12025550104', normalized_number: '+12025550104', region: 'US', primary: true }]
  }
  const preview = previewContactImport([
    { display_name: 'Alex Rowan', phones: [{ label: 'Mobile', number: '+12025550103' }] },
    { display_name: 'New Person', phones: [{ label: 'Mobile', number: '+12025550109' }] },
    { display_name: 'Shared', phones: [{ label: 'A', number: '+12025550103' }, { label: 'B', number: '+12025550104' }] },
    { display_name: 'No phone', phones: [] }
  ], [existing, other])
  assert.deepEqual(preview, { created: 1, updated: 1, skipped: 2 })

  const settings = await readFile(new URL('../src/components/ContactSyncSettings.vue', import.meta.url), 'utf8')
  const importBody = settings.slice(settings.indexOf('async function importContacts'))
  const confirmation = importBody.indexOf('requestConfirmation(')
  const write = importBody.indexOf('saveContact(')
  assert.ok(confirmation > 0 && write > confirmation, 'the confirmation must come before the first write')
  assert.match(importBody, /if \(!confirmed\) return null/)
})

test('an import stops on a failure that would repeat for every contact', async () => {
  const apiError = status => Object.assign(new Error('failed'), { status })
  for (const status of [400, 404, 409, 422]) {
    assert.equal(contactImportFailureStopsImport(apiError(status)), false, `HTTP ${status} is about one contact`)
  }
  for (const error of [apiError(0), apiError(401), apiError(403), apiError(429), apiError(500), apiError(503), new Error('offline'), null]) {
    assert.equal(contactImportFailureStopsImport(error), true)
  }

  const settings = await readFile(new URL('../src/components/ContactSyncSettings.vue', import.meta.url), 'utf8')
  assert.match(settings, /if \(contactImportFailureStopsImport\(error\)\) \{[\s\S]*?unprocessed: pending\.length - position/)
  assert.match(settings, /if \(result\.unprocessed > 0\) \{[\s\S]*?error\.value = t\('contactSync\.importStopped'/)
})
