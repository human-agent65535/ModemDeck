import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { createCallTestController } from '../src/state/callTest.ts'
import { callTestPath, parseCallTestStatus } from '../src/api/contract.ts'
import { callAudioWebSocketURL } from '../src/state/callAudioProtocol.ts'
import { showBrowserNotification } from '../src/state/browserNotifications.ts'

const snapshot = (id = 'test-a', phase = 'active') => ({
  id, phase, accepted_at: new Date().toISOString(), expires_at: new Date(Date.now() + 60_000).toISOString(),
  test_phase: 'speak', test_audio: {
    phase: 'speak', remaining_ms: 2000, captured_frames: 50, captured_dbfs: -20,
    captured_peak_dbfs: -10, received_packets: 100, sent_packets: 100
  }
})
function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function setup(t, overrides = {}) {
  if (!t.callTestTimers) { t.mock.timers.enable({ apis: ['setTimeout'] }); t.callTestTimers = true }
  const events = []
  const gateway = {
    startCallTest: async language => { events.push(['start', language]); return snapshot() },
    getCallTestStatus: async id => snapshot(id),
    endCallTest: async id => { events.push(['end', id]) },
    ...overrides.gateway
  }
  const controller = createCallTestController({
    unavailable: () => false, failureMessage: () => 'Failed', notify: () => () => {},
    startMedia: id => events.push(['media', id]), stopMedia: id => events.push(['stop', id]),
    ...overrides, gateway
  })
  t.after(() => controller.stop())
  return { ...controller, events, start: language => {
    controller.schedule(language)
    t.mock.timers.tick(5000)
    return controller.answer()
  } }
}

test('call-test contract preserves audio evidence and rejects malformed responses', () => {
  assert.deepEqual(parseCallTestStatus(snapshot()).test_audio, snapshot().test_audio)
  assert.throws(() => parseCallTestStatus({ ...snapshot(), phase: 'unknown' }))
  assert.throws(() => parseCallTestStatus({ ...snapshot(), expires_at: 'not-a-date' }))
  assert.throws(() => parseCallTestStatus({ ...snapshot(), test_audio: { ...snapshot().test_audio, captured_dbfs: NaN } }))
  assert.equal(callTestPath('test / 1', 'hangup'), '/api/v1/call-tests/test%20%2F%201/hangup')
  assert.equal(callAudioWebSocketURL('test / 1', { href: 'https://deck.example/?secret=ignored' }, 'call-tests'),
    'wss://deck.example/api/v1/call-tests/test%20%2F%201/media/ws')
})

test('scheduled tests notify after five seconds and open no media until answered', async t => {
  const notifications = []
  const controller = setup(t, { notify: () => { notifications.push('shown'); return () => notifications.push('closed') } })
  controller.schedule('zh-CN')
  t.mock.timers.tick(4999)
  assert.equal(controller.state.status, 'scheduled')
  assert.deepEqual(controller.events, [])
  assert.deepEqual(notifications, [])
  t.mock.timers.tick(1)
  assert.equal(controller.state.status, 'ringing')
  assert.deepEqual(notifications, ['shown'])
  assert.deepEqual(controller.events, [])
  await controller.answer()
  assert.deepEqual(notifications, ['shown', 'closed'])
  assert.equal(controller.state.status, 'running')
})

test('cancelled or unanswered test calls dismiss notification and never capture audio', async t => {
  let closed = 0
  const controller = setup(t, { notify: () => () => { closed++ } })
  controller.schedule('en-US')
  await controller.stop()
  t.mock.timers.tick(5000)
  assert.equal(controller.state.status, 'ended')
  assert.equal(closed, 0)
  controller.schedule('en-US')
  t.mock.timers.tick(5000)
  t.mock.timers.tick(30_000)
  assert.equal(controller.state.status, 'ended')
  assert.equal(closed, 1)
  assert.deepEqual(controller.events, [])
})

test('test notifications share permission and preference gates and close on cancellation', t => {
  const originalWindow = globalThis.window
  const originalNotification = globalThis.Notification
  const notifications = []
  let enabled = false, clicks = 0
  globalThis.window = { isSecureContext: true, localStorage: { getItem: () => enabled ? 'enabled' : null } }
  globalThis.Notification = class {
    static permission = 'granted'
    constructor(title, options) { this.title = title; this.options = options; notifications.push(this) }
    close() { this.closed = true; this.onclose?.() }
  }
  t.after(() => {
    if (originalWindow === undefined) delete globalThis.window
    else globalThis.window = originalWindow
    if (originalNotification === undefined) delete globalThis.Notification
    else globalThis.Notification = originalNotification
  })
  const controller = new AbortController()
  const input = { title: 'Test call', body: 'Answer in the call window', tag: 'test',
    signal: controller.signal, onClick: () => { clicks++ } }
  assert.equal(showBrowserNotification(input), false)
  enabled = true
  globalThis.Notification.permission = 'denied'
  assert.equal(showBrowserNotification(input), false)
  globalThis.Notification.permission = 'granted'
  assert.equal(showBrowserNotification(input), true)
  assert.equal(notifications[0].options.tag, 'test')
  controller.abort()
  assert.equal(notifications[0].closed, true)
  notifications[0].onclick()
  assert.equal(clicks, 0, 'a stale notification must not reopen a stopped test')
  assert.equal(showBrowserNotification(input), false)
})

