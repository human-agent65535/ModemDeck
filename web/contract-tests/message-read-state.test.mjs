import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { createMessageReadCoordinator } from '../src/state/workspace.ts'
import {
  canAcknowledgeMessageThread,
  messageViewportIsAtBottom
} from '../src/views/messages/messageReadVisibility.ts'

test('message read coordinator sends a trailing request for concurrent opens', async () => {
  const releases = []
  const calls = []
  const coordinator = createMessageReadCoordinator(async input => {
    calls.push(input)
    await new Promise(resolve => {
      releases.push(resolve)
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
  releases.shift()()
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(calls, [
    input,
    {
      ...input,
      line_id: 'line-main'
    }
  ])
  releases.shift()()
  await Promise.all([first, second])
})

test('message read eligibility requires a rendered, visible, focused bottom viewport', () => {
  const eligible = {
    selected: true,
    messagesReady: true,
    unread: true,
    composing: false,
    manuallyUnread: false,
    documentVisible: true,
    windowFocused: true,
    atBottom: true
  }
  assert.equal(canAcknowledgeMessageThread(eligible), true)
  for (const [field, value] of [
    ['selected', false],
    ['messagesReady', false],
    ['unread', false],
    ['composing', true],
    ['manuallyUnread', true],
    ['documentVisible', false],
    ['windowFocused', false],
    ['atBottom', false]
  ]) {
    assert.equal(
      canAcknowledgeMessageThread({ ...eligible, [field]: value }),
      false,
      `${field} should block read acknowledgement`
    )
  }
})

test('message viewport accepts a small bottom rounding tolerance', () => {
  assert.equal(
    messageViewportIsAtBottom({
      scrollHeight: 1000,
      scrollTop: 468,
      clientHeight: 500
    }),
    true
  )
  assert.equal(
    messageViewportIsAtBottom({
      scrollHeight: 1000,
      scrollTop: 467,
      clientHeight: 500
    }),
    false
  )
})

test('Messages view renders and positions a conversation before acknowledging it', async () => {
  const source = await readFile(
    new URL('../src/views/MessagesView.vue', import.meta.url),
    'utf8'
  )
  const openStart = source.indexOf('async function openThread(')
  const openEnd = source.indexOf(
    'async function acknowledgeSelectedThreadRead(',
    openStart
  )
  const openThread = source.slice(openStart, openEnd)
  const loadIndex = openThread.indexOf(
    'const messages = await loadMessages(thread, true)'
  )
  const renderIndex = openThread.indexOf('await nextTick()', loadIndex)
  const scrollIndex = openThread.indexOf(
    'scrollMessageViewportToEnd()',
    renderIndex
  )
  const markIndex = openThread.indexOf(
    'await acknowledgeSelectedThreadRead(true)',
    scrollIndex
  )

  assert.ok(loadIndex >= 0)
  assert.ok(renderIndex > loadIndex)
  assert.ok(scrollIndex > renderIndex)
  assert.ok(markIndex > loadIndex)
  assert.ok(markIndex > scrollIndex)
  assert.match(source, /selectedKey\.value !== thread\.key/)
  assert.match(source, /document\.visibilityState === 'visible'/)
  assert.match(source, /document\.hasFocus\(\)/)
  assert.match(source, /messageViewportIsAtBottom\(viewport\)/)
  assert.match(source, /@scroll="onMessagesScroll"/)
  assert.doesNotMatch(source, /attemptedReadKey/)
  assert.doesNotMatch(source, /openedThreadKey/)
  assert.match(source, /retryThreadRead/)
  assert.match(source, /selectedReadError/)
  assert.match(
    source,
    /if \(!alreadyManual\) manuallyUnreadThreadKeys\.value\.delete\(thread\.key\)/
  )
  assert.match(source, /for \(const key of insertedManualKeys\)/)
})
