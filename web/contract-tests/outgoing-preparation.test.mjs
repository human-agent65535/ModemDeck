import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'
const source = readFileSync(new URL('../src/state/call.ts', import.meta.url), 'utf8')
const ast = ts.createSourceFile('call.ts', source, ts.ScriptTarget.Latest, true)
const functions = ['dial', 'cancelOutgoingPreparation']
const code = ts.transpileModule(ast.statements.filter(s => ts.isFunctionDeclaration(s) && functions.includes(s.name?.text)).map(s => s.getText(ast).replace(/^export /, '')).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
function deferred() { let resolve, reject; const promise = new Promise((a,b) => {resolve=a;reject=b}); return {promise,resolve,reject} }
function harness() {
  const operations = [], readiness = deferred(), preparation = deferred(), committed = deferred()
  const ctx = vm.createContext({ Date, Error, outgoingGeneration: 0, outgoingPreparation: undefined, mutationEpoch: 0,
    CALL_LEASE_HEARTBEAT_MS: 5000, callState: { session: null, busy: false },
    capabilityReason: () => '', ownedSession: () => null, lineIsOccupied: () => false, translate: x => x,
    requestError: error => ({ message: error.message, status: 0 }),
    gateway: { prepareCall: (...args) => { operations.push(['prepare', ...args]); return preparation.promise },
      commitCallPreparation: id => {operations.push(['commit',id]);return committed.promise},
      cancelCallPreparation: async id => operations.push(['cancel',id]), renewCallPreparation: async () => {} },
    preparedCallMediaIsReady: () => true,
    prepareOutgoingCallMedia: () => readiness.promise,
    adoptPreparedCallMedia: session => operations.push(['adopt',session.id]),
    acceptSession: session => {ctx.callState.session=session;operations.push(['accept',session.id])},
    showCallSurface() {}, closeDialer() {}, shutdownCallMedia: () => operations.push(['stop']),
    presentEndedSession: (session, confirmed) => {ctx.callState.session={...session,...confirmed,phase:confirmed?.phase || 'ended'}},
    window: { setInterval: () => 1, clearInterval() {} }
  })
  vm.runInContext(code, ctx)
  return {ctx,operations,readiness,preparation,committed,
    prepare: async (enabled = false) => { preparation.resolve({preparation_id:'p1', recording_enabled:enabled}); await new Promise(resolve=>setImmediate(resolve)) },
    complete: () => committed.resolve({id:'c1',phase:'dialing'}) }
}
test('dial reserves with recording preference and commits only after actual media readiness', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1',false)
  assert.equal(await h.ctx.dial('555','line1',true),false)
  await h.prepare(); assert.equal(h.operations.filter(x=>x[0]==='commit').length,0)
  h.readiness.resolve(); await new Promise(resolve=>setImmediate(resolve)); h.complete()
  assert.equal(await pending,true)
  assert.deepEqual(h.operations.filter(x=>['prepare','commit','adopt'].includes(x[0])),[['prepare','line1','555',false],['commit','p1'],['adopt','c1']])
})
test('local audio preparation failure cancels reservation without dialing', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1'); await h.prepare()
  h.readiness.reject(new Error('mic denied')); assert.equal(await pending,false)
  assert.equal(h.operations.some(x=>x[0]==='commit'),false)
  assert.equal(h.operations.some(x=>x[0]==='cancel'),true)
  assert.equal(h.ctx.callState.session.phase,'failed')
})
test('cancel before prepare response cleans a late reservation and never commits', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1'); await h.ctx.cancelOutgoingPreparation(); await h.prepare()
  assert.equal(await pending,false); assert.deepEqual(h.operations.filter(x=>x[0]==='cancel'),[['cancel','p1']])
})
test('cancel during local audio readiness does not accept a late success', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1'); await h.prepare(); await h.ctx.cancelOutgoingPreparation()
  h.readiness.resolve(); assert.equal(await pending,false)
  assert.equal(h.operations.some(x=>x[0]==='commit'),false); assert.equal(h.ctx.callState.session.phase,'ended')
})
test('late commit response after cancel remains ended and uses cancellation endpoint', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1'); await h.prepare(); h.readiness.resolve()
  await new Promise(resolve=>setImmediate(resolve)); await h.ctx.cancelOutgoingPreparation(); h.complete()
  assert.equal(await pending,false); assert.equal(h.ctx.callState.session.phase,'ended')
  assert.equal(h.operations.some(x=>x[0]==='adopt'),false)
  assert.equal(h.operations.filter(x=>x[0]==='cancel').length,2)
})

test('audio lost after ready but before commit cancels without dialing', async () => {
  const h=harness(), pending=h.ctx.dial('555','line1'); await h.prepare()
  h.ctx.preparedCallMediaIsReady=()=>false;h.readiness.resolve()
  assert.equal(await pending,false);assert.equal(h.operations.some(x=>x[0]==='commit'),false)
})

for (const enabled of [true, false]) {
  test(`preparing caption uses the server recording snapshot (${enabled}) before media and switches to call authority after commit`, async () => {
    const h = harness()
    const pending = h.ctx.dial('555', 'line1')
    await h.prepare(enabled)
    assert.equal(h.ctx.callState.outgoingRecordingEnabled, enabled)
    assert.equal(h.ctx.callState.pendingAction, 'dial')
    h.readiness.resolve()
    await new Promise(resolve => setImmediate(resolve))
    h.complete()
    assert.equal(await pending, true)
    assert.equal(h.ctx.callState.pendingAction, '')
  })
}