test('starting twice creates one test and stop releases both microphone and server session', async t => {
  const controller = setup(t)
  await Promise.all([controller.start('zh-CN'), controller.start('en-US')])
  assert.equal(controller.state.status, 'running')
  assert.deepEqual(controller.events, [['start', 'zh-CN'], ['media', 'test-a']])
  await controller.stop()
  await controller.stop()
  assert.equal(controller.state.status, 'ended')
  assert.deepEqual(controller.events.slice(2), [['stop', 'test-a'], ['end', 'test-a']])
})

test('leaving during start ends the late server session without opening the microphone', async t => {
  const pending = deferred()
  const controller = setup(t, { gateway: { startCallTest: () => pending.promise } })
  const start = controller.start('en-US')
  await controller.stop()
  pending.resolve(snapshot())
  await start
  assert.equal(controller.state.status, 'ended')
  assert.deepEqual(controller.events, [['end', 'test-a']])
})

test('a real call blocks start and can preempt a pending test', async t => {
  let unavailable = true
  const pending = deferred()
  const controller = setup(t, { unavailable: () => unavailable, gateway: { startCallTest: () => pending.promise } })
  await controller.start('en-US')
  assert.equal(controller.state.status, 'idle')
  unavailable = false
  const start = controller.start('en-US')
  unavailable = true
  pending.resolve(snapshot())
  await start
  assert.equal(controller.state.status, 'ended')
  assert.deepEqual(controller.events, [['end', 'test-a']])
})

test('poll failures release audio and late responses cannot revive a stopped test', async t => {
  const pending = deferred()
  const controller = setup(t, { gateway: { getCallTestStatus: () => pending.promise } })
  await controller.start('en-US')
  pending.reject(new Error('Connection lost'))
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(controller.state.status, 'error')
  assert.equal(controller.state.error, 'Connection lost')
  assert.deepEqual(controller.events.slice(-2), [['stop', 'test-a'], ['end', 'test-a']])

  const next = deferred()
  const second = setup(t, { gateway: { getCallTestStatus: () => next.promise } })
  await second.start('en-US')
  await second.stop()
  next.resolve(snapshot())
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(second.state.status, 'ended')
})

test('test audio stops at the local deadline even when a status request is stalled', async t => {
  const pending = deferred()
  const controller = setup(t, { gateway: { getCallTestStatus: () => pending.promise } })
  await controller.start('en-US')
  t.mock.timers.tick(60_000)
  assert.equal(controller.state.status, 'ended')
  assert.deepEqual(controller.events.slice(-2), [['stop', 'test-a'], ['end', 'test-a']])
  pending.resolve(snapshot())
  await Promise.resolve()
  assert.equal(controller.state.status, 'ended')
})

test('server-ended tests stop media and retain the last feedback', async t => {
  const controller = setup(t, { gateway: { getCallTestStatus: async id => snapshot(id, 'ended') } })
  await controller.start('en-US')
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(controller.state.status, 'ended')
  assert.equal(controller.state.session.test_audio.captured_frames, 50)
  assert.deepEqual(controller.events.slice(-2), [['stop', 'test-a'], ['end', 'test-a']])
})

test('empty real-call snapshots preserve diagnostics and stale diagnostic cleanup cannot stop a real call', () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const code = ast.statements.filter(statement => ts.isFunctionDeclaration(statement) &&
    ['syncCallMedia', 'shutdownCallMedia', 'stopCallTestMedia'].includes(statement.name?.text))
    .map(statement => statement.getText(ast).replace(/^export /, '')).join('\n')
  const state = { callID: '', status: 'idle' }
  const context = vm.createContext({ preparationMedia: undefined, mediaTarget: 'calls', attemptedCallID: '', fixtureCallMediaPreview: true,
    callMediaState: state, setIdle: () => { state.callID = ''; state.status = 'idle' } })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  context.syncCallMedia({ id: 'test-a', phase: 'active', media_available: true }, 'call-tests')
  context.syncCallMedia(null)
  assert.equal(state.callID, 'test-a')
  context.syncCallMedia({ id: 'real-call', phase: 'active', media_available: true })
  context.stopCallTestMedia('test-a')
  assert.equal(state.callID, 'real-call')
  context.shutdownCallMedia()
  assert.equal(state.status, 'idle')
})
