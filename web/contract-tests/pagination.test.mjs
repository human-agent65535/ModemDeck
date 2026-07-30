import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseMessages } from '../src/api/normalize.ts'

test('list responses require a consistent continuation cursor', () => {
  const page = parseMessages({
    messages: [{
      id: 2,
      line_id: 'line-main',
      peer: '+810000000001',
      content: 'latest',
      type: 1,
      status: 0,
      timestamp: '2026-07-30T02:00:00Z'
    }],
    meta: {
      limit: 1,
      next_cursor: 'opaque-next-page',
      has_more: true
    }
  })
  assert.equal(page.items[0]?.id, '2')
  assert.equal(page.meta.next_cursor, 'opaque-next-page')
  assert.throws(
    () =>
      parseMessages({
        messages: [],
        meta: { limit: 50, next_cursor: '', has_more: true }
      }),
    /has_more/
  )
})

test('fixture cursor pages do not repeat list rows and messages load older first', async () => {
  const gateway = createFixtureGateway()

  const contacts = await gateway.listContacts({ limit: 1 })
  assert.equal(contacts.items.length, 1)
  assert.equal(contacts.meta.has_more, true)
  const nextContacts = await gateway.listContacts({
    limit: 1,
    cursor: contacts.meta.next_cursor
  })
  assert.notEqual(nextContacts.items[0]?.id, contacts.items[0]?.id)

  const threads = await gateway.listThreads({ limit: 1 })
  const thread = threads.items[0]
  assert.ok(thread)
  const newestMessages = await gateway.listMessages({
    line_id: thread.line_id,
    peer: thread.peer,
    limit: 1
  })
  if (newestMessages.meta.has_more) {
    const olderMessages = await gateway.listMessages({
      line_id: thread.line_id,
      peer: thread.peer,
      limit: 1,
      cursor: newestMessages.meta.next_cursor
    })
    assert.ok(
      Date.parse(olderMessages.items[0].timestamp) <=
        Date.parse(newestMessages.items[0].timestamp)
    )
    assert.notEqual(olderMessages.items[0].id, newestMessages.items[0].id)
  }
})

test('communication lists share automatic infinite loading and preserve message scroll position', async () => {
  const [contacts, messages, calls, recordings] = await Promise.all([
    readFile(new URL('../src/views/ContactsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/CallsView.vue', import.meta.url), 'utf8'),
    readFile(new URL('../src/views/RecordingsView.vue', import.meta.url), 'utf8')
  ])
  for (const source of [contacts, messages, calls, recordings]) {
    assert.match(source, /InfiniteScrollTrigger/)
  }
  assert.match(messages, /@load="loadOlderMessages"/)
  assert.match(
    messages,
    /previousTop \+ currentViewport\.scrollHeight - previousHeight/
  )
})
