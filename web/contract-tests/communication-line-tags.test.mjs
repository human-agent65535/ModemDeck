import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine
} from '../src/utils/lineIdentity.ts'

const messagesView = new URL('../src/views/MessagesView.vue', import.meta.url)
const callsView = new URL('../src/views/CallsView.vue', import.meta.url)
const recordingsView = new URL('../src/views/RecordingsView.vue', import.meta.url)
const dashboardView = new URL('../src/views/DashboardView.vue', import.meta.url)

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
  assert.equal(findLine(lookup, '001020000000002'), lines[1])
  assert.equal(lineTagFallback(main, lines, 'imei-main', 'line-main'), '主卡')
  assert.equal(
    lineTagFallback(secondary, lines, 'imei-main', 'line-secondary'),
    '线路 2'
  )
  assert.equal(
    lineTagFallback(undefined, lines, 'imei-main', 'removed-line-1937'),
    '线路 1937'
  )
  assert.equal(lineTagFallback(undefined, lines, 'imei-main'), '未知线路')
  assert.deepEqual(lineTagLine(undefined, 'removed-line-1937'), {
    id: 'removed-line-1937',
    iccid: '',
    line_label: ''
  })
})

test('message rows and conversation detail identify the original line', async () => {
  const source = await readFile(messagesView, 'utf8')

  assert.match(source, /import LineTag from '\.\.\/components\/LineTag\.vue'/)
  assert.match(source, /createLineLookup\(lines\.value\)/)
  assert.match(source, /function threadLineFallback\(thread: MessageThread\)/)
  assert.match(
    source,
    /if \(thread\.local_phone\) \{[\s\S]*?findLine\(lineLookup\.value, thread\.local_phone\)/
  )
  assert.match(source, /findLine\([\s\S]*?thread\.imsi,[\s\S]*?thread\.iccid/)
  assert.match(
    source,
    /<LineTag[\s\S]*?:line="lineTagLine\(lineForThread\(thread\), thread\.local_phone, thread\.imsi, thread\.iccid\)"[\s\S]*?:fallback="threadLineFallback\(thread\)"/
  )
  assert.match(
    source,
    /class="conversation-line-tag"[\s\S]*?:line="lineTagLine\(lineForThread\(selectedThread\), selectedThread\.local_phone, selectedThread\.imsi, selectedThread\.iccid\)"[\s\S]*?:fallback="threadLineFallback\(selectedThread\)"/
  )
})

test('call rows and call detail identify the original line', async () => {
  const source = await readFile(callsView, 'utf8')

  assert.match(source, /import LineTag from '\.\.\/components\/LineTag\.vue'/)
  assert.match(source, /function lineForCall\(call: CallRecord\)/)
  assert.match(source, /function callLineFallback\(call: CallRecord\)/)
  assert.match(
    source,
    /if \(call\.local_phone\) \{[\s\S]*?findLine\(lineLookup\.value, call\.local_phone\)/
  )
  assert.match(source, /findLine\([\s\S]*?call\.line_iccid,[\s\S]*?call\.line_imsi/)
  assert.match(
    source,
    /<LineTag[\s\S]*?:line="lineTagLine\(lineForCall\(call\), call\.local_phone, call\.line_iccid, call\.line_imsi\)"[\s\S]*?:fallback="callLineFallback\(call\)"/
  )
  assert.match(
    source,
    /<dt>线路<\/dt>[\s\S]*?:line="lineTagLine\(lineForCall\(selected\), selected\.local_phone, selected\.line_iccid, selected\.line_imsi\)"[\s\S]*?:fallback="callLineFallback\(selected\)"/
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
    /<dt>线路<\/dt>[\s\S]*?:line="lineTagLine\(lineForRecording\(selected\), selected\.call\.device_id\)"[\s\S]*?:fallback="recordingLineFallback\(selected\)"/
  )
})

test('dashboard recent activity and details retain line identity', async () => {
  const source = await readFile(dashboardView, 'utf8')

  assert.match(source, /function lineForActivity\(activity: DashboardActivity\)/)
  assert.match(
    source,
    /class="dashboard-activity-meta"[\s\S]*?:line="activityLineTagLine\(activity\)"[\s\S]*?:fallback="activityLineFallback\(activity\)"/
  )
  assert.match(
    source,
    /<dt>线路<\/dt>[\s\S]*?:line="lineTagLine\(lineForCall\(selectedCall\), selectedCall\.device_id\)"[\s\S]*?:fallback="callLineFallback\(selectedCall\)"/
  )
  assert.match(
    source,
    /class="dashboard-detail-line-tag"[\s\S]*?:line="lineTagLine\(lineForThread\(selectedThread\), selectedThread\.line_id, selectedThread\.iccid\)"[\s\S]*?:fallback="threadLineFallback\(selectedThread\)"/
  )
  assert.match(
    source,
    /callNumber\(selectedCall\.remote_number, callName\(selectedCall\), selectedCall\.device_id\)/
  )
  assert.match(
    source,
    /startMessage\(selectedThread\.peer, threadName\(selectedThread\), selectedThread\.line_id \|\| selectedThread\.iccid\)/
  )
})
