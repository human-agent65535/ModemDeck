import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { contactsResource, fetchAllContacts } from '../src/state/workspace.ts'

function functions(file, names) {
  const source = readFileSync(new URL('../src/' + file, import.meta.url), 'utf8')
  const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)?.[1] || source
  const ast = ts.createSourceFile('test.ts', script, ts.ScriptTarget.Latest, true)
  return ast.statements.filter(node => ts.isFunctionDeclaration(node) && names.includes(node.name?.text))
    .map(node => node.getText(ast)).join('\n')
}

function evaluate(source, bindings = {}) {
  const context = vm.createContext(bindings)
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  return context
}
const ref = value => ({ value })

test('microphone replacement stays silent until installed and preserves the latest mute choice', async () => {
  for (const initialMute of [true, false]) {
    const raw = { enabled: true }, outgoing = { kind: 'audio', enabled: true }
    const replacement = { getAudioTracks: () => [raw] }
    const state = { muted: initialMute }
    const context = evaluate(functions('state/callMedia.ts', ['replaceCallInput']), {
      peer: { getSenders: () => [{ track: { kind: 'audio' }, replaceTrack: async track => {
        assert.equal(track.enabled, false, 'no audio leaks during replacement')
        if (!initialMute) state.muted = true
      } }] },
      localStream: {}, microphonePipeline: {}, currentCallID: 'call-test', inputReplaceGeneration: 0,
      navigator: { mediaDevices: { getUserMedia: async () => replacement } },
      selectedAudioInputConstraints: () => ({}), createMicrophonePipeline: async () => ({ track: outgoing }),
      stopMicrophonePipeline() {}, markAudioInputSwitching() {}, markAudioInputActive() {},
      refreshAudioDevices: async () => {}, markAudioInputError(error) { assert.fail(error) },
      mediaError: error => String(error), translate: key => key, callMediaState: state
    })
    await context.replaceCallInput('replacement')
    assert.equal(state.muted, true)
    assert.equal(raw.enabled, false)
    assert.equal(outgoing.enabled, false)
  }
})

test('send completion keeps a newer draft and does not redirect its composer', async () => {
  let complete
  const draft = ref('message A')
  const context = evaluate(functions('views/MessagesView.vue', ['submit']), {
    draft, composerGeneration: 0, sending: ref(false), sendError: ref(''), sendDisabledReason: ref(''),
    replyThreadKey: ref('thread-a'), activeLineID: ref('line-a'), activeRecipient: ref('+12025550101'),
    sendMessage: input => { assert.equal(input.content, 'message A'); return new Promise(resolve => { complete = resolve }) },
    props: { embeddedCompose: true }, emit() { assert.fail('must not close the edited composer') },
    composingNew: ref(false), scrollToEnd() { assert.fail('must not move a newer conversation') }, t: key => key
  })
  const pending = context.submit()
  draft.value = 'message B'
  context.composerGeneration += 1
  complete({ thread: { key: 'thread-a' } })
  await pending
  assert.equal(draft.value, 'message B')
  assert.equal(context.sending.value, false)
})

test('contact transfers fetch every page without resetting the visible contact list', async () => {
  const fixture = createFixtureGateway()
  const item = (await fixture.listContacts({ limit: 1 })).items[0]
  const all = Array.from({ length: 151 }, (_, i) => ({ ...item, id: 'contact-' + i }))
  const original = gateway.listContacts
  contactsResource.data = [all[140]]
  const requests = []
  try {
    gateway.listContacts = async query => {
      requests.push(query)
      const offset = Number(query.cursor || 0)
      return { items: all.slice(offset, offset + 100), meta: {
        limit: 100, has_more: offset === 0, next_cursor: offset === 0 ? '100' : ''
      } }
    }
    assert.equal((await fetchAllContacts()).length, 151)
    assert.equal(requests.length, 2)
    assert.deepEqual(contactsResource.data.map(contact => contact.id), ['contact-140'])
    gateway.listContacts = async () => ({ items: [], meta: { has_more: true, next_cursor: 'same' } })
    await assert.rejects(fetchAllContacts())
    gateway.listContacts = async () => { throw new Error('page unavailable') }
    await assert.rejects(fetchAllContacts(), /page unavailable/)
  } finally { gateway.listContacts = original }
})

test('switching APN devices clears the draft and an outstanding confirmation cannot change its target', async () => {
  const names = ['selectLine', 'resetLineServices', 'editProfile', 'saveProfile', 'isCurrentLineServiceRequest']
  const bindings = { lineServiceGeneration: 0, t: key => key, activateNetworkSelection() {},
    loadActiveLineService: async () => {}, loadProfiles: async () => {}, showSuccess() {}, showError() {} }
  for (const key of ['selectedLineID', 'requestedDeviceLineID', 'simStatus', 'simLoadStatus', 'simError', 'profiles',
    'profileLoadStatus', 'profileError', 'ussdStatus', 'ussdLoadStatus', 'ussdError', 'ussdResult', 'editingProfileID',
    'profileName', 'profileAPN', 'profileIPFamily', 'profileUser', 'profilePassword', 'profilePending', 'deviceDetailOpen']) {
    bindings[key] = ref('')
  }
  bindings.selectedLineID.value = 'line-a'
  bindings.selectDeviceConfiguration = id => { bindings.selectedLineID.value = id }
  let confirm
  bindings.requestConfirmation = () => new Promise(resolve => { confirm = resolve })
  bindings.gateway = { saveConnectionProfile() { assert.fail('stale APN draft must never be sent') } }
  const context = evaluate(functions('components/DeviceConfigurationPanel.vue', names), bindings)
  context.editProfile({ profile_id: 7, apn: 'a.private', user: 'a-user' })
  const pending = context.saveProfile()
  context.selectLine({ id: 'line-b' }, false)
  assert.equal(bindings.profileAPN.value, '')
  assert.equal(bindings.editingProfileID.value, null)
  confirm(true)
  await pending
})

test('reselecting the current user or bot leaves unsaved fields intact', () => {
  for (const [file, name] of [['UserSettingsPanel.vue', 'selectUser'], ['TelegramSettingsForm.vue', 'selectUnit']]) {
    const context = evaluate(functions('components/' + file, [name]), {
      saving: ref(false), deleting: ref(false), pairingRevoking: ref(false), creating: ref(false), selectedID: ref('selected'),
      applyUser() { assert.fail('reselection discarded user draft') }, discardDraft() { assert.fail('reselection discarded bot draft') }
    })
    context[name]('selected')
  }
})

test('Escape is consumed by the inner selector and an already handled key never closes the dialog', () => {
  for (const file of ['LineSelector.vue', 'SelectControl.vue']) {
    let closed = false
    const event = { key: 'Escape', defaultPrevented: false, stopped: false,
      preventDefault() { this.defaultPrevented = true }, stopPropagation() { this.stopped = true } }
    const child = evaluate(functions('components/' + file, ['onOptionKeydown']), {
      activeIndex: ref(0), options: ref([{}]), props: { options: [{}] }, closeMenu() { closed = true }
    })
    child.onOptionKeydown(event, 0)
    assert.equal(closed, true)
    assert.equal(event.stopped, true)
    const parent = evaluate(functions('components/OverlayDialog.vue', ['onKeydown']), {
      overlayID: 'dialog', overlayIsTopmost: () => true, props: { closeOnEscape: true },
      emit() { assert.fail('outer editor must remain open') }
    })
    parent.onKeydown(event)
  }
})
