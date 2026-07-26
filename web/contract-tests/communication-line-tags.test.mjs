import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine,
  phoneIdentitiesMatch
} from '../src/utils/lineIdentity.ts'

const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)
const callsView = new URL('../src/views/CallsView.vue', import.meta.url)
const recordingsView = new URL('../src/views/RecordingsView.vue', import.meta.url)
const dashboardView = new URL('../src/views/DashboardView.vue', import.meta.url)
const messageThreadListItem = new URL(
  '../src/components/MessageThreadListItem.vue',
  import.meta.url
)
const callHistoryListItem = new URL(
  '../src/components/CallHistoryListItem.vue',
  import.meta.url
)

const lines = [
  {
    id: 'line-main',
    iccid: '898601',
    imsi: '001010000000001',
    phone_number: '+1 202 555 0101',
    operator: '',
    device_imei: 'imei-main',
    device_alias: '',
    line_label: ''
  },
  {
    id: 'line-secondary',
    iccid: '898602',
    imsi: '001020000000002',
    phone_number: '+1 202 555 0102',
    operator: '',
    device_imei: 'imei-secondary',
    device_alias: '',
    line_label: '工作'
  }
]

test('shared line identity keeps known and historical records distinct', () => {
  const lookup = createLineLookup(lines)
  const main = findLine(lookup, 'line-main')
  const secondary = findLine(lookup, '898602')

  assert.equal(main, lines[0])
  assert.equal(secondary, lines[1])
  assert.equal(findLine(lookup, '+81 (80) 1234-5678'), lines[0])
  assert.equal(findLine(lookup, '0081 80 1234 5678'), lines[0])
  assert.equal(findLine(lookup, '001020000000002'), lines[1])
  assert.equal(lineTagFallback(main, lines, 'imei-main', 'line-main'), 'Primary line')
  assert.equal(
    lineTagFallback(secondary, lines, 'imei-main', 'line-secondary'),
    'Line 2'
  )
  assert.equal(
    lineTagFallback(undefined, lines, 'imei-main', 'removed-line-1937'),
    'Line 1937'
  )
  assert.equal(lineTagFallback(undefined, lines, 'imei-main'), 'Unknown line')
  assert.deepEqual(lineTagLine(undefined, 'removed-line-1937'), {
    id: 'removed-line-1937',
    iccid: '',
    line_label: ''
  })
})

test('international plus and 00 prefixes share one phone identity', () => {
  assert.equal(phoneIdentitiesMatch('+1 202 555 0101', '0081 80 1234 5678'), true)
  assert.equal(phoneIdentitiesMatch('+1 202 555 0101', '+81 (80) 1234-5678'), true)
  assert.equal(phoneIdentitiesMatch('00123', '+123'), false)
  assert.equal(phoneIdentitiesMatch('', ''), false)
})

