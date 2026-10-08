import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import createAudioCore from '../src/state/audioCore.mjs'
import { CALL_AUDIO_FORMAT, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError, validateCallAudioProgress } from '../src/state/callAudioProtocol.ts'

const wasmBinary = readFileSync(new URL('../src/state/audioCore.wasm', import.meta.url))
const mediaSource = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
const mediaAST = ts.createSourceFile('media.ts', mediaSource, ts.ScriptTarget.Latest, true)
function mediaFunctions(names) {
  return ts.transpileModule(mediaAST.statements.filter(s => ts.isFunctionDeclaration(s) && names.includes(s.name?.text))
    .map(s => s.getText(mediaAST)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
}
function transport() {
  let now = 0, connection
  const posted = [], sent = [], recovered = [], failed = []
  const runtime = { context: { currentTime: 0, state: 'running' }, contextEpoch: 0, receivePending: 0, sentBytes: 0, pendingWrites: [], realOutputSamples: 0, lastRealRenderUs: -Infinity,
    ingressCapacity: 200, sendCapacity: 100, resetSequence: true, ready: false,
    node: { port: { postMessage: m => posted.push(m) } }, sentFrames: 0, receivedFrames: 0 }
  const owner = { ownerToken: 'owner-a' }
  const ctx = vm.createContext({ ArrayBuffer, performance: { now: () => now },
    audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined, socketWatchdogID: undefined,
    MEDIA_CONNECT_TIMEOUT_MS: 5000, MEDIA_WRITE_TIMEOUT_MS: 2000, CALL_AUDIO_FORMAT,
    callMediaState: {}, isCurrent: (id, token) => id === 'call-a' && token === 1,
    callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError, validateCallAudioProgress,
    translate: x => x, playRemoteAudio: async () => {}, settleMediaRecovery() {},
    window: { location: { href: 'https://calls.example.test/' }, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 2 },
    recoverConnection: (...args) => { recovered.push(args); ctx.socket = undefined; runtime.ready = false },
    failConnection: (...args) => { failed.push(args); ctx.socket = undefined; runtime.ready = false },
    WebSocket: class { static OPEN = 1; readyState = 1; bufferedAmount = 0;
      constructor() { connection = this } send(packet) { sent.push(packet) } }
  })
  vm.runInContext(mediaFunctions(['openAudioSocket', 'updateAudioRuntime', 'receiveAudioMessage', 'updateSocketProgress']), ctx)
  return { ctx, runtime, owner, posted, sent, recovered, failed, setNow: v => { now = v },
    get connection() { return connection }, async start() {
      await ctx.openAudioSocket('call-a', 1, owner)
      connection.onopen()
      connection.onmessage({ data: JSON.stringify({ type: 'ready', ...CALL_AUDIO_FORMAT }) })
    }, message(data) { ctx.receiveAudioMessage('call-a', 1, owner, runtime, data) } }
}

test('call audio uses same-origin WSS, encoded call IDs and no credentials in its URL', () => {
  assert.equal(callAudioWebSocketURL('call / 1', { href: 'https://calls.example.test/settings?token=ignored' }),
    'wss://calls.example.test/api/v1/calls/call%20%2F%201/media/ws')
  assert.equal(callAudioWebSocketURL('c', { href: 'http://localhost:5173/' }), 'ws://localhost:5173/api/v1/calls/c/media/ws')
  assert.throws(() => callAudioWebSocketURL(' ', { href: 'https://example.test/' }))
})

test('MD12 packet framing enforces endian, bounds and negotiated format', () => {
  const packet = encodeCallAudioPacket(0x01020304, new Uint8Array([0xf8, 0xff, 0xfe]))
  assert.deepEqual([...new Uint8Array(packet).slice(0, 8)], [77, 68, 1, 0, 1, 2, 3, 4])
  const decoded = decodeCallAudioPacket(packet)
  assert.equal(decoded.sequence, 0x01020304)
  assert.equal(decoded.timestamp, (0x01020304 * 320) >>> 0)
  assert.deepEqual([...decoded.payload], [0xf8, 0xff, 0xfe])
  assert.throws(() => encodeCallAudioPacket(0, new Uint8Array()))
  assert.throws(() => encodeCallAudioPacket(0, new Uint8Array(1276)))
  for (const offset of [0, 1, 2, 3]) {
    const invalid = packet.slice(0)
    new Uint8Array(invalid)[offset] = 0xff
    assert.throws(() => decodeCallAudioPacket(invalid))
  }
  assert.throws(() => decodeCallAudioPacket(new ArrayBuffer(12)))
  validateCallAudioProgress({ sequence: 0xffffffff, timestamp: 0xffffff00 }, { sequence: 0, timestamp: 64 })
  validateCallAudioProgress({ sequence: 0, timestamp: 0 }, { sequence: 5, timestamp: 1600 })
  for (const packet of [{ sequence: 0, timestamp: 0 }, { sequence: 0xffffffff, timestamp: 0 }, { sequence: 1, timestamp: 321 }]) {
    assert.throws(() => validateCallAudioProgress({ sequence: 0, timestamp: 0 }, packet))
  }
  const ready = { type: 'ready', version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20 }
  assert.equal(isCallAudioReady(ready), true)
  for (const [key, value] of [['version', 2], ['codec', 'pcm'], ['sample_rate', 48000], ['channels', 2], ['frame_ms', 40]]) {
    assert.equal(isCallAudioReady({ ...ready, [key]: value }), false)
  }
})


test('transport JSON error followed by close recovers once; ownership and format faults remain fatal', async () => {
  for (const code of ['transport_timeout', 'transport_closed', 'backpressure', 'invalid_audio', 'lease_expired', 'ownership_lost', 'unknown']) {
    const t = transport(); await t.start()
    const close = t.connection.onclose
    t.connection.onmessage({ data: JSON.stringify({ type: 'error', code }) })
    close({ code: 1011 })
    assert.equal(t.recovered.length, isRecoverableCallAudioError(code) ? 1 : 0, code)
    assert.equal(t.failed.length, isRecoverableCallAudioError(code) ? 0 : 1, code)
    assert.equal(t.owner.claimed, true)
  }
})

test('recovery releases only media immediately, backs off within one deadline and aborts stale reconnects', async () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const code = ast.statements.filter(statement => ts.isFunctionDeclaration(statement) &&
    ['isCurrent', 'beginRecoveryWindow', 'recoverConnection'].includes(statement.name?.text)).map(statement => statement.getText(ast)).join('\n')
  const timers = [], releases = [], opened = []
  const owner = { ownerToken: 'owner-a' }
  const context = vm.createContext({ generation: 1, currentCallID: 'call-a', audioRuntime: { recoveryAttempts: 0 },
    recoveryTimeoutID: undefined, reconnectTimeoutID: undefined, callMediaState: {},
    MEDIA_RECOVERY_TIMEOUT_MS: 15000, MEDIA_RECONNECT_DELAY_MS: 500, translate: key => key,
    clearSocketResources() {}, failConnection() {},
    window: { setTimeout: (callback, delay) => { timers.push({ callback, delay }); return timers.length } },
    gateway: { releaseCallMedia: (id, token) => { releases.push({ id, token }); return Promise.reject(Error('network interrupted')) } },
    openAudioSocket: (id, token) => { opened.push({ id, token }) }
  })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  context.recoverConnection('call-a', 1, owner)
  assert.deepEqual(releases, [{ id: 'call-a', token: 'owner-a' }], 'old media is released before waiting')
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(timers[0].delay, 15000)
  assert.equal(timers[1].delay, 500)
  timers[1].callback()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(timers[2].delay, 1000)
  timers[2].callback()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(timers[3].delay, 2000)
  assert.equal(timers.filter(timer => timer.delay === 15000).length, 1, 'retries never extend the recovery deadline')
  context.generation = 2
  context.currentCallID = 'call-b'
  timers[3].callback()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(releases.length, 3, 'stale recovery cannot touch the new call')
  assert.equal(opened.length, 0)
})

test('repeated ready then early transport failures cannot restart the recovery deadline', async () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const selected = ['isCurrent', 'clearRecoveryWindow', 'beginRecoveryWindow', 'settleMediaRecovery', 'recoverConnection', 'openAudioSocket', 'updateAudioRuntime', 'updateSocketProgress']
  const code = ast.statements.filter(statement => ts.isFunctionDeclaration(statement) && selected.includes(statement.name?.text))
    .map(statement => statement.getText(ast)).join('\n').replaceAll('import.meta.url', '"https://calls.example.test/assets/app.js"')
  let connection, now = 0, nextID = 0, failed = 0
  const timers = new Map(), intervals = new Map(), cleared = []
  const runtime = { context: { currentTime: 0, state: 'running' }, contextEpoch: 0, sentBytes: 0, pendingWrites: [], node: { port: { postMessage() {} } }, ready: false,
    lastActivity: Date.now(), recoveryAttempts: 0, sentFrames: 0, receivedFrames: 0, lastSentAt: -Infinity, lastReceivedAt: -Infinity }
  const context = vm.createContext({ URL, performance: { now: () => now }, generation: 1, currentCallID: 'call-a',
    audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined, reconnectTimeoutID: undefined,
    recoveryTimeoutID: undefined, socketWatchdogID: undefined, socketBufferedSince: undefined,
    MEDIA_CONNECT_TIMEOUT_MS: 5000, MEDIA_RECOVERY_TIMEOUT_MS: 15000, MEDIA_RECONNECT_DELAY_MS: 500,
    MEDIA_STABLE_WINDOW_MS: 1000, MEDIA_STABLE_FRAMES: 40,
    MEDIA_HEALTH_INTERVAL_MS: 1000, MEDIA_WRITE_TIMEOUT_MS: 2000,
    CALL_AUDIO_FORMAT: { version: 1 }, callMediaState: { status: 'connecting', error: '' },
    isCallAudioReady, isRecoverableCallAudioError, callAudioWebSocketURL, translate: key => key,
    playRemoteAudio: async () => {},
    WebSocket: class { bufferedAmount = 0; constructor() { connection = this } send() {} },
    window: { location: { href: 'https://calls.example.test/' },
      setTimeout: (callback, delay) => { timers.set(++nextID, { callback, delay }); return nextID },
      clearTimeout: id => { cleared.push(id); timers.delete(id) },
      setInterval: callback => { intervals.set(++nextID, callback); return nextID },
      clearInterval: id => intervals.delete(id) },
    gateway: { releaseCallMedia: async () => {} },
    clearSocketResources: () => {
      context.socket = undefined
      runtime.ready = false
      runtime.sentFrames = runtime.receivedFrames = 0
      runtime.stableSince = undefined
      runtime.lastSentAt = runtime.lastReceivedAt = -Infinity
      intervals.clear()
    },
    failConnection: () => { failed++; context.generation++; context.currentCallID = ''; context.callMediaState.status = 'error' }
  })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  const ownership = { ownerToken: 'owner-a', claimed: false }
  const ready = () => {
    connection.onopen()
    connection.onmessage({ data: JSON.stringify({ type: 'ready', version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20 }) })
  }
  await context.openAudioSocket('call-a', 1, ownership)
  ready()
  let deadlineID
  for (let attempt = 0; attempt < 6; attempt++) {
    connection.onmessage({ data: JSON.stringify({ type: 'error', code: 'backpressure' }) })
    deadlineID ??= context.recoveryTimeoutID
    assert.equal(context.recoveryTimeoutID, deadlineID)
    await new Promise(resolve => setImmediate(resolve))
    const [id, timer] = [...timers].find(([_id, value]) => value.delay !== 15000 && value.delay !== 5000)
    timers.delete(id)
    now += timer.delay
    timer.callback()
    ready()
    for (const callback of intervals.values()) callback()
    assert.equal(context.recoveryTimeoutID, deadlineID, 'ready without useful audio keeps the original deadline')
  }
  assert.equal(runtime.recoveryAttempts, 6)
  assert.equal(cleared.includes(deadlineID), false)
  now = 15000
  timers.get(deadlineID).callback()
  assert.equal(failed, 1)
  assert.equal(context.callMediaState.status, 'error')
})

