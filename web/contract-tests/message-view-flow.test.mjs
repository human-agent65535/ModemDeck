import assert from 'node:assert/strict'
import test from 'node:test'
import {
  findRecipientThread,
  messageReturnRoute,
  messageThreadUsesLine
} from '../src/views/messages/messageFlow.ts'

const mainLine = {
  id: 'line-main',
  iccid: '898601',
  imsi: '46001',
  phone_number: '',
  operator: 'China Unicom',
  device_imei: 'imei-main',
  device_alias: '',
  line_label: '主卡'
}

const secondaryLine = {
  ...mainLine,
  id: 'line-secondary',
  iccid: '898602',
  imsi: '00102',
  device_imei: 'imei-secondary',
  line_label: '副卡'
}

const mainThread = {
  key: '898601|+819012345678',
  imsi: '46001',
  iccid: '898601',
  line_id: 'line-main',
  peer: '+1 202 555 0103',
  last_timestamp: '2026-07-24T01:00:00Z',
  unread_count: 0
}

const secondaryThread = {
  ...mainThread,
  key: '898602|+819012345678',
  iccid: '898602',
  line_id: 'imei-secondary'
}

test('message conversations match every stable identity of their line', () => {
  assert.equal(messageThreadUsesLine(mainThread, mainLine), true)
  assert.equal(messageThreadUsesLine(secondaryThread, secondaryLine), true)
  assert.equal(messageThreadUsesLine(mainThread, secondaryLine), false)
})

test('recipient lookup is phone-format tolerant and remains line specific', () => {
  const threads = [mainThread, secondaryThread]

  assert.equal(
    findRecipientThread(threads, '+81 (90) 1234-5678', mainLine)?.key,
    mainThread.key
  )
  assert.equal(
    findRecipientThread(threads, '819012345678', secondaryLine)?.key,
    secondaryThread.key
  )
  assert.equal(findRecipientThread(threads, '+81 80 0000 0000', mainLine), undefined)
  assert.equal(findRecipientThread(threads, '+1 202 555 0103'), undefined)
})

test('leaving compose restores its originating conversation when available', () => {
  assert.deepEqual(messageReturnRoute(mainThread.key), {
    name: 'messages',
    params: { threadKey: mainThread.key }
  })
  assert.deepEqual(messageReturnRoute(''), { name: 'messages' })
})
