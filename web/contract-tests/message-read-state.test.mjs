import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { createMessageReadCoordinator } from '../src/state/workspace.ts'

test('message read coordinator sends one request for concurrent opens', async () => {
  let release
  const calls = []
  const coordinator = createMessageReadCoordinator(async input => {
    calls.push(input)
    await new Promise(resolve => {
      release = resolve
    })
  })
  const input = {
    line_id: ' line-main ',
    peer: '+819012345678'
  }

  const first = coordinator(input)
  const second = coordinator({
    ...input,
    line_id: 'line-main'
  })

  assert.equal(first, second)
  assert.deepEqual(calls, [input])
  release()
  await Promise.all([first, second])
})

test('Messages view waits for messages and guards each unread snapshot once', async () => {
  const source = await readFile(
    new URL('../src/views/MessagesView.vue', import.meta.url),
    'utf8'
  )
  const loadIndex = source.indexOf('const messages = await loadMessages(thread, force)')
  const markIndex = source.indexOf('await markThreadReadInView(current)')

  assert.ok(loadIndex >= 0)
  assert.ok(markIndex > loadIndex)
  assert.match(source, /selectedThread\.value\?\.unread_count \|\| 0/)
  assert.match(source, /readKey !== attemptedReadKey/)
  assert.match(source, /selectedKey\.value !== thread\.key/)
  assert.doesNotMatch(source, /\(\) => threadsResource\.status/)
  assert.match(source, /retryThreadRead/)
  assert.match(source, /selectedReadError/)
})