test('a recovery deadline resets only after at least one second of useful duplex media', () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const code = ast.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === 'settleMediaRecovery').getText(ast)
  let now = 999, cleared = 0
  const runtime = { stableSince: 0, lastSentAt: 999, lastReceivedAt: 999, sentFrames: 40, receivedFrames: 40, realOutputSamples: 48000, lastRealRenderUs: 999000, pendingWrites: [], recoveryAttempts: 5 }
  const context = vm.createContext({ recoveryTimeoutID: 1, socketBufferedSince: undefined,
    MEDIA_STABLE_WINDOW_MS: 1000, MEDIA_STABLE_FRAMES: 40, performance: { now: () => now },
    MEDIA_HEALTH_INTERVAL_MS: 1000, MEDIA_WRITE_TIMEOUT_MS: 2000,
    clearRecoveryWindow: () => { cleared++ } })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  now = 1000
  runtime.receivedFrames = 39
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  runtime.receivedFrames = 40
  runtime.pendingWrites = [{ sentAt: -1 }]
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  runtime.pendingWrites = []
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0, 'buffering restarts the stable media window')
  now = 2000
  runtime.lastSentAt = runtime.lastReceivedAt = now
  runtime.lastRealRenderUs = now * 1000
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 1)
  assert.equal(runtime.recoveryAttempts, 0)
})


