import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { ThreadDraftStore } from '../src/state/threadDrafts.ts'

test('parking, taking and revisions are per thread; navigation is not an edit', () => {
  const drafts = new ThreadDraftStore()
  drafts.edited('A')
  drafts.park('A', 'hello')
  assert.equal(drafts.peek('A'), 'hello')
  assert.equal(drafts.take('A'), 'hello')
  assert.equal(drafts.peek('A'), '', 'the composer owns a taken draft')
  assert.equal(drafts.revision('A'), 1)
  assert.equal(drafts.revision('B'), 0)
  drafts.park('A', '   ')
  assert.equal(drafts.peek('A'), '', 'blank drafts are not parked')
})

test('settleSent forgets the sent version only while it is unedited', () => {
  const drafts = new ThreadDraftStore()
  drafts.edited('A')
  const sent = drafts.revision('A')
  drafts.park('A', 'sent text')
  assert.equal(drafts.settleSent('A', sent), true)
  assert.equal(drafts.peek('A'), '')

  drafts.edited('A')
  const second = drafts.revision('A')
  drafts.edited('A')
  drafts.park('A', 'typed after sending')
  assert.equal(drafts.settleSent('A', second), false)
  assert.equal(drafts.peek('A'), 'typed after sending')
  assert.equal(drafts.settleSent('', 0), false)
})

test('MessagesView parks, takes, records edits and settles through ThreadDraftStore', async () => {
  const source = await readFile(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8')
  assert.match(source, /const threadDrafts = new ThreadDraftStore\(\)/)
  assert.match(source, /watch\(selectedKey, switchThreadDraft\)/)
  assert.match(source, /watch\(draft, noteDraftEdit, \{ flush: 'sync' \}\)/)
  assert.match(
    source,
    /function startMessage\(\): void \{[\s\S]*?threadDrafts\.park\(composeReturnThreadKey, draft\.value\)[\s\S]*?composingNew\.value = true/
  )
  // Settling (live or parked) happens before the generation guard, which stays.
  assert.match(
    source,
    /threadDrafts\.settleSent\(draftThreadKey, sentRevision\)[\s\S]*?setComposerDraft\(''\)[\s\S]*?if \(generation !== composerGeneration\) return/
  )
})
