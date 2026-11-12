import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test, { mock } from 'node:test'
import { createRenderer, effectScope, ref } from 'vue'
import {
  objectValue,
  requiredBoolean,
  requiredString,
  requiredTimestamp,
  stringValue
} from '../src/api/parse.ts'
import { ApiError } from '../src/api/types.ts'
import { useDebouncedCallback } from '../src/composables/useDebouncedCallback.ts'
import { useListSelection } from '../src/composables/useListSelection.ts'
import { loadNextPage, paginationState } from '../src/state/pagination.ts'
import { playableRecordingCallIDs, recordingCatalogState } from '../src/state/recording.ts'
import {
  attachedLineForKey,
  bootstrapResource,
  communicationPeerAvatar,
  communicationPeerName,
  CONTACT_NAME_FIRST,
  contactsResource,
  defaultLineID,
  displayPhoneNumber,
  lineFallbackFor,
  lineForKey,
  lineWithKey,
  RECORD_NAME_ONLY,
  resetMissingLineFilter,
  resetWorkspaceState,
  unreadMissedCallCount
} from '../src/state/workspace.ts'
import { translate } from '../src/i18n/index.ts'
import { communicationAvatarFallback } from '../src/utils/communicationAvatar.ts'
import { communicationAddressKind } from '../src/utils/communicationAddress.ts'
import {
  communicationQuery,
  matchesCommunicationQuery
} from '../src/utils/communicationSearch.ts'
import { contactInputFromContact, contactPhoneInputs } from '../src/utils/contactInput.ts'
import {
  callDirectionLabel,
  formatBytes,
  formatCallClock,
  formatCallElapsed,
  formatCompletedDuration,
  formatDateTime,
  formatPlaybackTime,
  recordingStatusLabel
} from '../src/utils/format.ts'
import {
  errorResourceStatus,
  forbiddenOrUserErrorMessage,
  LocalizedError,
  userErrorMessage
} from '../src/utils/errorMessage.ts'
import { randomRequestID } from '../src/utils/id.ts'
import {
  compactPhoneSyntax,
  lineTagFallback,
  normalizedPhoneIdentity
} from '../src/utils/lineIdentity.ts'
import { utf8ByteLength } from '../src/utils/utf8.ts'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

test('byte sizes keep their three styles', () => {
  // Shared below 1 KiB.
  for (const style of ['adaptive', 'recording', 'roundedKiB']) {
    assert.equal(formatBytes(512, style), '512 B')
  }
  // Traffic: 1024 steps up to PB with magnitude-based precision.
  assert.equal(formatBytes(1536), '1.50 KB')
  assert.equal(formatBytes(20 * 1024), '20.0 KB')
  assert.equal(formatBytes(200 * 1024 * 1024), '200 MB')
  assert.equal(formatBytes(3 * 1024 ** 5), '3.00 PB')
  assert.equal(formatBytes(2048 * 1024 ** 5), '2048 PB')
  // Recordings: KB or MB, always one decimal.
  assert.equal(formatBytes(1536, 'recording'), '1.5 KB')
  assert.equal(formatBytes(200 * 1024 * 1024, 'recording'), '200.0 MB')
  assert.equal(formatBytes(3 * 1024 ** 3, 'recording'), '3072.0 MB')
  // Certificate files: whole KiB, rounded up.
  assert.equal(formatBytes(1025, 'roundedKiB'), '2 KiB')
  assert.equal(formatBytes(3 * 1024 * 1024, 'roundedKiB'), '3072 KiB')
})

test('clocks keep their padding: call clock pads minutes, playback does not', () => {
  assert.equal(formatCallClock(5), '00:05')
  assert.equal(formatPlaybackTime(5), '0:05')
  assert.equal(formatCallClock(605), '10:05')
  assert.equal(formatPlaybackTime(605), '10:05')
  assert.equal(formatCallClock(3725), '1:02:05')
  assert.equal(formatPlaybackTime(3725), '1:02:05')
  assert.equal(formatCallClock(-3), '00:00')
  assert.equal(formatPlaybackTime(-3), '0:00')
  assert.equal(formatPlaybackTime(Number.NaN), '0:00')
  assert.equal(formatPlaybackTime(Number.POSITIVE_INFINITY), '0:00')
  assert.equal(formatCallClock(Number.NaN), 'NaN:NaN')
})

