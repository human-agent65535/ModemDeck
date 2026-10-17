import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { ThreadDraftStore } from '../src/state/threadDrafts.ts'

const view = () => readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8')

// A minimal model of the MessagesView composer wiring, driving the real store.
function composer() {
  const drafts = new ThreadDraftStore()
  const state = { selected: '', composingNew: false, draft: '' }
  return {
    drafts,
    state,
    open(key) {
      if (!state.composingNew && state.selected) drafts.park(state.selected, state.draft)
      state.selected = key
      state.draft = drafts.take(key)
    },
    startNew() {
      if (!state.composingNew) drafts.park(state.selected, state.draft)
      state.composingNew = true
      state.selected = ''
      state.draft = ''
    },
    cancelNew(returnKey) {
      state.composingNew = false
      state.selected = returnKey
      state.draft = drafts.take(returnKey)
    },
    beginSend() {
      return { key: state.composingNew ? '' : state.selected, text: state.draft }
    },
    sendSucceeded(sent, { stillUnchanged = true } = {}) {
      drafts.settleSent(sent.key, sent.text)
      if (stillUnchanged) state.draft = ''
    }
  }
}

test('a sent message is not restored after new-message compose is cancelled', () => {
  const c = composer()
  c.open('A')
  c.state.draft = 'hello from A'
  c.open('B')
  c.open('A')
  assert.equal(c.state.draft, 'hello from A')
  const sent = c.beginSend()
  c.sendSucceeded(sent)
  assert.equal(c.state.draft, '')
  c.startNew()
  c.cancelNew('A')
  assert.equal(c.state.draft, '', 'sent text must not come back')
})

test('leaving a thread while it sends forgets the sent text but keeps later drafts', () => {
  const c = composer()
  c.open('A')
  c.state.draft = 'first'
  const sent = c.beginSend()
  c.open('B') // user leaves before the send completes; "first" is parked
  c.sendSucceeded(sent, { stillUnchanged: false })
  assert.equal(c.drafts.peek('A'), '')

  c.open('A')
  c.state.draft = 'second'
  const another = c.beginSend()
  c.state.draft = 'third, typed while sending'
  c.open('B')
  c.sendSucceeded(another, { stillUnchanged: false })
  assert.equal(c.drafts.peek('A'), 'third, typed while sending')
})

test('an edited draft is parked over an older one when composing new', () => {
  const c = composer()
  c.open('A')
  c.state.draft = 'old'
  c.open('B')
  c.open('A')
  c.state.draft = 'edited'
  c.startNew()
  c.cancelNew('A')
  assert.equal(c.state.draft, 'edited')
})

test('MessagesView parks, takes and settles drafts through ThreadDraftStore', async () => {
  const source = await view()
  assert.match(source, /const threadDrafts = new ThreadDraftStore\(\)/)
  assert.match(
    source,
    /watch\(selectedKey, \(next, previous\) => \{[\s\S]*?if \(composingNew\.value\) return[\s\S]*?threadDrafts\.park\(previous, draft\.value\)[\s\S]*?threadDrafts\.take\(next\)/
  )
  assert.match(
    source,
    /function startMessage\(\): void \{[\s\S]*?threadDrafts\.park\(composeReturnThreadKey, draft\.value\)[\s\S]*?composingNew\.value = true/
  )
  // Settling must happen before the "user typed meanwhile" early return.
  assert.match(
    source,
    /threadDrafts\.settleSent\(draftThreadKey, sentText\)\s*\n\s*if \(generation !== composerGeneration\) return/
  )
})
