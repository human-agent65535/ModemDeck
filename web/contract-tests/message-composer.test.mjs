import assert from 'node:assert/strict'
import test from 'node:test'
import { useMessageComposer } from '../src/composables/useMessageComposer.ts'

const target = { thread_key: 'A', line_id: 'line-a', to: '+12025550101' }
function sendingComposer() {
  const composer = useMessageComposer()
  composer.openThread('A')
  composer.text.value = 'first'
  return { composer, send: composer.beginSend(target) }
}

test('a draft has one owner across thread changes, composing new and cancellation', () => {
  const c = useMessageComposer()
  c.openThread('A'); c.text.value = 'old'
  c.openThread('B'); c.text.value = 'B draft'
  c.openThread('A'); c.text.value = 'edited'
  c.openCompose('new'); c.text.value = 'discard this'
  c.openThread('A')
  assert.equal(c.text.value, 'edited')
  c.openThread('B')
  assert.equal(c.text.value, 'B draft')
  c.openCompose('new')
  assert.equal(c.text.value, '')
})

test('success clears only its draft, wherever it is displayed, without reclaiming a later visit', () => {
  for (const returnBeforeSuccess of [false, true]) {
    const { composer: c, send } = sendingComposer()
    c.openThread('B'); c.text.value = 'B draft'
    if (returnBeforeSuccess) c.openThread('A')
    assert.equal(c.succeed(send), false, 'navigation ends the send’s presentation ownership')
    c.openThread('A')
    assert.equal(c.text.value, '')
    c.openThread('B')
    assert.equal(c.text.value, 'B draft')
    c.openThread('A'); c.openCompose('new'); c.openThread('A')
    assert.equal(c.text.value, '', 'accepted text cannot reappear after composing new')
  }
})

test('normal success can still present after clearing, and settles only once', () => {
  const { composer: c, send } = sendingComposer()
  assert.equal(c.succeed(send), true)
  assert.equal(c.text.value, '')
  assert.equal(c.isCurrent(send), true, 'clearing is not a user edit')
  assert.equal(c.sending.value, false)
  c.text.value = 'next'
  assert.equal(c.isCurrent(send), false)
  assert.equal(c.succeed(send), false)
  c.fail(send, 'late error')
  assert.equal(c.text.value, 'next')
  assert.equal(c.error.value, '')
})

test('later edits are preserved even when their final text equals the sent text', () => {
  for (const replacement of ['next', 'first']) {
    const { composer: c, send } = sendingComposer()
    c.openThread('B'); c.openThread('A')
    c.text.value = 'intermediate'
    c.text.value = replacement
    c.openThread('B')
    assert.equal(c.succeed(send), false)
    c.openThread('A')
    assert.equal(c.text.value, replacement)
  }
})

test('new-message destination edits invalidate both clearing and completion effects', () => {
  for (const field of ['recipient', 'lineID']) {
    const c = useMessageComposer()
    c.openCompose('new', target.to)
    c.lineID.value = target.line_id
    c.text.value = 'body'
    const send = c.beginSend({ to: c.recipient.value, line_id: c.lineID.value })
    c[field].value = field === 'recipient' ? '+12025550102' : 'line-b'
    assert.equal(c.succeed(send), false)
    assert.equal(c.text.value, 'body')
    assert.equal(send.input.to, target.to)
    assert.equal(send.input.line_id, target.line_id)
  }
})

test('failure belongs to the original unedited draft, not the currently open conversation', () => {
  const { composer: c, send } = sendingComposer()
  c.openThread('B')
  assert.equal(c.beginSend(target), null, 'another view cannot duplicate the pending send')
  c.fail(send, 'send failed')
  assert.equal(c.error.value, '')
  assert.equal(c.sending.value, false)
  c.openThread('A')
  assert.equal(c.text.value, 'first')
  assert.equal(c.error.value, 'send failed')
  c.text.value = 'edited'
  assert.equal(c.error.value, '')
  const retry = c.beginSend(target)
  c.text.value = 'newer'
  c.fail(retry, 'old error')
  assert.equal(c.text.value, 'newer')
  assert.equal(c.error.value, '')
})

test('cancelled compose, deleted threads and disposed pages cannot be revived by late results', () => {
  const c = useMessageComposer()
  c.openCompose('same-route', target.to); c.text.value = 'old compose'
  const send = c.beginSend(target)
  c.close(); c.openCompose('same-route', target.to); c.text.value = 'new compose'
  assert.equal(c.succeed(send), false)
  assert.equal(c.text.value, 'new compose')
  c.openThread('A'); c.text.value = 'deleted'
  const deleted = c.beginSend(target)
  c.forgetThread('A'); c.openThread('A')
  c.fail(deleted, 'old error')
  assert.equal(c.text.value, '')
  assert.equal(c.error.value, '')
  c.text.value = 'last'
  const disposed = c.beginSend(target)
  c.dispose()
  assert.equal(c.succeed(disposed), false)
  assert.equal(c.isCurrent(disposed), false)
})