test('call elapsed time counts whole seconds from activation and never goes negative', () => {
  const start = '2026-01-01T00:00:00.000Z'
  const startMs = Date.parse(start)
  assert.equal(formatCallElapsed(start, startMs + 65_900), '01:05')
  assert.equal(formatCallElapsed(start, startMs - 5_000), '00:00')
})

test('completed durations read as seconds under a minute and m:ss after', () => {
  assert.equal(formatCompletedDuration(42), '42 sec')
  assert.equal(formatCompletedDuration(65), '1:05')
  assert.equal(formatCompletedDuration(3725), '62:05')
})

test('date-time styles keep each caller\'s Intl options', async () => {
  const value = '2026-03-05T09:41:07Z'
  const date = new Date(value)
  const expected = options => new Intl.DateTimeFormat('en-US', options).format(date)
  assert.equal(formatDateTime(value), expected({
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
  }))
  assert.equal(formatDateTime(value, 'numeric'), expected({
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  }))
  assert.equal(formatDateTime(value, 'log'), expected({
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
  }))
  assert.equal(formatDateTime(value, 'logTime'), expected({
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
  }))
  assert.equal(formatDateTime(value, 'medium'), expected({ dateStyle: 'medium', timeStyle: 'short' }))
  assert.equal(formatDateTime('not a date', 'log'), 'not a date')
  assert.equal(formatDateTime('', 'medium'), '')

  // Each caller keeps its own empty-value text.
  const [certificate, diagnostics, access] = await Promise.all([
    source('../src/components/CertificateFacts.vue'),
    source('../src/components/DiagnosticsPanel.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue')
  ])
  assert.match(certificate, /value \? formatDateTime\(value, 'numeric'\) : t\('tls\.notProvided'\)/)
  assert.match(diagnostics, /formatDateTime\(value, timeOnly \? 'logTime' : 'log'\) \|\| '—'/)
  assert.match(access, /value \? formatDateTime\(value, 'medium'\) : ''/)
})

test('phone compaction is shared; only avatars fold a 00 exit code into +', () => {
  assert.equal(compactPhoneSyntax(' +49 (30) 123-45.67/8 '), '+493012345678')
  assert.equal(compactPhoneSyntax('0049 30 1234567'), '0049301234567')
  assert.equal(
    compactPhoneSyntax('0049 30 1234567', { internationalDoubleZero: true }),
    '+49301234567'
  )
  assert.equal(compactPhoneSyntax('0030', { internationalDoubleZero: false }), '0030')

  // Address matching keeps 0049… and +49… apart.
  assert.equal(normalizedPhoneIdentity('+49 30 1234567'), '+49301234567')
  assert.equal(normalizedPhoneIdentity('0049 30 1234567'), '0049 30 1234567')
  assert.notEqual(
    normalizedPhoneIdentity('0049 30 1234567'),
    normalizedPhoneIdentity('+49 30 1234567')
  )
  assert.equal(normalizedPhoneIdentity(' Bank '), 'bank')
  assert.equal(normalizedPhoneIdentity(''), '')

  // Avatars treat them as the same number.
  assert.equal(
    communicationAvatarFallback({
      channel: 'call',
      name: '0049 30 1234567',
      address: '+49301234567',
      contactBound: true
    }),
    'person'
  )

  // Address kinds strip / as well.
  assert.equal(communicationAddressKind('+49 30/1234567'), 'subscriber')
  assert.equal(communicationAddressKind('11/88'), 'short_code')
})