test('actual dispatch retains 100–1500ms batches; only the explicit ingress resource bound fails', async () => {
  for (const count of [5, 8, 25, 75]) {
    const t = transport(); await t.start(); t.setNow(count * 20)
    for (let seq = 0; seq < count; seq++) t.connection.onmessage({ data: encodeCallAudioPacket(seq, new Uint8Array([0xf8])) })
    const packets = t.posted.filter(m => m.type === 'packet')
    assert.equal(packets.length, count)
    assert.ok(packets.every(m => m.nowUs === count * 20000))
    assert.equal(t.recovered.length, 0)
    for (const m of packets) t.message({ type: 'received', epoch: m.epoch })
    assert.equal(t.runtime.receivePending, 0)
  }
  const t = transport(); await t.start()
  for (let seq = 0; seq < 201; seq++) t.connection?.onmessage({ data: encodeCallAudioPacket(seq, new Uint8Array([0xf8])) })
  assert.equal(t.posted.filter(m => m.type === 'packet').length, 200)
  assert.equal(t.recovered.length, 1)
})

test('encoded batches have no per-frame TTL and stale epochs cannot send, acknowledge or mutate the replacement', async () => {
  const t = transport(); await t.start()
  const epoch = t.runtime.contextEpoch
  t.setNow(1500)
  for (let seq = 0; seq < 75; seq++) t.message({ type: 'encoded', epoch, sequence: seq, payload: new Uint8Array([0xf8]) })
  assert.equal(t.sent.length, 76, 'one start control plus all 75 media packets')
  let downlinkSequence = 99
  for (const state of ['interrupted', 'suspended', 'closed']) {
    t.runtime.context.state = state
    t.ctx.updateAudioRuntime(t.runtime)
    const pausedEpoch = t.runtime.contextEpoch
    t.connection.onmessage({ data: encodeCallAudioPacket(downlinkSequence++, new Uint8Array([0xf8])) })
    assert.equal(t.runtime.receivePending, 0)
    t.runtime.context.state = 'running'
    t.setNow(3500)
    t.ctx.updateAudioRuntime(t.runtime)
    const before = t.posted.length
    t.message({ type: 'encoded', epoch: pausedEpoch, sequence: 0, payload: new Uint8Array([0xf8]) })
    t.message({ type: 'received', epoch })
    t.message({ type: 'error', epoch, message: 'old failed' })
    assert.equal(t.posted.length, before)
    assert.equal(t.runtime.sentFrames, 0)
    assert.equal(t.runtime.receivedFrames, 0)
    assert.equal(t.failed.length, 0)
  }
  t.ctx.audioRuntime = { ...t.runtime }
  t.message({ type: 'error', epoch: t.runtime.contextEpoch, message: 'old node' })
  assert.equal(t.failed.length, 0)
})

