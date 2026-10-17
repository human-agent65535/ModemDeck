import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick } from 'vue'
import { messageComposeRoute, messageThreadRoute } from '../src/router/messageRoute.ts'
import { messagesFor, refreshThreads, refreshMessages } from '../src/state/workspace.ts'
import { flushView, mountMessagesView } from './helpers/messages-view.mjs'

test('a normal send clears and positions its message even if refreshing threads fails', async t => {
  const c = await mountMessagesView(t)
  c.readOlder()
  c.network.failThreads = true
  c.state.draft = 'accepted SMS'
  const pending = c.state.submit()
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.state.draft, '')
  assert.equal(c.state.sending, false)
  assert.equal(c.state.sendError, '')
  assert.equal(messagesFor('A').data.at(-1).content, 'accepted SMS')
  assert.equal(c.viewport.scrollTop, c.viewport.scrollHeight)
})

test('a thread refresh preserves its reading position and does not reopen the conversation', async t => {
  const c = await mountMessagesView(t)
  c.readOlder()
  const count = c.requests.length
  await refreshThreads(); await flushView()
  assert.equal(c.requests.length, count, 'replacing list objects must not reopen the selected thread')
  assert.equal(c.viewport.scrollTop, 100)
  await refreshMessages(c.threads[0]); await flushView()
  assert.equal(c.viewport.scrollTop, 100, 'background messages preserve a reader’s position')
  c.viewport.scrollTop = 1000; c.state.onMessagesScroll()
  await refreshMessages(c.threads[0]); await flushView()
  assert.equal(c.viewport.scrollTop, 1000)
})

test('A sends, navigates to B and back before success: no old draft and no forced scroll', async t => {
  const c = await mountMessagesView(t)
  c.state.draft = 'first'
  const pending = c.state.submit()
  await c.open('B')
  c.state.draft = 'B draft'
  await c.open('A')
  assert.equal(c.state.draft, 'first')
  c.readOlder()
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.state.draft, '')
  assert.equal(c.viewport.scrollTop, 100)
  await c.open('B'); assert.equal(c.state.draft, 'B draft')
  await c.open('A'); assert.equal(c.state.draft, '')
  c.state.startMessage(); await flushView()
  c.state.backToList(); await flushView()
  assert.equal(c.state.selectedKey, 'A')
  assert.equal(c.state.draft, '')
})

test('a newer draft survives send completion after a round trip, including edit then undo', async t => {
  const c = await mountMessagesView(t)
  for (const replacement of ['newer', 'first']) {
    c.state.draft = 'first'
    const pending = c.state.submit()
    await c.open('B'); await c.open('A')
    c.state.draft = 'temporary edit'; c.state.draft = replacement
    c.readOlder()
    c.sends.at(-1).complete()
    await pending; await flushView()
    assert.equal(c.state.draft, replacement)
    assert.equal(c.viewport.scrollTop, 100)
  }
})

test('a failed A send cannot surface in B, and its unedited A draft remains retryable', async t => {
  const c = await mountMessagesView(t)
  c.state.draft = 'first'
  const pending = c.state.submit()
  await c.open('B'); c.state.draft = 'B draft'
  c.sends[0].reject(new Error('synthetic send failure'))
  await pending; await flushView()
  assert.equal(c.state.sendError, '')
  assert.equal(c.state.draft, 'B draft')
  await c.open('A')
  assert.equal(c.state.draft, 'first')
  assert.ok(c.state.sendError)
  c.state.draft = 'edited'
  assert.equal(c.state.sendError, '')
})

test('new-compose cancellation and browser history never copy text between drafts', async t => {
  const c = await mountMessagesView(t)
  c.state.draft = 'A draft'
  c.state.startMessage(); await flushView()
  assert.equal(c.state.composingNew, true)
  c.state.draft = 'new draft'
  c.router.back(); await flushView()
  assert.equal(c.state.selectedKey, 'A')
  assert.equal(c.state.draft, 'A draft')
  c.router.forward(); await flushView()
  assert.equal(c.state.composingNew, true)
  assert.equal(c.state.draft, '')
  c.state.backToList(); await flushView()
  assert.equal(c.state.draft, 'A draft')
})

test('a successful new send navigates to its conversation without reusing the composer draft', async t => {
  const c = await mountMessagesView(t)
  c.state.startMessage(); await flushView()
  c.state.editRecipient(c.threads[0].peer)
  c.state.draft = 'new SMS'
  await flushView()
  const pending = c.state.submit()
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.state.selectedKey, 'A')
  assert.equal(c.state.composingNew, false)
  assert.equal(c.state.draft, '')
  assert.equal(c.viewport.scrollTop, 1000)
})