test('communication search matches text fields and number digits only', () => {
  const byName = communicationQuery('  ALEX ')
  assert.deepEqual(byName, { text: 'alex', digits: '' })
  assert.equal(matchesCommunicationQuery(byName, { text: ['Alex Rowan'], numbers: ['+1 202'] }), true)
  assert.equal(matchesCommunicationQuery(byName, { text: [undefined], numbers: ['+1 202'] }), false)

  const byNumber = communicationQuery('(202) 555')
  assert.equal(byNumber.digits, '202555')
  assert.equal(matchesCommunicationQuery(byNumber, { text: ['Alex'], numbers: ['+1 202-555-0103'] }), true)
  assert.equal(matchesCommunicationQuery(byNumber, { text: ['Alex'], numbers: ['+1 303 555 0103'] }), false)

  // Letters never match a number, and a record without numbers matches by text only.
  assert.equal(matchesCommunicationQuery(communicationQuery('x'), { numbers: ['123'] }), false)
  assert.equal(matchesCommunicationQuery(communicationQuery('hello'), { text: ['Bob', 'Hello there'] }), true)
})

test('debounced callbacks run once with the last arguments and stop with their scope', () => {
  mock.timers.enable({ apis: ['setTimeout'] })
  try {
    const calls = []
    const scope = effectScope()
    const debounced = scope.run(() => useDebouncedCallback(value => calls.push(value), 250))
    debounced.schedule('a')
    mock.timers.tick(200)
    debounced.schedule('b')
    mock.timers.tick(249)
    assert.deepEqual(calls, [])
    mock.timers.tick(1)
    assert.deepEqual(calls, ['b'])

    debounced.schedule('c')
    debounced.cancel()
    mock.timers.tick(500)
    assert.deepEqual(calls, ['b'])

    debounced.schedule('d')
    scope.stop()
    mock.timers.tick(500)
    assert.deepEqual(calls, ['b'])
  } finally {
    mock.timers.reset()
  }
})