test('production device state callback resets the owner; Resume handles interrupted/suspended but never closed', async () => {
  let callback
  const visit = node => {
    if (ts.isBinaryExpression(node) && node.left.getText(mediaAST) === 'context.onstatechange') callback = node.getText(mediaAST)
    ts.forEachChild(node, visit)
  }
  visit(mediaAST)
  for (const state of ['interrupted', 'suspended', 'closed']) {
    const t = transport(); await t.start()
    let resumes = 0
    Object.assign(t.ctx, { context: t.runtime.context, runtime: t.runtime, callID: 'call-a', token: 1,
      remoteAudio: { play: async () => {}, pause() {} }, audioState: { callVolume: 100 }, applySelectedAudioOutput: async () => true })
    t.runtime.context.resume = async () => { resumes++; t.runtime.context.state = 'running'; t.runtime.context.onstatechange() }
    vm.runInContext(mediaFunctions(['playRemoteAudio']) + ts.transpileModule(callback, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, t.ctx)
    const oldEpoch = t.runtime.contextEpoch
    t.runtime.context.state = state
    t.runtime.context.onstatechange()
    assert.ok(t.runtime.contextEpoch > oldEpoch)
    assert.equal(t.ctx.callMediaState.playbackBlocked, true)
    assert.equal(t.posted.at(-1).type, 'deactivate')
    await t.ctx.playRemoteAudio()
    assert.equal(resumes, state === 'closed' ? 0 : 1)
    assert.equal(t.ctx.callMediaState.playbackBlocked, state === 'closed')
    if (state !== 'closed') assert.equal(t.posted.at(-1).type, 'activate')
  }
})

test('steady nonempty WebSocket draining stays healthy; only a specific pending frame ages out', async () => {
  const t = transport(); await t.start()
  let latestBytes = 0
  // Deliberately never report an empty writer. Each send completes its previous
  // frame while retaining the latest one, for longer than the2s write bound.
  for (let frame = 0; frame < 200; frame++) {
    t.setNow(frame * 20)
    t.connection.bufferedAmount = latestBytes
    t.message({ type: 'encoded', epoch: t.runtime.contextEpoch, sequence: frame, payload: new Uint8Array([0xf8]) })
    latestBytes = 13
    t.connection.bufferedAmount = latestBytes
  }
  assert.equal(t.recovered.length, 0)
  assert.ok(t.runtime.pendingWrites.length <= 2)
  t.setNow(5480)
  assert.equal(t.ctx.updateSocketProgress(t.runtime, t.connection), false, '1500ms pending is allowed')
  t.connection.bufferedAmount = 0
  assert.equal(t.ctx.updateSocketProgress(t.runtime, t.connection), false, 'completed1500ms frame clears')
  t.message({ type: 'encoded', epoch: t.runtime.contextEpoch, sequence: 200, payload: new Uint8Array([0xf8]) })
  t.connection.bufferedAmount = 13
  t.setNow(7481)
  assert.equal(t.ctx.updateSocketProgress(t.runtime, t.connection), true, 'same actual frame blocked more than2s')
})

test('WebSocket pending frame capacity counts actual packets rather than worst-case bytes', async () => {
  const t = transport(); await t.start()
  for (let frame = 0; frame < 101; frame++) {
    t.setNow(frame * 10)
    t.connection.bufferedAmount = frame * 13
    t.message({ type: 'encoded', epoch: t.runtime.contextEpoch, sequence: frame, payload: new Uint8Array([0xf8]) })
  }
  assert.equal(t.runtime.pendingWrites.length, 100)
  assert.equal(t.recovered.length, 1)
})

test('PLC-only reception cannot clear the recovery budget without real shared-core output', () => {
  let cleared = 0
  const runtime = { stableSince: 0, lastSentAt: 2000, lastReceivedAt: 2000, sentFrames: 100, receivedFrames: 100,
    realOutputSamples: 0, lastRealRenderUs: 0, pendingWrites: [], recoveryAttempts: 4 }
  const context = vm.createContext({ recoveryTimeoutID: 1, performance: { now: () => 2000 }, MEDIA_HEALTH_INTERVAL_MS: 1000,
    MEDIA_STABLE_WINDOW_MS: 1000, MEDIA_STABLE_FRAMES: 40, clearRecoveryWindow: () => { cleared++ } })
  vm.runInContext(mediaFunctions(['settleMediaRecovery']), context)
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  runtime.stableSince = 0; runtime.realOutputSamples = 48000; runtime.lastRealRenderUs = 2000000
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 1)
})

