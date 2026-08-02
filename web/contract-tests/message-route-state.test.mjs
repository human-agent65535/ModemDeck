import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseCallRecord, parseThread } from '../src/api/normalize.ts'
import {
  messageThreadReference,
  visibleMessageThreadKey
} from '../src/router/messageRoute.ts'

test('the visible message thread is derived from the router', () => {
  const threadKey = 'line-main|+12025550103'
  const reference = messageThreadReference(threadKey)

  assert.equal(
    visibleMessageThreadKey({
      name: 'messages',
      params: { threadRef: reference },
      query: {}
    }),
    threadKey
  )
  assert.equal(
    visibleMessageThreadKey({
      name: 'dashboard',
      params: {},
      query: { item: `message:${reference}` }
    }),
    threadKey
  )
  assert.equal(
    visibleMessageThreadKey({ name: 'calls', params: {}, query: {} }),
    ''
  )
  assert.equal(
    visibleMessageThreadKey({
      name: 'dashboard',
      params: {},
      query: { item: 'message:invalid' }
    }),
    ''
  )
})

test('thread parsing preserves stable identity and the latest message cursor', () => {
  const thread = parseThread({
    key: 'backend-thread-key',
    line_id: 'line-main',
    local_phone: '+1 202 555 0101',
    imsi: '001010000000001',
    iccid: '8986012345678900001',
    peer: '+1 202 555 0103',
    last_message_id: 42,
    last_timestamp: '2026-07-24T12:00:00Z',
    unread_count: 1,
    marked_unread: false,
    favorite: true
  })

  assert.equal(thread.key, 'backend-thread-key')
  assert.equal(thread.line_id, 'line-main')
  assert.equal(thread.peer, '+1 202 555 0103')
  assert.equal(thread.last_message_id, '42')
  assert.equal(thread.marked_unread, false)
  assert.equal(thread.favorite, true)
  assert.equal('local_phone' in thread, false)
  assert.equal('imsi' in thread, false)
  assert.equal('iccid' in thread, false)
})

test('call parsing preserves stable historical line identities', () => {
  const call = parseCallRecord({
    id: 'call-history',
    line_id: 'line-history',
    endpoint_line_id: 'retired-modem',
    local_phone: '+12025550198',
    line_iccid: '8986012345678900001',
    line_imsi: '001010000000001',
    direction: 'outgoing',
    remote_number: '+12025550199',
    started_at: '2026-07-24T12:00:00Z',
    duration_seconds: 4,
    missed: false,
    read: true,
    favorite: true
  })

  assert.equal(call.line_id, 'line-history')
  assert.equal(call.read, true)
  assert.equal(call.favorite, true)
  assert.equal('local_phone' in call, false)
  assert.equal('line_iccid' in call, false)
  assert.equal('line_imsi' in call, false)
  assert.equal('endpoint_line_id' in call, false)
})

test('message queries and reads use only stable line id and peer', async () => {
  const client = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8')
  assert.match(
    client,
    /line_id: query\.line_id,[\s\S]*?peer: query\.peer/
  )
  assert.doesNotMatch(client, /local_phone: query\.local_phone|iccid: query\.iccid/)

  const gateway = createFixtureGateway()
  const thread = (await gateway.listThreads()).items.find(
    item => item.contact_name === 'Alex Rowan'
  )
  assert.ok(thread)
  const messages = await gateway.listMessages({
    line_id: thread.line_id,
    peer: thread.peer
  })
  assert.ok(messages.items.length > 0)

  await gateway.markThreadRead({
    line_id: thread.line_id,
    peer: thread.peer
  })
  const updated = (await gateway.listThreads()).items.find(item => item.key === thread.key)
  assert.equal(updated?.unread_count, 0)
})

test('sent messages reconcile against refreshed backend threads without making a key', async () => {
  const workspace = await readFile(
    new URL('../src/state/workspace.ts', import.meta.url),
    'utf8'
  )

  assert.match(workspace, /const threads = await refreshThreads\(\)/)
  assert.match(workspace, /findThreadForSentMessage\(threads \|\| threadsResource\.data/)
  assert.doesNotMatch(workspace, /`\$\{sent\.iccid\}\|\$\{sent\.peer\}`/)
})
