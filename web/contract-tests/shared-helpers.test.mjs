import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test, { mock } from 'node:test'
import { effectScope } from 'vue'
import {
  objectValue,
  requiredBoolean,
  requiredString,
  requiredTimestamp,
  stringValue
} from '../src/api/parse.ts'
import { useDebouncedCallback } from '../src/composables/useDebouncedCallback.ts'
import { unreadMissedCallCount } from '../src/state/workspace.ts'
import { communicationAvatarFallback } from '../src/utils/communicationAvatar.ts'
import { communicationAddressKind } from '../src/utils/communicationAddress.ts'
import {
  communicationQuery,
  matchesCommunicationQuery
} from '../src/utils/communicationSearch.ts'
import {
  formatBytes,
  formatCallClock,
  formatCallElapsed,
  formatCompletedDuration,
  formatDateTime,
  formatPlaybackTime
} from '../src/utils/format.ts'
import { randomRequestID } from '../src/utils/id.ts'
import { compactPhoneSyntax, normalizedPhoneIdentity } from '../src/utils/lineIdentity.ts'
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