const workletSource = readFileSync(new URL('../src/state/callAudio.worklet.ts', import.meta.url), 'utf8').replace(/^import .*\n/gm, '')
const workletCode = ts.transpileModule(workletSource, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText
function worklet(rate = 48000, createCore = createAudioCore) {
  let Processor
  const messages = []
  const context = vm.createContext({ sampleRate: rate, currentTime: 0, createAudioCore: createCore, Float32Array, Uint8Array, BigInt,
    AudioWorkletProcessor: class { port = { postMessage: m => messages.push(m), onmessage: undefined } },
    registerProcessor: (_name, value) => { Processor = value } })
  vm.runInContext(workletCode, context)
  const p = new Processor({ processorOptions: { wasmBinary } })
  p.port.onmessage({ data: { type: 'activate', epoch: 1, nowUs: 0, contextTime: 0, sequence: 0 } })
  let frames = 0
  return { p, context, messages, rate,
    post(data) { p.port.onmessage({ data: { epoch: 1, ...data } }) },
    render(count = 128, capture = false) {
      context.currentTime = frames / rate
      const out = new Float32Array(count)
      const input = Float32Array.from({ length: count }, (_, i) => capture ? Math.sin((frames + i) * 2 * Math.PI * 440 / rate) * .3 : 0)
      p.process([[input]], [[out]])
      frames += count
      return out
    },
    stats() {
      const ptr = p.core._malloc(256)
      assert.equal(p.core._md_neteq_get_stats(p.receiver, ptr), 0)
      const data = new DataView(p.core.HEAPU8.buffer, ptr, 136)
      const fields = ['concealed', 'events', 'inserted', 'removed', 'discarded', 'received', 'emitted']
      const s = Object.fromEntries(fields.map((name, i) => [name, Number(data.getBigUint64(i * 8, true))]))
      s.targetDelay = data.getUint32(72, true)
      s.internalRate = data.getUint32(80, true)
      s.realOutputSamples = Number(data.getBigUint64(104, true))
      s.lastRealRenderUs = Number(data.getBigUint64(112, true))
      s.renderErrors = Number(data.getBigUint64(120, true))
      p.core._free(ptr)
      return s
    }, dispose() { p.port.onmessage({ data: { type: 'deactivate', epoch: 2 } }) }
  }
}

function encodedTone(count) {
  const m = createAudioCore({ wasmBinary, print() {}, printErr() {} })
  const encoder = m._md_opus_encoder_create(48000, 1)
  const pcm = m._malloc(960 * 4), out = m._malloc(1275)
  const packets = []
  for (let frame = 0; frame < count; frame++) {
    for (let i = 0; i < 960; i++) m.HEAPF32[(pcm >> 2) + i] = Math.sin((frame * 960 + i) * 2 * Math.PI * 440 / 48000) * .3
    const size = m._md_opus_encode_float(encoder, pcm, 960, out, 1275)
    assert.ok(size > 0 && size <= 1275)
    packets.push(m.HEAPU8.slice(out, out + size))
  }
  m._md_opus_encoder_destroy(encoder); m._free(pcm); m._free(out)
  return packets
}
const tonePackets = encodedTone(1000)
function replay(delay = () => 0, { wrap = false, quantum = 128, seconds = 12 } = {}) {
  const w = worklet()
  let previousArrival = 0
  const events = tonePackets.slice(0, seconds * 50 - 25).map((payload, i) => {
    const batch = Math.floor(i / 5)
    previousArrival = Math.max(previousArrival, ((batch + 1) * 100 + 50 + delay(batch)) * 1000)
    return { payload, sequence: wrap ? (0xffffffce + i) >>> 0 : i,
      timestamp: wrap ? (0xffffc180 + i * 320) >>> 0 : i * 320,
      nowUs: previousArrival }
  })
  let next = 0, samples = 0, energy = 0, tailMissing = 0, tailStart, tailEnd, health
  const total = seconds * 48000
  while (samples < total) {
    const nowUs = samples * 1e6 / 48000
    while (next < events.length && events[next].nowUs <= nowUs) w.post({ type: 'packet', ...events[next++] })
    const out = w.render(Math.min(quantum, total - samples))
    if (samples >= 8 * 48000 && samples < 10 * 48000) {
      energy += out.reduce((sum, v) => sum + v * v, 0)
      tailMissing += out.filter(v => v === 0).length
    }
    samples += out.length
    if (tailStart === undefined && samples >= 8 * 48000) tailStart = w.stats().concealed
    if (tailEnd === undefined && samples >= 10 * 48000) tailEnd = w.stats().concealed
    for (const message of w.messages.splice(0)) {
      if (message.type === 'encoded') w.post({ type: 'ack' })
      if (message.type === 'health') health = message
      assert.notEqual(message.type, 'error', message.message)
    }
  }
  const stats = w.stats()
  assert.ok(health.realOutputSamples >= 48000, 'health is based on actual non-PLC render output')
  assert.ok(health.lastRealRenderUs > 1000000)
  assert.equal(health.renderErrors, 0)
  w.dispose()
  return { stats, energy, tailMissing, tailConcealed: tailEnd - tailStart }
}

test('actual production worklet uses one real NetEq/Opus WASM at native48k with10ms remainder only', () => {
  const captureBlocks = [], encoderRates = []
  const w = worklet(48000, options => {
    const core = createAudioCore(options)
    const create = core._md_opus_encoder_create, encode = core._md_opus_encode_float
    core._md_opus_encoder_create = (rate, channels) => { encoderRates.push(rate); return create(rate, channels) }
    core._md_opus_encode_float = (encoder, input, frames, output, capacity) => {
      captureBlocks.push(core.HEAPF32.slice(input >> 2, (input >> 2) + frames))
      return encode(encoder, input, frames, output, capacity)
    }
    return core
  })
  assert.deepEqual(encoderRates, [48000], 'native samples go directly to the shared 48k Opus encoder')
  assert.equal(w.messages[0].type, 'ready')
  assert.equal(w.messages[0].memoryBytes, 33554432)
  assert.equal(w.messages[0].shared, false)
  assert.equal(w.messages[0].sendCapacity, 100)
  assert.equal(w.messages[0].ingressCapacity, 200)
  const sizes = [128, 256, 64, 192]
  let frames = 0, encoded = 0
  for (let i = 0; frames < 48000; i++) {
    const count = Math.min(sizes[i % sizes.length], 48000 - frames)
    assert.ok(w.render(count, true).every(Number.isFinite))
    frames += count
    for (const message of w.messages.splice(0)) {
      assert.notEqual(message.type, 'error', message.message)
      if (message.type === 'encoded') {
        assert.equal(message.sequence, encoded++)
        assert.ok(message.payload.length > 0 && message.payload.length <= 1275)
        w.post({ type: 'ack' })
      }
    }
  }
  assert.equal(encoded, 50)
  assert.equal(captureBlocks.length, 50)
  for (let frame = 0; frame < captureBlocks.length; frame++) {
    assert.equal(captureBlocks[frame].length, 960)
    for (let i = 0; i < 960; i++) assert.equal(captureBlocks[frame][i],
      Math.fround(Math.sin((frame * 960 + i) * 2 * Math.PI * 440 / 48000) * .3),
      'all native capture samples survive variable render-quantum boundaries without resampling')
  }
  const stats = w.stats()
  assert.equal(stats.internalRate, 48000)
  w.dispose()
})

test('actual receiver recovers after one41–150ms delay without permanent missing PCM or overflow', () => {
  const baseline = replay()
  for (const delay of [41, 60, 100, 150]) {
    const result = replay(batch => batch === 4 ? delay : 0)
    assert.ok(result.energy > baseline.energy * .9, `${delay}ms: useful tail audio remains continuous`)
    assert.equal(result.tailMissing, baseline.tailMissing)
    assert.ok(result.tailConcealed <= baseline.tailConcealed + 480, `${delay}ms: no persistent PLC in healthy tail`)
    assert.equal(result.stats.discarded, 0)
    assert.equal(result.stats.received, baseline.stats.received)
  }
})

test('actual receiver adapts alternating100ms and sustained phase changes; TCP stalls do not fail the transport', () => {
  for (const delay of [b => b % 2 ? 100 : 0, b => b >= 4 ? 100 : 0,
    b => b >= 4 ? -20 : 0, b => b >= 4 ? -100 : 0, b => (b * 7919) % 101,
    b => b >= 4 && b < 9 ? 500 - (b - 4) * 100 : 0,
    b => b >= 4 && b < 19 ? 1500 - (b - 4) * 100 : 0]) {
    const result = replay(delay)
    assert.ok(result.energy > 1000, 'healthy tail resumes after the disturbance')
    assert.equal(result.stats.received, 575)
    assert.ok(result.stats.targetDelay >= 40 && result.stats.targetDelay <= 200)
  }
})

test('standard48k RTP wrap and sequence wrap preserve complete receiver output statistics', () => {
  assert.deepEqual(replay(() => 0, { wrap: true }), replay())
})

test('arrival before the latest render remains a real late packet; stale device epochs cannot insert', () => {
  const w = worklet()
  for (let i = 0; i < 40; i++) { w.render(); w.messages.splice(0).forEach(m => { if (m.type === 'encoded') w.post({ type: 'ack' }) }) }
  w.post({ type: 'packet', payload: tonePackets[0], sequence: 0, timestamp: 0, nowUs: 50000 })
  w.render()
  assert.ok(w.messages.some(m => m.type === 'received'))
  assert.ok(!w.messages.some(m => m.type === 'error'))
  w.post({ type: 'deactivate', epoch: 2 })
  w.post({ type: 'activate', epoch: 3, nowUs: 2000000, contextTime: w.context.currentTime })
  w.post({ type: 'packet', epoch: 1, payload: tonePackets[1], sequence: 1, timestamp: 320, nowUs: 70000 })
  assert.equal(w.stats().received, 0)
  w.dispose()
})

test('only resource exhaustion stops actual sender or ingress; bounded capacity is from the shared ABI', () => {
  const w = worklet()
  for (let i = 0; i < 760; i++) w.render()
  assert.equal(w.messages.filter(m => m.type === 'encoded').length, 100)
  assert.ok(w.messages.some(m => m.type === 'error' && m.message.includes('backpressure')))
  w.dispose()
  const r = worklet()
  for (let seq = 0; seq < 201; seq++) r.post({ type: 'packet', payload: tonePackets[seq], sequence: seq, timestamp: seq * 320, nowUs: 0 })
  assert.equal(r.messages.filter(m => m.type === 'received').length, 200)
  assert.ok(r.messages.some(m => m.type === 'error' && m.message.includes('backpressure')))
  r.dispose()
})

test('shared receiver rejects non-negotiated stereo and40ms Opus before NetEq insertion', () => {
  for (const [channels, frames] of [[2, 320], [1, 640]]) {
    const w = worklet(), m = w.p.core
    const encoder = m._md_opus_encoder_create(16000, channels)
    const input = m._malloc(frames * channels * 4), output = m._malloc(1275)
    m.HEAPF32.fill(.1, input >> 2, (input >> 2) + frames * channels)
    const size = m._md_opus_encode_float(encoder, input, frames, output, 1275)
    assert.ok(size > 0)
    const payload = m.HEAPU8.slice(output, output + size)
    m._md_opus_encoder_destroy(encoder); m._free(input); m._free(output)
    w.post({ type: 'packet', payload, sequence: 0, timestamp: 0, nowUs: 0 })
    assert.ok(w.messages.some(message => message.type === 'error'))
    assert.equal(w.messages.filter(message => message.type === 'received').length, 0)
    w.dispose()
  }
})
