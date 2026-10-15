import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

const source = readFileSync(new URL('../src/components/CallSurface.vue', import.meta.url), 'utf8').split('<script setup lang="ts">')[1].split('</script>')[0]
const ast = ts.createSourceFile('surface.ts', source, ts.ScriptTarget.Latest, true)
const names = new Set(['isTestCall', 'owned', 'busy', 'active', 'recordingAdjustable', 'recordingControlReady', 'recordingLabel'])
const selected = ast.statements.filter(statement =>
  (ts.isVariableStatement(statement) && statement.declarationList.declarations.some(declaration => names.has(declaration.name.getText(ast)))) ||
  (ts.isFunctionDeclaration(statement) && statement.name?.text === 'toggleRecording')
)
const code = ts.transpileModule(selected.map(statement => statement.getText(ast)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
function harness() {
  const changes = []
  const ctx = vm.createContext({
    computed: get => ({ get value() { return get() } }),
    callTestSession: { value: null },
    session: { value: { id: 'call-1', phase: 'dialing' } },
    terminal: { value: false }, occupied: { value: false },
    callState: { owned: true, busy: false, pendingAction: '', outgoingRecordingEnabled: undefined },
    callRecordingState: { callID: 'call-1', status: 'ready', segmentsStatus: 'ready', enabled: false, recordingStatus: 'off', busy: false },
    t: key => key,
    setCallRecordingEnabled: enabled => changes.push(enabled)
  })
  vm.runInContext(code, ctx)
  return { ctx, changes, value: name => vm.runInContext(`${name}.value`, ctx), toggle: () => vm.runInContext('toggleRecording()', ctx) }
}
for (const enabled of [true, false]) {
  test(`recording snapshot (${enabled}) stays read-only through prepare/connect/dial/ring`, () => {
    const h = harness()
    h.ctx.callState.pendingAction = 'dial'
    h.ctx.callState.outgoingRecordingEnabled = enabled
    h.ctx.callRecordingState.enabled = !enabled
    assert.equal(h.value('recordingLabel'), enabled ? 'dialer.recordingEnabled' : 'dialer.recordingDisabled')
    assert.equal(h.value('recordingAdjustable'), false)
    h.toggle()
    assert.deepEqual(h.changes, [])
    h.ctx.callState.pendingAction = ''
    h.ctx.callRecordingState.enabled = enabled
    for (const phase of ['connecting', 'dialing', 'ringing-out']) {
      h.ctx.session.value.phase = phase
      assert.equal(h.value('recordingLabel'), enabled ? 'dialer.recordingEnabled' : 'dialer.recordingDisabled')
      assert.equal(h.value('recordingAdjustable'), false)
      h.toggle()
    }
    assert.deepEqual(h.changes, [])
  })
}
test('active toggle uses server state and waits for authority, busy operations and ownership', () => {
  const h = harness()
  h.ctx.session.value.phase = 'active'
  assert.equal(h.value('recordingAdjustable'), true)
  h.toggle()
  assert.deepEqual(h.changes, [true])
  h.ctx.callRecordingState.enabled = true
  h.toggle()
  assert.deepEqual(h.changes, [true, false])
  for (const mutate of [
    () => { h.ctx.callRecordingState.busy = true },
    () => { h.ctx.callRecordingState.busy = false; h.ctx.callState.busy = true },
    () => { h.ctx.callState.busy = false; h.ctx.callRecordingState.callID = 'another-call' },
    () => { h.ctx.callRecordingState.callID = 'call-1'; h.ctx.callState.owned = false }
  ]) { mutate(); h.toggle() }
  assert.deepEqual(h.changes, [true, false])
})
test('preparing has no false default before server response and post-commit ignores its old snapshot', () => {
  const h = harness()
  h.ctx.callState.pendingAction = 'dial'
  assert.equal(h.value('recordingLabel'), '')
  h.ctx.callState.outgoingRecordingEnabled = true
  h.ctx.callState.pendingAction = ''
  assert.equal(h.value('recordingLabel'), 'dialer.recordingDisabled')
  h.ctx.callRecordingState.callID = 'previous-call'
  assert.equal(h.value('recordingLabel'), '')
})
test('active recording status preserves applying and failure feedback', () => {
  const h = harness()
  h.ctx.session.value.phase = 'active'
  h.ctx.callRecordingState.busy = true
  assert.equal(h.value('recordingLabel'), 'calls.applyingRecording')
  h.ctx.callRecordingState.busy = false
  h.ctx.callRecordingState.recordingStatus = 'failed'
  assert.equal(h.value('recordingLabel'), 'calls.recordingFailed')
  h.ctx.callRecordingState.recordingStatus = 'recording'
  assert.equal(h.value('recordingLabel'), 'calls.recordingActive')
})


test('diagnostics use the shared call surface without exposing real recording controls', () => {
  const h = harness()
  h.ctx.callTestSession.value = { id: 'call-1' }
  h.ctx.session.value.phase = 'active'
  h.ctx.session.value.control_state = 'owned'
  h.ctx.callRecordingState.enabled = true
  h.ctx.callRecordingState.recordingStatus = 'recording'
  assert.equal(h.value('recordingAdjustable'), false)
  assert.equal(h.value('recordingLabel'), '')
  h.toggle()
  assert.deepEqual(h.changes, [])
})