test('editing a new-message destination prevents the old completion from clearing or navigating', async t => {
  const c = await mountMessagesView(t)
  await c.router.push(messageComposeRoute({ recipient: c.threads[0].peer, name: '', lineKey: '' }))
  await flushView()
  c.state.draft = 'body'
  const pending = c.state.submit()
  c.state.editRecipient(c.threads[1].peer)
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.state.composingNew, true)
  assert.equal(c.state.newRecipient, c.threads[1].peer)
  assert.equal(c.state.draft, 'body')
})

test('input typed while post-send navigation waits belongs to the destination draft', async t => {
  const c = await mountMessagesView(t)
  c.state.startMessage(); await flushView()
  c.state.editRecipient(c.threads[0].peer)
  c.state.draft = 'first'
  await flushView()
  let releaseNavigation
  const stop = c.router.beforeResolve(to => to.params.threadRef
    ? new Promise(resolve => { releaseNavigation = resolve }) : undefined)
  const pending = c.state.submit()
  c.sends[0].complete(); await flushView()
  assert.ok(releaseNavigation)
  assert.equal(c.state.selectedKey, 'A')
  c.state.draft = 'next, while navigating'
  releaseNavigation(); await pending; await flushView()
  assert.equal(c.state.draft, 'next, while navigating')
  stop()
})

test('cancelling a pending compose cannot close or clear its replacement', async t => {
  const c = await mountMessagesView(t)
  c.state.startMessage(); await flushView()
  c.state.editRecipient(c.threads[0].peer)
  c.state.draft = 'first'; await flushView()
  const pending = c.state.submit()
  c.state.backToList(); await flushView()
  c.state.startMessage(); await flushView()
  c.state.editRecipient(c.threads[1].peer)
  c.state.draft = 'replacement'
  c.sends[0].complete(); await pending; await flushView()
  assert.equal(c.state.composingNew, true)
  assert.equal(c.state.newRecipient, c.threads[1].peer)
  assert.equal(c.state.draft, 'replacement')
})

test('Dashboard embedded conversations preserve drafts and scope delayed errors to their owner', async t => {
  const c = await mountMessagesView(t, { embeddedThreadKey: 'A' })
  c.state.draft = 'A draft'
  const pending = c.state.submit()
  c.props.embeddedThreadKey = 'B'; await flushView()
  c.state.draft = 'B draft'
  c.sends[0].reject(new Error('failure'))
  await pending; await flushView()
  assert.equal(c.state.draft, 'B draft')
  assert.equal(c.state.sendError, '')
  c.props.embeddedThreadKey = 'A'; await flushView()
  assert.equal(c.state.draft, 'A draft')
  assert.ok(c.state.sendError)
})

test('an embedded compose emits completion only for its still-current input', async t => {
  const c = await mountMessagesView(t, { embeddedCompose: true, initialRecipient: '+12025550101' })
  c.state.draft = 'first'
  const pending = c.state.submit()
  c.state.draft = 'next'
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.state.draft, 'next')
  assert.deepEqual(c.events, [])
  const second = c.state.submit()
  c.sends[1].complete()
  await second; await flushView()
  assert.deepEqual(c.events, [['sent', 'A']])
})

test('an embedded recipient display-name refresh does not replace an edited draft', async t => {
  const c = await mountMessagesView(t, {
    embeddedCompose: true, initialRecipient: '+12025550101', initialRecipientName: 'Before'
  })
  c.state.draft = 'keep this'
  c.props.initialRecipientName = 'After'; await flushView()
  assert.equal(c.state.draft, 'keep this')
})

test('an unmounted composer and a late conversation load cannot move the replacement view', async t => {
  const c = await mountMessagesView(t)
  let release
  c.network.loadMessages = () => new Promise(resolve => { release = resolve })
  await c.router.push(messageThreadRoute('B')); await nextTick()
  c.network.loadMessages = null
  await c.open('A'); c.readOlder()
  release({ items: [], meta: c.meta }); await flushView()
  assert.equal(c.state.selectedKey, 'A')
  assert.equal(c.viewport.scrollTop, 100)
  c.state.draft = 'last'
  const pending = c.state.submit()
  c.unmount()
  c.sends[0].complete()
  await pending; await flushView()
  assert.equal(c.viewport.scrollTop, 100)
  assert.deepEqual(c.events, [])
})
