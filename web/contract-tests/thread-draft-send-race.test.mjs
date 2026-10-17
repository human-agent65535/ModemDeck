import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import { nextTick, ref, watch } from 'vue'
import { ThreadDraftStore } from '../src/state/threadDrafts.ts'

// Runs the real MessagesView submit and draft handlers with real Vue refs and
// watchers wired the way the component wires them.
function messagesViewFunctions(names) {
  const source = readFileSync(new URL('../src/views/MessagesView.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)?.[1] || ''
  const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true)
  const found = ast.statements
    .filter(node => ts.isFunctionDeclaration(node) && names.includes(node.name?.text))
    .map(node => node.getText(ast))
  assert.equal(found.length, names.length, 'MessagesView handlers exist')
  return found.join('\n')
}

function mountComposer() {
  let complete
  const selectedKey = ref('A')
  const draft = ref('')
  const composingNew = ref(false)
  const context = vm.createContext({
    draft, selectedKey, composingNew,
    threadDrafts: new ThreadDraftStore(),
    restoringDraft: false,
    composerGeneration: 0,
    sending: ref(false), sendError: ref(''), sendDisabledReason: ref(''),
    replyThreadKey: ref('A'), activeLineID: ref('line-a'), activeRecipient: ref('+12025550101'),
    sendMessage: () => new Promise(resolve => { complete = resolve }),
    props: { embeddedCompose: false }, emit() {}, t: key => key
  })
  const source = messagesViewFunctions(['setComposerDraft', 'switchThreadDraft', 'noteDraftEdit', 'submit'])
  vm.runInContext(
    ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText,
    context
  )
  watch(selectedKey, context.switchThreadDraft)
  watch(draft, context.noteDraftEdit, { flush: 'sync' })
  watch(draft, () => { context.composerGeneration += 1 }, { flush: 'sync' })
  return {
    context, selectedKey, draft,
    async open(key) { selectedKey.value = key; await nextTick() },
    type(text) { draft.value = text },
    complete: () => complete({ thread: { key: 'A' } })
  }
}

test('A sends, user visits B and returns to A before success: the composer is empty', async () => {
  const c = mountComposer()
  c.type('hello from A')
  const pending = c.context.submit()
  await c.open('B')
  await c.open('A')
  assert.equal(c.draft.value, 'hello from A', 'unsent while pending')
  c.complete()
  await pending
  assert.equal(c.draft.value, '', 'the sent text is cleared from the visible composer')
  await c.open('B')
  await c.open('A')
  assert.equal(c.draft.value, '', 'and never restored later')
})

test('same flow but a new draft typed after returning is kept', async () => {
  const c = mountComposer()
  c.type('hello from A')
  const pending = c.context.submit()
  await c.open('B')
  await c.open('A')
  c.type('a newer draft')
  c.complete()
  await pending
  assert.equal(c.draft.value, 'a newer draft')
  await c.open('B')
  await c.open('A')
  assert.equal(c.draft.value, 'a newer draft')
})

test('success while the user is still on B forgets the parked sent text', async () => {
  const c = mountComposer()
  c.type('hello from A')
  const pending = c.context.submit()
  await c.open('B')
  c.complete()
  await pending
  await c.open('A')
  assert.equal(c.draft.value, '')
})