test('request IDs are random version 4 UUIDs', () => {
  const first = randomRequestID()
  assert.match(first, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  assert.notEqual(first, randomRequestID())
})

test('UTF-8 byte length counts encoded bytes, not characters', () => {
  assert.equal(utf8ByteLength(''), 0)
  assert.equal(utf8ByteLength('abc'), 3)
  assert.equal(utf8ByteLength('密'), 3)
  assert.equal(utf8ByteLength('😀'), 4)
})

test('API base validators keep each parser\'s leniency and error type', async () => {
  assert.throws(() => objectValue([], 'response'), { message: 'response 必须是对象' })
  class CustomError extends Error {}
  assert.throws(
    () => objectValue(null, 'response', message => new CustomError(message)),
    error => error instanceof CustomError && error.message === 'response 必须是对象'
  )

  assert.equal(stringValue({ a: ' x ' }, 'a'), 'x')
  assert.equal(stringValue({ a: 7 }, 'a'), '7')
  assert.equal(stringValue({ a: true }, 'a'), '')

  // normalize.ts accepts numbers; contract.ts accepts only strings, optionally blank.
  assert.equal(requiredString({ id: 12 }, 'line', 'id', { acceptNumber: true }), '12')
  assert.throws(() => requiredString({ id: 12 }, 'line', 'id'), { message: 'line 缺少 id' })
  assert.throws(() => requiredString({ id: ' ' }, 'line', 'id', { acceptNumber: true }), { message: 'line 缺少 id' })
  assert.equal(requiredString({ id: ' ' }, 'line', 'id', { allowEmpty: true }), '')
  assert.equal(requiredString({ id: ' a ' }, 'line', 'id'), 'a')

  assert.equal(requiredTimestamp({ at: '2026-01-01T00:00:00Z' }, 'x', 'at'), '2026-01-01T00:00:00Z')
  assert.throws(() => requiredTimestamp({ at: 'soon' }, 'x', 'at'), { message: 'x.at 不是有效时间' })

  assert.equal(requiredBoolean({ on: false }, 'x', 'on'), false)
  assert.throws(() => requiredBoolean({ on: 'yes' }, 'x', 'on'), { message: 'x.on 必须是布尔值' })

  // client.ts keeps raising ApiError(invalid_response) through the shared checks.
  const client = await source('../src/api/client.ts')
  assert.match(client, /const invalidResponse: ParseFailure = message => new ApiError\(message, 0, 'invalid_response'\)/)
  assert.match(client, /return objectValue\(value, path, invalidResponse\)/)
  assert.match(client, /return requiredBoolean\(source, path, key, invalidResponse\)/)
})

test('unread missed calls are missed and not yet read', () => {
  assert.equal(
    unreadMissedCallCount([
      { missed: true, read: false },
      { missed: true, read: true },
      { missed: false, read: false }
    ]),
    1
  )
})

test('a contact maps to a whole-contact input, with only the overridden fields changed', () => {
  const contact = {
    id: 'c1',
    display_name: 'Ada',
    avatar: 'data:image/png;base64,AA',
    favorite: false,
    notes: 'note',
    preferred_line_id: 'line-a',
    revision: 7,
    created_at: '2026-01-01T00:00:00Z',
    phones: [
      { id: 'p1', label: 'Mobile', number: '+1 202 555 0101', normalized_number: '+12025550101', region: 'US', primary: true },
      { id: 'p2', label: 'Work', number: '090-1234-5678', primary: false }
    ]
  }
  const phones = [
    { id: 'p1', label: 'Mobile', number: '+1 202 555 0101', region: 'US', primary: true },
    { id: 'p2', label: 'Work', number: '090-1234-5678', region: undefined, primary: false }
  ]
  // Server-owned fields (id, timestamps, normalized_number) are not sent back.
  assert.deepEqual(contactPhoneInputs(contact.phones), phones)
  const input = {
    display_name: 'Ada',
    avatar: 'data:image/png;base64,AA',
    favorite: false,
    notes: 'note',
    preferred_line_id: 'line-a',
    revision: 7,
    phones
  }
  assert.deepEqual(contactInputFromContact(contact), input)
  // Same keys in the same order as the copies it replaced, so the request body is unchanged.
  assert.deepEqual(Object.keys(contactInputFromContact(contact)), Object.keys(input))
  assert.deepEqual(contactInputFromContact(contact, { favorite: true }), { ...input, favorite: true })
  const added = { label: 'Other', number: '+12025550199', primary: false }
  assert.deepEqual(
    contactInputFromContact(contact, { phones: [...contactPhoneInputs(contact.phones), added] }).phones,
    [...phones, added]
  )
})

function bootstrapLine(id, extra = {}) {
  return { id, line_label: '', device_name: '', model: '', iccid: '', device_imei: '', ...extra }
}

test('line lookups: full catalog, attached-only, and the tag fallback text', t => {
  t.after(resetWorkspaceState)
  const main = bootstrapLine('line-main')
  const second = bootstrapLine('line-second')
  const named = bootstrapLine('line-named', { device_name: 'Quectel EC25' })
  const retired = bootstrapLine('line-retired')
  resetWorkspaceState()
  assert.equal(defaultLineID.value, '')
  bootstrapResource.data = {
    lines: [main, second, named],
    line_catalog: [main, second, named, retired],
    line_settings: { default_line_id: 'line-main' }
  }
  assert.equal(defaultLineID.value, 'line-main')

  // lineForKey trims and falls back to retired lines; the attached lookup does neither.
  assert.equal(lineForKey(' line-retired ')?.id, 'line-retired')
  assert.equal(attachedLineForKey('line-retired'), undefined)
  assert.equal(lineForKey(' line-second')?.id, 'line-second')
  assert.equal(attachedLineForKey(' line-second'), undefined)
  assert.equal(attachedLineForKey('line-second')?.id, 'line-second')
  assert.equal(lineWithKey([retired], 'line-retired'), retired)
  assert.equal(lineWithKey([retired], ' line-retired'), undefined)

  // Same text the per-page copies produced from lineForKey, attached lines and the default.
  const lines = bootstrapResource.data.lines
  for (const id of ['line-main', 'line-second', 'line-named', 'line-retired', 'gone-1937', '']) {
    assert.equal(lineFallbackFor(id), lineTagFallback(lineForKey(id), lines, 'line-main', id))
  }
  assert.equal(lineFallbackFor('line-main'), 'Primary line')
  assert.equal(lineFallbackFor('line-second'), 'Line 2')
  assert.equal(lineFallbackFor('line-named'), 'Quectel EC25')
  // A retired line is not among attached lines, so it reads as the first line.
  assert.equal(lineFallbackFor('line-retired'), 'Line 1')
  assert.equal(lineFallbackFor('gone-1937'), 'Line 1937')
  assert.equal(lineFallbackFor(''), 'Unknown line')
})

test('call direction keeps the history and diagnostic wordings', () => {
  assert.equal(callDirectionLabel('incoming', { missed: true }), 'Missed call')
  assert.equal(callDirectionLabel('incoming'), 'Incoming')
  assert.equal(callDirectionLabel('outgoing'), 'Outgoing')
  // History: anything that is not incoming reads as outgoing.
  assert.equal(callDirectionLabel('sideways'), 'Outgoing')
  assert.equal(callDirectionLabel(''), 'Outgoing')
  // Diagnostics: longer incoming text, raw or unknown otherwise, no missed branch.
  assert.equal(callDirectionLabel('incoming', { style: 'diagnostic' }), 'Incoming call')
  assert.equal(callDirectionLabel('outgoing', { style: 'diagnostic' }), 'Outgoing')
  assert.equal(callDirectionLabel('sideways', { style: 'diagnostic' }), 'sideways')
  assert.equal(callDirectionLabel('', { style: 'diagnostic' }), 'Direction unknown')
})

test('recording status keeps the catalog and per-call list rules', () => {
  for (const playable of [undefined, false]) {
    assert.equal(recordingStatusLabel('pending', playable), 'Waiting to record')
    assert.equal(recordingStatusLabel('recording', playable), 'Recording')
    assert.equal(recordingStatusLabel('failed', playable), 'Recording failed')
  }
  // Per-call list (status only): an unrecognised status reads as playable.
  assert.equal(recordingStatusLabel('ready'), 'Playable')
  // Catalog: playable wins over status, otherwise an unrecognised status is unavailable.
  assert.equal(recordingStatusLabel('failed', true), 'Playable')
  assert.equal(recordingStatusLabel('ready', true), 'Playable')
  assert.equal(recordingStatusLabel('ready', false), 'Unavailable')
})

test('the playable-recording index lists calls with at least one playable recording', t => {
  const original = recordingCatalogState.data
  t.after(() => { recordingCatalogState.data = original })
  recordingCatalogState.data = [
    { id: 'r1', call_id: 'call-a', playable: false },
    { id: 'r2', call_id: 'call-a', playable: true },
    { id: 'r3', call_id: 'call-b', playable: false }
  ]
  assert.deepEqual([...playableRecordingCallIDs.value], ['call-a'])
})

function nextPageFixture(overrides = {}) {
  let generation = 0
  const pagination = Object.assign(paginationState(), {
    nextCursor: 'c1', hasMore: true, pages: 1
  })
  const target = { status: 'ready', data: [{ id: 'a' }, { id: 'b' }] }
  const restarts = []
  const options = {
    target,
    pagination,
    generation: () => generation,
    loader: async () => ({ items: [{ id: 'b' }, { id: 'c' }], meta: { next_cursor: 'c2', has_more: false } }),
    restart: async () => { restarts.push(true); return ['restarted'] },
    identity: item => item.id,
    errorMessage: () => 'load failed',
    ...overrides
  }
  return { options, target, pagination, restarts, bump: () => { generation += 1 } }
}

test('next-page loading appends unique items, guards its cursor, and restarts on a stale cursor', async () => {
  const appended = nextPageFixture()
  assert.deepEqual(await loadNextPage(appended.options), [{ id: 'a' }, { id: 'b' }, { id: 'c' }])
  assert.deepEqual(
    { ...appended.pagination },
    { nextCursor: 'c2', hasMore: false, loadingMore: false, error: '', pages: 2, generation: 0 }
  )
  // Nothing more to load: the current data comes back and no request is made.
  let calls = 0
  const done = nextPageFixture({ loader: async () => { calls += 1 } })
  done.pagination.hasMore = false
  assert.equal(await loadNextPage(done.options), done.target.data)
  assert.equal(calls, 0)

  const prepended = nextPageFixture({ prepend: true })
  assert.deepEqual((await loadNextPage(prepended.options)).map(item => item.id), ['b', 'c', 'a'])

  const stale = nextPageFixture()
  stale.options.loader = async () => { stale.bump(); return { items: [{ id: 'z' }], meta: { next_cursor: '', has_more: false } } }
  assert.equal(await loadNextPage(stale.options), null)
  assert.deepEqual(stale.target.data.map(item => item.id), ['a', 'b'])
  // The owner moved on, so its new load owns loadingMore.
  assert.equal(stale.pagination.loadingMore, true)

  const cursor = nextPageFixture({
    loader: async () => { throw new ApiError('bad cursor', 400, 'invalid_argument', 'cursor') }
  })
  assert.deepEqual(await loadNextPage(cursor.options), ['restarted'])
  assert.equal(cursor.restarts.length, 1)

  const failed = nextPageFixture({ loader: async () => { throw new Error('offline') } })
  assert.equal(await loadNextPage(failed.options), null)
  assert.equal(failed.pagination.error, 'load failed')
  assert.equal(failed.pagination.loadingMore, false)
  assert.equal(failed.restarts.length, 0)
})

test('Escape leaves selection mode only while the list is mounted', () => {
  const previousWindow = globalThis.window
  const target = new EventTarget()
  globalThis.window = target
  try {
    const renderer = createRenderer({
      createElement: tag => ({ tag, children: [] }),
      createText: text => ({ text }),
      createComment: text => ({ text }),
      insert(child, parent) { child.parent = parent; parent.children.push(child) },
      remove(child) { child.parent.children = child.parent.children.filter(item => item !== child) },
      setText() {},
      setElementText() {},
      parentNode: node => node.parent,
      nextSibling: () => null,
      patchProp() {}
    })
    let selection
    const app = renderer.createApp({
      setup() {
        selection = useListSelection(item => item)
        return () => null
      }
    })
    const press = key => target.dispatchEvent(Object.assign(new Event('keydown'), { key }))
    app.mount({ children: [] })
    selection.toggle('a')
    press('Enter')
    assert.equal(selection.active.value, true)
    press('Escape')
    assert.equal(selection.active.value, false)
    assert.equal(selection.count.value, 0)

    selection.toggle('a')
    app.unmount()
    press('Escape')
    assert.equal(selection.active.value, true)
  } finally {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  }
})

test('peer names keep each page\'s order between the record name and the contact name', t => {
  t.after(resetWorkspaceState)
  resetWorkspaceState()
  contactsResource.data = [{
    id: 'contact-1',
    display_name: 'Saved Contact',
    avatar: 'data:image/png;base64,AA==',
    favorite: false,
    phones: [{ id: 'phone-1', label: 'mobile', number: '090-1111-2222', normalized_number: '+819011112222', primary: true }]
  }]
  const known = { number: '+819011112222', lineID: 'line-1', recordName: 'Name On Record' }
  const unnamed = { number: '+819011112222', lineID: 'line-1' }
  const stranger = { number: '+819099998888', lineID: 'line-1' }

  // Every page and notification: the name the user saved wins over the record's.
  assert.equal(communicationPeerName(known, CONTACT_NAME_FIRST), 'Saved Contact')
  assert.equal(communicationPeerName(unnamed, CONTACT_NAME_FIRST), 'Saved Contact')
  // A number with no saved contact still shows the name carried on the record.
  assert.equal(
    communicationPeerName({ ...stranger, recordName: 'Name On Record' }, CONTACT_NAME_FIRST),
    'Name On Record'
  )
  // Global search shows only the record's name and never looks up the contact's.
  assert.equal(communicationPeerName(known, RECORD_NAME_ONLY), 'Name On Record')
  assert.equal(communicationPeerName(unnamed, RECORD_NAME_ONLY), '090-1111-2222')
  assert.equal(communicationPeerName({ ...unnamed, recordName: '' }, RECORD_NAME_ONLY), '090-1111-2222')
  // Without any name every order falls back to the displayed number.
  for (const order of [CONTACT_NAME_FIRST, RECORD_NAME_ONLY]) {
    assert.equal(communicationPeerName(stranger, order), displayPhoneNumber(stranger.number, 'line-1'))
  }

  assert.equal(communicationPeerAvatar('+819011112222'), 'data:image/png;base64,AA==')
  assert.equal(communicationPeerAvatar('+819099998888'), '')
})

test('a line filter resets to every line only when its line is gone', () => {
  const lines = [bootstrapLine('line-a'), bootstrapLine('line-b')]
  const filter = ref('line-b')
  resetMissingLineFilter(filter, lines)
  assert.equal(filter.value, 'line-b')
  resetMissingLineFilter(filter, lines.slice(0, 1))
  assert.equal(filter.value, 'all')
  resetMissingLineFilter(filter, [])
  assert.equal(filter.value, 'all')
})

test('user error messages never show server or thrown text', () => {
  const fallback = 'Localized fallback'
  // Unknown codes, plain thrown errors and anything else use the caller's fallback.
  assert.equal(userErrorMessage(new ApiError('raw server text', 400, 'unknown_code'), fallback), fallback)
  assert.equal(userErrorMessage(new ApiError('raw server text', 409), fallback), fallback)
  assert.equal(userErrorMessage(new Error('Outgoing media readiness timed out'), fallback), fallback)
  assert.equal(userErrorMessage('raw string', fallback), fallback)
  assert.equal(userErrorMessage(undefined, fallback), fallback)
  // Known codes and transport failures translate.
  assert.equal(userErrorMessage(new ApiError('raw', 409, 'conflict'), fallback), translate('errors.operation_conflict'))
  // Only an explicit revision_conflict reads as "edited elsewhere".
  assert.equal(userErrorMessage(new ApiError('raw', 409, 'revision_conflict'), fallback), translate('errors.revision_conflict'))
  assert.notEqual(translate('errors.operation_conflict'), translate('errors.revision_conflict'))
  assert.equal(userErrorMessage(new ApiError('raw', 404, 'call_not_found'), fallback), translate('errors.not_found'))
  assert.equal(userErrorMessage(new ApiError('raw', 401), fallback), translate('errors.authentication_required'))
  assert.equal(userErrorMessage(new ApiError('raw', 403), fallback), translate('errors.forbidden'))
  assert.equal(userErrorMessage(new ApiError('raw', 503), fallback), translate('errors.server'))
  assert.equal(userErrorMessage(new TypeError('Failed to fetch'), fallback), translate('errors.network'))
  // Text the client already localized is shown as is.
  assert.equal(userErrorMessage(new LocalizedError('Bild ist zu groß'), fallback), 'Bild ist zu groß')
  // Console output that logs the error name keeps reading `Error`.
  assert.equal(new LocalizedError('x').name, 'Error')
})

test('a 403 shows the resource\'s own forbidden text, otherwise the user error message', () => {
  const forbidden = 'Recordings are not available to you'
  assert.equal(forbiddenOrUserErrorMessage(new ApiError('raw', 403), 'fallback', forbidden), forbidden)
  // Even a known code does not override the resource's forbidden text.
  assert.equal(forbiddenOrUserErrorMessage(new ApiError('raw', 403, 'conflict'), 'fallback', forbidden), forbidden)
  assert.equal(forbiddenOrUserErrorMessage(new ApiError('raw', 400), 'fallback', forbidden), 'fallback')
  assert.equal(forbiddenOrUserErrorMessage(new ApiError('raw', 500), 'fallback', forbidden), translate('errors.server'))
  assert.equal(forbiddenOrUserErrorMessage(new LocalizedError('Shown as is'), 'fallback', forbidden), 'Shown as is')

  assert.equal(errorResourceStatus(new ApiError('raw', 403)), 'forbidden')
  assert.equal(errorResourceStatus(new ApiError('raw', 500)), 'error')
  assert.equal(errorResourceStatus(new Error('raw')), 'error')
})