test('message rows and conversation detail identify the original line', async () => {
  const [source, row] = await Promise.all([
    readFile(messagesView, 'utf8'),
    readFile(messageThreadListItem, 'utf8')
  ])

  assert.match(source, /import MessageThreadListItem from/)
  assert.match(row, /import LineTag from '\.\/LineTag\.vue'/)
  assert.match(source, /createLineLookup\(lines\.value\)/)
  assert.match(source, /function threadLineFallback\(thread: MessageThread\)/)
  assert.match(
    source,
    /if \(thread\.local_phone\) \{[\s\S]*?findLine\(lineLookup\.value, thread\.local_phone\)/
  )
  assert.match(source, /findLine\([\s\S]*?thread\.imsi,[\s\S]*?thread\.iccid/)
  assert.match(
    source,
    /<MessageThreadListItem[\s\S]*?:line="lineTagLine\(lineForThread\(thread\), thread\.local_phone, thread\.imsi, thread\.iccid\)"[\s\S]*?:line-fallback="threadLineFallback\(thread\)"/
  )
  assert.match(row, /<LineTag :line="line" :fallback="lineFallback"/)
  assert.match(
    source,
    /<ContactHeaderIdentity[\s\S]*?:line="lineTagLine\(lineForThread\(selectedThread\), selectedThread\.local_phone, selectedThread\.imsi, selectedThread\.iccid\)"[\s\S]*?:line-fallback="threadLineFallback\(selectedThread\)"/
  )
})

test('call rows and call detail identify the original line', async () => {
  const [source, row] = await Promise.all([
    readFile(callsView, 'utf8'),
    readFile(callHistoryListItem, 'utf8')
  ])

  assert.match(source, /import CallHistoryListItem from/)
  assert.match(source, /import LineTag from '\.\.\/components\/LineTag\.vue'/)
  assert.match(row, /import LineTag from '\.\/LineTag\.vue'/)
  assert.match(source, /function lineForCall\(call: CallRecord\)/)
  assert.match(source, /function callLineFallback\(call: CallRecord\)/)
  assert.match(
    source,
    /if \(call\.local_phone\) \{[\s\S]*?findLine\(lineLookup\.value, call\.local_phone\)/
  )
  assert.match(source, /findLine\([\s\S]*?call\.line_iccid,[\s\S]*?call\.line_imsi/)
  assert.match(
    source,
    /<CallHistoryListItem[\s\S]*?:line="lineTagLine\(lineForCall\(call\), call\.local_phone, call\.line_iccid, call\.line_imsi\)"[\s\S]*?:line-fallback="callLineFallback\(call\)"/
  )
  assert.match(row, /<LineTag :line="line" :fallback="lineFallback"/)
  assert.match(
    source,
    /<ContactHeaderIdentity[\s\S]*?:line="lineTagLine\(lineForCall\(selected\), selected\.local_phone, selected\.line_iccid, selected\.line_imsi\)"[\s\S]*?:line-fallback="callLineFallback\(selected\)"/
  )
  assert.match(source, /function actionLineKey\(call: CallRecord\)/)
  assert.match(source, /openDialer\(call\.remote_number, displayName\(call\), actionLineKey\(call\)\)/)
  assert.match(source, /const selectedLineKey = actionLineKey\(call\)/)
  assert.doesNotMatch(source, /findLine\([\s\S]{0,200}?call\.device_id/)
  assert.doesNotMatch(source, /\{ line: call\.device_id \}/)
})

test('recording rows and recording detail identify the call line', async () => {
  const source = await readFile(recordingsView, 'utf8')

  assert.match(source, /import LineTag from '\.\.\/components\/LineTag\.vue'/)
  assert.match(source, /function lineForRecording\(recording: RecordingEntry\)/)
  assert.match(source, /function recordingLineFallback\(recording: RecordingEntry\)/)
  assert.match(
    source,
    /<LineTag[\s\S]*?:line="lineTagLine\(lineForRecording\(recording\), recording\.call\.device_id\)"[\s\S]*?:fallback="recordingLineFallback\(recording\)"/
  )
  assert.match(
    source,
    /<ContactHeaderIdentity[\s\S]*?:line="lineTagLine\(lineForRecording\(selected\), selected\.call\.device_id\)"[\s\S]*?:line-fallback="recordingLineFallback\(selected\)"/
  )
})

test('call and recording histories can be filtered by communication line', async () => {
  const [calls, recordings] = await Promise.all([
    readFile(callsView, 'utf8'),
    readFile(recordingsView, 'utf8')
  ])

  for (const source of [calls, recordings]) {
    assert.match(source, /import LineSelector from '\.\.\/components\/LineSelector\.vue'/)
    assert.match(source, /const lineFilterKey = ref\('all'\)/)
    assert.match(source, /lineKey\(line\) === lineFilterKey\.value/)
    assert.match(source, /<LineSelector[\s\S]*?v-model="lineFilterKey"[\s\S]*?include-all/)
  }
  assert.match(calls, /const filteredLine =[\s\S]*?lineFilterKey\.value === 'all'/)
  assert.match(calls, /const callLine = lineForCall\(call\)/)
  assert.match(recordings, /const filteredRecordings = computed/)
  assert.match(recordings, /lineForRecording\(recording\)/)
  assert.match(calls, /t\('calls\.allLinesDescription'\)/)
  assert.match(recordings, /t\('recordings\.allLinesDescription'\)/)
})

test('dashboard recent activity retains line identity and delegates detail surfaces', async () => {
  const [source, messages, calls] = await Promise.all([
    readFile(dashboardView, 'utf8'),
    readFile(messagesView, 'utf8'),
    readFile(callsView, 'utf8')
  ])

  assert.match(
    source,
    /<MessageThreadListItem[\s\S]*?:line="lineTagLine\(lineForThread\(activity\.thread\), activity\.thread\.local_phone, activity\.thread\.imsi, activity\.thread\.iccid\)"[\s\S]*?:line-fallback="threadLineFallback\(activity\.thread\)"/
  )
  assert.match(
    source,
    /<CallHistoryListItem[\s\S]*?:line="lineTagLine\(lineForCall\(activity\.call\), activity\.call\.local_phone, activity\.call\.line_iccid, activity\.call\.line_imsi\)"[\s\S]*?:line-fallback="callLineFallback\(activity\.call\)"/
  )
  assert.match(
    source,
    /<MessagesView[\s\S]*?:embedded-thread-key="selectedThread\.key"/
  )
  assert.match(
    source,
    /<CallsView[\s\S]*?:embedded-call-id="selectedCall\.id"/
  )
  assert.match(
    messages,
    /:line="lineTagLine\(lineForThread\(selectedThread\), selectedThread\.local_phone, selectedThread\.imsi, selectedThread\.iccid\)"/
  )
  assert.match(
    calls,
    /:line="lineTagLine\(lineForCall\(selected\), selected\.local_phone, selected\.line_iccid, selected\.line_imsi\)"/
  )
})
