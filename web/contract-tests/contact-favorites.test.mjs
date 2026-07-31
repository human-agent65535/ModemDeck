import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseContact } from '../src/api/normalize.ts'
import {
  formatPhoneNumber,
  phoneDestination,
  primaryPhoneDestination
} from '../src/utils/format.ts'
import {
  filterPhoneRegionOptions,
  phoneRegionOptions
} from '../src/utils/phoneRegions.ts'
import {
  bootstrapResource,
  contactForNumber,
  contactsResource,
  displayPhoneNumber,
  resolveLine
} from '../src/state/workspace.ts'

const editor = new URL('../src/components/ContactEditor.vue', import.meta.url)
const countrySelector = new URL('../src/components/CountryRegionSelector.vue', import.meta.url)
const avatarPicker = new URL('../src/components/ContactAvatarPicker.vue', import.meta.url)
const numberActions = new URL('../src/components/ContactNumberActions.vue', import.meta.url)
const contactSuggest = new URL('../src/components/ContactSuggestInput.vue', import.meta.url)
const dialer = new URL('../src/components/DialerPanel.vue', import.meta.url)
const callSurface = new URL('../src/components/CallSurface.vue', import.meta.url)
const contactsView = new URL('../src/views/ContactsView.vue', import.meta.url)
const dashboardView = new URL('../src/views/DashboardView.vue', import.meta.url)
const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)
const recordingsView = new URL('../src/views/RecordingsView.vue', import.meta.url)
const globalStyles = new URL('../src/style.css', import.meta.url)

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
        original_number: '090-1234-5678',
        canonical_e164: '+819012345678',
        region: 'JP',
        primary: true
      }
    ]
  })

  assert.equal(contact.favorite, true)
  assert.equal(contact.avatar, 'data:image/png;base64,iVBORw0KGgo=')
  assert.equal(contact.phones[0]?.number, '090-1234-5678')
  assert.equal(contact.phones[0]?.normalized_number, '+819012345678')
  assert.equal(contact.phones[0]?.region, 'JP')
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

