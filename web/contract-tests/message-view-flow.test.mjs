import assert from 'node:assert/strict'
import test from 'node:test'
import {
  findRecipientThread,
  messageReturnRoute,
  messageThreadUsesLine
} from '../src/views/messages/messageFlow.ts'
import { messageThreadKeyFromReference } from '../src/router/messageRoute.ts'

const mainLine = {
  id: 'line-main',
  iccid: '898601',
  imsi: '46001',
  phone_number: '+1 202 555 0101',
  operator: 'Aurora Mobile',
  device_imei: 'imei-main',
  device_name: '',
  line_label: 'Line A'
}

const secondaryLine = {
  ...mainLine,
  id: 'line-secondary',
  iccid: '898602',
  imsi: '00102',
  phone_number: '+1 202 555 0102',
  device_imei: 'imei-secondary',
  line_label: 'Line B'
}

const mainThread = {
  key: 'backend-main-thread',
  line_id: 'line-main',
  peer: '+1 202 555 0103',
  last_timestamp: '2026-07-24T01:00:00Z',
  unread_count: 0
}

const secondaryThread = {
  ...mainThread,
  key: 'backend-secondary-thread',
  line_id: 'line-secondary'
}

test('message conversations match only the stable line id', () => {
  assert.equal(messageThreadUsesLine(mainThread, mainLine), true)
  assert.equal(messageThreadUsesLine(secondaryThread, secondaryLine), true)
  assert.equal(messageThreadUsesLine(mainThread, secondaryLine), false)
})

test('recipient lookup uses backend-canonical global identity and remains line specific', () => {
  const threads = [mainThread, secondaryThread]

  assert.equal(
    findRecipientThread(threads, '+1 (202) 555-0103', mainLine)?.key,
    mainThread.key
  )
  assert.equal(
    findRecipientThread(threads, '+1 202 555 0103', secondaryLine)?.key,
    secondaryThread.key
  )
  assert.equal(findRecipientThread(threads, '12025550103', secondaryLine), undefined)
  assert.equal(findRecipientThread(threads, '+1 202 555 0199', mainLine), undefined)
  assert.equal(findRecipientThread(threads, '+1 202 555 0103'), undefined)
})

test('leaving compose restores its originating conversation when available', () => {
  const route = messageReturnRoute(mainThread.key)
  assert.equal(route.name, 'messages')
  assert.match(route.params.threadRef, /^m_[a-f0-9]{16}$/)
  assert.equal(
    messageThreadKeyFromReference(route.params.threadRef),
    mainThread.key
  )
  assert.deepEqual(messageReturnRoute(''), { name: 'messages' })
})