test('contact presentation stays original while communication uses canonical E.164', async () => {
  const phones = [
    {
      number: '090-1234-5678',
      normalized_number: '+819012345678',
      primary: true
    }
  ]
  assert.equal(phoneDestination(phones[0]), '+819012345678')
  assert.equal(primaryPhoneDestination(phones), '+819012345678')

  const [suggestSource, dialerSource, contactsSource, dashboardSource, messagesSource] =
    await Promise.all([
      readFile(contactSuggest, 'utf8'),
      readFile(dialer, 'utf8'),
      readFile(contactsView, 'utf8'),
      readFile(dashboardView, 'utf8'),
      readFile(messagesView, 'utf8')
    ])

  assert.match(suggestSource, /emit\('update:modelValue', phoneDestination\(suggestion\.phone\)\)/)
  assert.match(dialerSource, /number\.value = phoneDestination\(suggestion\.phone\)/)
  assert.match(contactsSource, /@click="call\(selected, phoneDestination\(phone\)\)"/)
  assert.match(contactsSource, /@click="message\(selected, phoneDestination\(phone\)\)"/)
  assert.match(dashboardSource, /primaryPhoneDestination\(contact\.phones\)/)
  assert.match(messagesSource, /newRecipient\.value = phoneDestination\(suggestion\.phone\)/)
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

test('contact actions use compact copy with complete accessible labels', async () => {
  const source = await readFile(numberActions, 'utf8')

  assert.match(source, /:aria-label="t\('contacts\.new'\)"/)
  assert.match(source, /:title="t\('contacts\.new'\)"/)
  assert.match(source, /t\('contacts\.newShort'\)/)
  assert.match(source, /:aria-label="t\('contacts\.addExisting'\)"/)
  assert.match(source, /:title="t\('contacts\.addExisting'\)"/)
  assert.match(source, /t\('contacts\.addExistingShort'\)/)
})

test('communication detail action groups share one stable gap', async () => {
  const [actions, messages, styles] = await Promise.all([
    readFile(numberActions, 'utf8'),
    readFile(messagesView, 'utf8'),
    readFile(globalStyles, 'utf8')
  ])

  assert.match(
    actions,
    /\.contact-number-actions\.is-compact \{[\s\S]*gap: var\(--detail-action-gap\);/
  )
  assert.match(
    messages,
    /class="detail-header__actions conversation-header__actions"/
  )
  assert.match(styles, /--detail-action-gap: 6px;/)
  assert.match(
    styles,
    /\.detail-header__actions \{[\s\S]*gap: var\(--detail-action-gap\);/
  )
})

test('recordings reuse the compact contact identity and actions in the header', async () => {
  const source = await readFile(recordingsView, 'utf8')

  assert.match(source, /<ContactHeaderIdentity/)
  assert.match(
    source,
    /<CommunicationAvatar[\s\S]*channel="call"[\s\S]*:address="recordingDisplayNumber\(recording\)"[\s\S]*:src="avatar\(recording\)"/
  )
  assert.match(source, /class="recording-list-item__avatar"/)
  assert.match(source, /:number="selected\.call\.remote_number"/)
  assert.match(
    source,
    /class="detail-header__actions recording-header__contact-actions"/
  )
  assert.match(source, /<ContactNumberActions[\s\S]*:contact="selectedContact"[\s\S]*compact/)
})

test('incoming and outgoing call surfaces reuse a matched contact avatar', async () => {
  const source = await readFile(callSurface, 'utf8')

  assert.match(source, /contactForNumber\(session\.value\.remote_number\)/)
  assert.match(
    source,
    /<CommunicationAvatar[\s\S]*channel="call"[\s\S]*:address="presentedNumber"[\s\S]*:src="contact\?\.avatar"/
  )
})

test('fixture persists favorite and preferred line through contact writes', async () => {
  const gateway = createFixtureGateway()
  const created = await gateway.createContact?.({
    display_name: 'Favorite',
    avatar: 'data:image/png;base64,iVBORw0KGgo=',
    favorite: true,
    preferred_line_id: 'line-fixture-main',
    phones: [
      {
        label: 'mobile',
        number: '080 9999 0000',
        region: 'JP',
        primary: true
      }
    ]
  })

  assert.equal(created?.favorite, true)
  assert.equal(created?.avatar, 'data:image/png;base64,iVBORw0KGgo=')
  assert.equal(created?.preferred_line_id, 'line-fixture-main')
  assert.equal(created?.phones[0]?.region, 'JP')

  const updated = await gateway.updateContact?.(created?.id || '', {
    display_name: 'Favorite',
    avatar: created?.avatar,
    favorite: false,
    preferred_line_id: 'line-fixture-travel',
    revision: created?.revision,
    phones:
      created?.phones.map(phone => ({
        id: phone.id,
        label: phone.label,
        number: phone.number,
        region: phone.region,
        primary: phone.primary
      })) || []
  })

  assert.equal(updated?.favorite, false)
  assert.equal(updated?.avatar, created?.avatar)
  assert.equal(updated?.preferred_line_id, 'line-fixture-travel')
  assert.equal(updated?.phones[0]?.region, 'JP')
})

test('contact editor fixes each number region independently from its preferred line', async () => {
  const [source, selectorSource] = await Promise.all([
    readFile(editor, 'utf8'),
    readFile(countrySelector, 'utf8')
  ])

  assert.match(source, /<CountryRegionSelector[\s\S]*v-model="phone\.region"/)
  assert.match(source, /region: phone\.region\?\.trim\(\)\.toUpperCase\(\) \|\| undefined/)
  assert.doesNotMatch(source, /find\(line => line\.id === draft\.preferredLineID\)/)
  assert.match(selectorSource, /role="listbox"/)
  assert.match(selectorSource, /contacts\.internationalNumber/)
  assert.match(selectorSource, /contacts\.searchCountries/)
})

test('contact number regions cover and search the full calling-code catalog', () => {
  const options = phoneRegionOptions('en-US', ['JP', 'VN'])

  assert.ok(options.length > 200)
  assert.equal(options[0]?.region, 'JP')
  assert.equal(options[1]?.region, 'VN')
  assert.equal(options.find(option => option.region === 'GB')?.callingCode, '44')
  assert.ok(
    filterPhoneRegionOptions(options, '+44').some(option => option.region === 'GB')
  )
  assert.equal(
    filterPhoneRegionOptions(options, 'United Kingdom')[0]?.region,
    'GB'
  )
})

test('contact lookup associates only one canonical owner', () => {
  const previous = contactsResource.data
  const previousBootstrap = bootstrapResource.data
  const sharedPhone = {
    id: 'phone-shared',
    label: 'mobile',
    number: '090-1234-5678',
    normalized_number: '+819012345678',
    region: 'JP',
    primary: true
  }
  try {
    contactsResource.data = [
      {
        id: 'contact-a',
        display_name: 'Aiko',
        favorite: false,
        phones: [sharedPhone]
      }
    ]
    assert.equal(contactForNumber('+819012345678')?.id, 'contact-a')
    assert.equal(
      displayPhoneNumber('+819012345678', 'line-fixture-main'),
      '090-1234-5678'
    )

    contactsResource.data = [
      ...contactsResource.data,
      {
        id: 'contact-b',
        display_name: 'Shared household',
        favorite: false,
        preferred_line_id: 'line-secondary',
        phones: [{ ...sharedPhone, id: 'phone-shared-b' }]
      }
    ]
    assert.equal(contactForNumber('+819012345678'), undefined)
    assert.equal(contactForNumber('090-1234-5678'), undefined)
    bootstrapResource.data = {
      lines: [
        { id: 'line-default', capabilities: { dial: true, message: true } },
        { id: 'line-secondary', capabilities: { dial: true, message: true } }
      ],
      line_catalog: [],
      line_settings: { default_line_id: 'line-default' }
    }
    assert.equal(resolveLine('dial', { number: '+819012345678' })?.id, 'line-default')
    assert.equal(
      resolveLine('dial', {
        contextKey: 'line-default',
        number: '+819012345678',
        preferredLineID: 'line-secondary'
      })?.id,
      'line-secondary'
    )
  } finally {
    contactsResource.data = previous
    bootstrapResource.data = previousBootstrap
  }
})

test('explicit contact selection carries its preferred line until the number is edited', async () => {
  const [dialerSource, messagesSource] = await Promise.all([
    readFile(dialer, 'utf8'),
    readFile(messagesView, 'utf8')
  ])

  assert.match(
    dialerSource,
    /preferredLineID: selectedContactPreferredLineID\.value/
  )
  assert.match(
    dialerSource,
    /selectedContactPreferredLineID\.value = suggestion\.contact\.preferred_line_id \|\| ''/
  )
  assert.match(
    dialerSource,
    /function editNumber[\s\S]*selectedContactPreferredLineID\.value = ''/
  )
  assert.match(
    messagesSource,
    /preferredLineID: selectedRecipientPreferredLineID\.value/
  )
  assert.match(
    messagesSource,
    /selectedRecipientPreferredLineID\.value = suggestion\.contact\.preferred_line_id \|\| ''/
  )
  assert.match(
    messagesSource,
    /function editRecipient[\s\S]*selectedRecipientPreferredLineID\.value = ''/
  )
})

test('unknown subscriber numbers use line-aware presentation without changing identity', () => {
  assert.equal(formatPhoneNumber('+819012345678', 'JP'), '090-1234-5678')
  assert.equal(formatPhoneNumber('+8613800138000', 'JP'), '+86 138 0013 8000')
  assert.equal(formatPhoneNumber('10010', 'CN'), '10010')
  assert.equal(phoneDestination({
    number: '090-1234-5678',
    normalized_number: '+819012345678'
  }), '+819012345678')
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
  assert.match(viewSource, /region: phone\.region/)
  assert.match(viewSource, /:aria-pressed="selected\.favorite"/)
  assert.match(viewSource, /v-if="contact\.favorite"/)
})
