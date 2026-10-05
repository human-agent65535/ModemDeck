import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { Application, createDecoder, createEncoder } from 'libopus-wasm'
import { CallAudioReceiveClock, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError } from '../src/state/callAudioProtocol.ts'

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
  const ready = { type: 'ready', version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20 }
  assert.equal(isCallAudioReady(ready), true)
  for (const [key, value] of [['version', 2], ['codec', 'pcm'], ['sample_rate', 48000], ['channels', 2], ['frame_ms', 40]]) {
    assert.equal(isCallAudioReady({ ...ready, [key]: value }), false)
  }
})

test('receive clock rejects replay and inconsistent gaps while accepting uint32 wraps', () => {
  const clock = new CallAudioReceiveClock()
  const sequence = 0xfffffffe
  const timestamp = (sequence * 320) >>> 0
  assert.equal(clock.accept(sequence, timestamp, 0), true)
  assert.equal(clock.accept(0xffffffff, (timestamp + 320) >>> 0, 20), true)
  assert.equal(clock.accept(0, (timestamp + 640) >>> 0, 40), true)
  assert.equal(clock.accept(2, (timestamp + 1280) >>> 0, 80), true)
  assert.throws(() => clock.accept(2, (timestamp + 1280) >>> 0, 80))
  assert.throws(() => clock.accept(1, (timestamp + 960) >>> 0, 81))
  assert.throws(() => clock.accept(3, (timestamp + 1601) >>> 0, 100))
})

test('TCP backlog is dropped; stable current cadence can recover a higher latency baseline', () => {
  const clock = new CallAudioReceiveClock()
  assert.equal(clock.accept(0, 0, 0), true)
  for (let i = 1; i <= 10; i++) assert.equal(clock.accept(i, i * 320, 500), false, 'never replay a stalled burst')
  assert.equal(clock.accept(11, 11 * 320, 520), false)
  assert.equal(clock.accept(12, 12 * 320, 540), false)
  assert.equal(clock.accept(13, 13 * 320, 560), true, 'new real-time cadence re-anchors after 3 gaps')
  assert.equal(clock.accept(14, 14 * 320, 580), true)
  // A later stalled burst still has to qualify again.
  for (let i = 15; i <= 18; i++) assert.equal(clock.accept(i, i * 320, 1000), false)
})

test('stale TCP bursts remain stale after a warmed-up long-running clock', () => {
  const clock = new CallAudioReceiveClock()
  for (let i = 0; i <= 100; i++) assert.equal(clock.accept(i, i * 320, i * 20), true)
  for (let i = 101; i <= 110; i++) assert.equal(clock.accept(i, i * 320, 2520), false)
  assert.equal(clock.accept(111, 111 * 320, 2540), false)
  assert.equal(clock.accept(112, 112 * 320, 2560), false)
  assert.equal(clock.accept(113, 113 * 320, 2580), true)
})

test('transport error JSON followed by close retries once; format/ownership errors stay fatal', async () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const connect = ast.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === 'openAudioSocket')
    .getText(ast).replace('import.meta.url', '"https://calls.example.test/assets/app.js"')
  for (const code of ['transport_timeout', 'transport_closed', 'backpressure', 'invalid_audio', 'lease_expired', 'ownership_lost', 'unknown']) {
    let worker, connection, recovered = 0, failed = 0
    const ownership = { ownerToken: 'owner-a', claimed: false }
    const runtime = { context: { currentTime: 0 }, node: { port: { postMessage() {} } }, ready: false, lastActivity: 0 }
    const bindings = { URL, CALL_AUDIO_FORMAT: { version: 1 },
      audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined,
      MEDIA_CONNECT_TIMEOUT_MS: 5000, callMediaState: { status: 'connecting', error: '' },
      isCurrent: () => true, isCallAudioReady, isRecoverableCallAudioError, clearRecoveryWindow() {},
      translate: key => key, playRemoteAudio: async () => {},
      window: { location: { href: 'https://calls.example.test/' }, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 2 },
      callAudioWebSocketURL, Worker: class { constructor() { worker = this } },
      WebSocket: class { constructor() { connection = this } send() {} },
      recoverConnection: (callID, token, owner) => {
        assert.equal(callID, 'call-a'); assert.equal(token, 1); assert.equal(owner, ownership)
        recovered++; bindings.socket = undefined
      },
      failConnection: () => { failed++; bindings.socket = undefined }
    }
    const context = vm.createContext(bindings)
    // vm contextifies the bindings object; assign the current socket there when
    // the stub clears it, matching clearSocketResources in production.
    bindings.recoverConnection = (callID, token, owner) => {
      assert.equal(callID, 'call-a'); assert.equal(token, 1); assert.equal(owner, ownership)
      recovered++; context.socket = undefined
    }
    bindings.failConnection = () => { failed++; context.socket = undefined }
    vm.runInContext(ts.transpileModule(connect, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
    await context.openAudioSocket('call-a', 1, ownership)
    worker.onmessage({ data: { type: 'ready' } })
    connection.onopen()
    const laterClose = connection.onclose
    connection.onmessage({ data: JSON.stringify({ type: 'error', code, message: 'fault' }) })
    laterClose({ code: 1011 })
    assert.equal(recovered, isRecoverableCallAudioError(code) ? 1 : 0, code)
    assert.equal(failed, isRecoverableCallAudioError(code) ? 0 : 1, code)
    assert.equal(ownership.claimed, true, 'control ownership is not revoked by a transient media fault')
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
  const selected = ['isCurrent', 'clearRecoveryWindow', 'beginRecoveryWindow', 'settleMediaRecovery', 'recoverConnection', 'openAudioSocket']
  const code = ast.statements.filter(statement => ts.isFunctionDeclaration(statement) && selected.includes(statement.name?.text))
    .map(statement => statement.getText(ast)).join('\n').replaceAll('import.meta.url', '"https://calls.example.test/assets/app.js"')
  let worker, connection, now = 0, nextID = 0, failed = 0
  const timers = new Map(), intervals = new Map(), cleared = []
  const runtime = { context: { currentTime: 0 }, node: { port: { postMessage() {} } }, ready: false,
    lastActivity: Date.now(), recoveryAttempts: 0, sentFrames: 0, receivedFrames: 0, lastSentAt: -Infinity, lastReceivedAt: -Infinity }
  const context = vm.createContext({ URL, performance: { now: () => now }, generation: 1, currentCallID: 'call-a',
    audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined, reconnectTimeoutID: undefined,
    recoveryTimeoutID: undefined, socketWatchdogID: undefined, socketBufferedSince: undefined,
    MEDIA_CONNECT_TIMEOUT_MS: 5000, MEDIA_RECOVERY_TIMEOUT_MS: 15000, MEDIA_RECONNECT_DELAY_MS: 500,
    MEDIA_STABLE_WINDOW_MS: 1000, MEDIA_STABLE_FRAMES: 40,
    CALL_AUDIO_MAX_AGE_MS: 100,
    CALL_AUDIO_FORMAT: { version: 1 }, callMediaState: { status: 'connecting', error: '' },
    isCallAudioReady, isRecoverableCallAudioError, callAudioWebSocketURL, translate: key => key,
    playRemoteAudio: async () => {}, Worker: class { constructor() { worker = this } },
    WebSocket: class { constructor() { connection = this } send() {} },
    window: { location: { href: 'https://calls.example.test/' },
      setTimeout: (callback, delay) => { timers.set(++nextID, { callback, delay }); return nextID },
      clearTimeout: id => { cleared.push(id); timers.delete(id) },
      setInterval: callback => { intervals.set(++nextID, callback); return nextID },
      clearInterval: id => intervals.delete(id) },
    gateway: { releaseCallMedia: async () => {} },
    clearSocketResources: () => {
      context.socket = undefined
      runtime.ready = false
      runtime.worker = undefined
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
    worker.onmessage({ data: { type: 'ready' } })
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
  const runtime = { stableSince: 0, lastSentAt: 999, lastReceivedAt: 999, sentFrames: 40, receivedFrames: 40, recoveryAttempts: 5 }
  const context = vm.createContext({ recoveryTimeoutID: 1, socketBufferedSince: undefined,
    MEDIA_STABLE_WINDOW_MS: 1000, MEDIA_STABLE_FRAMES: 40, performance: { now: () => now },
    CALL_AUDIO_MAX_AGE_MS: 100,
    clearRecoveryWindow: () => { cleared++ } })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  now = 1000
  runtime.receivedFrames = 39
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  runtime.receivedFrames = 40
  context.socketBufferedSince = 900
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0)
  context.socketBufferedSince = undefined
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 0, 'buffering restarts the stable media window')
  now = 2000
  runtime.lastSentAt = runtime.lastReceivedAt = now
  context.settleMediaRecovery(runtime)
  assert.equal(cleared, 1)
  assert.equal(runtime.recoveryAttempts, 0)
})

test('pinned WASM libopus round-trips mono 16 kHz 20 ms frames without browser-native Opus', async () => {
  const encoder = await createEncoder({ sampleRate: 16000, channels: 1, frameSize: 320, application: Application.Voip, bitrate: 24000 })
  const decoder = await createDecoder({ sampleRate: 16000, channels: 1, maxFrameSize: 320 })
  try {
    let energy = 0
    for (let frame = 0; frame < 20; frame++) {
      const input = Float32Array.from({ length: 320 }, (_, i) => Math.sin((frame * 320 + i) * 2 * Math.PI * 440 / 16000) * 0.3)
      const opus = encoder.encodeFloat(input, { maxPacketBytes: 1275 })
      assert.ok(opus.length > 0 && opus.length <= 1275)
      const pcm = decoder.decodeFloat(decodeCallAudioPacket(encodeCallAudioPacket(frame, opus)).payload, { maxFrameSize: 320 })
      assert.equal(pcm.length, 320)
      assert.ok(pcm.every(Number.isFinite))
      energy += pcm.reduce((sum, value) => sum + value * value, 0)
    }
    assert.ok(energy > 100, 'decoded audio contains the input tone')
    const silence = decoder.decodeFloat(encoder.encodeFloat(new Float32Array(320)))
    assert.equal(silence.length, 320)
    const longEncoder = await createEncoder({ sampleRate: 16000, channels: 1, frameSize: 640 })
    try { assert.throws(() => decoder.decodeFloat(longEncoder.encodeFloat(new Float32Array(640)), { maxFrameSize: 320 })) }
    finally { longEncoder.free() }
  } finally {
    encoder.free()
    decoder.free()
  }
})

function worklet(rate) {
  const messages = []
  let Processor
  const source = readFileSync(new URL('../src/state/callAudio.worklet.ts', import.meta.url), 'utf8')
  const context = vm.createContext({ sampleRate: rate, currentTime: 0, Float32Array,
    AudioWorkletProcessor: class { port = { postMessage: message => messages.push(message), onmessage: null } },
    registerProcessor: (_name, value) => { Processor = value } })
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  return { processor: new Processor(), messages, context }
}

test('worklet capture resamples native hardware into exact20ms transport frames', () => {
  for (const rate of [8000, 12000, 16000, 44100, 48000]) {
    const { processor, messages, context } = worklet(rate)
    const samples = Math.ceil(rate / 128)
    for (let block = 0; block < samples; block++) {
      context.currentTime = block * 128 / rate
      processor.process([[new Float32Array(128).fill(0.25)]], [[new Float32Array(128)]])
    }
    assert.ok(messages.length >= 49 && messages.length <= 50)
    assert.ok(messages.every((message, i) => message.pcm.length === 320 && message.index === i))
    assert.ok(messages[1].pcm.every(value => Math.abs(value - 0.25) < 0.001))
  }
})

test('worklet playback bounds delay, drops stale messages and clears audio on reconnect', () => {
  const { processor, context } = worklet(48000)
  processor.port.onmessage({ data: { type: 'play', pcm: new Float32Array(320).fill(0.1), sentAt: -1 } })
  assert.equal(processor.playbackSize, 0)
  for (let i = 0; i < 20; i++) processor.port.onmessage({ data: { type: 'play', pcm: new Float32Array(320).fill(i / 20), sentAt: 0 } })
  assert.equal(processor.playbackSize, 1600)
  const rendered = new Float32Array(128)
  processor.process([], [[rendered]])
  assert.ok(rendered.every(value => value >= 0.75), 'latest audio wins after a stall')
  processor.port.onmessage({ data: { type: 'clear' } })
  context.currentTime = 1
  processor.process([], [[rendered]])
  assert.ok(rendered.every(value => value === 0))
})

test('all ingress API listeners forward only WebSocket upgrades and preserve transport protections', () => {
  const nginx = readFileSync(new URL('../nginx.conf', import.meta.url), 'utf8')
  assert.match(nginx, /map \$http_upgrade \$websocket_upgrade[\s\S]*?default '';[\s\S]*?~\*\^websocket\$ websocket/)
  const listeners = nginx.match(/location \/api\/ \{[\s\S]*?\n        \}/g)
  assert.equal(listeners.length, 3)
  for (const location of listeners) {
    for (const header of ['Upgrade $websocket_upgrade', 'Connection $websocket_connection', 'Host $http_host', 'X-Forwarded-Host $http_host']) assert.ok(location.includes('proxy_set_header ' + header))
    for (const directive of ['proxy_http_version 1.1', 'proxy_request_buffering off', 'proxy_buffering off', 'proxy_cache off', 'proxy_read_timeout 1h', 'proxy_send_timeout 1h']) assert.ok(location.includes(directive))
  }
  assert.match(nginx, /script-src 'self' 'wasm-unsafe-eval'/)
  assert.match(nginx, /worker-src 'self'; connect-src 'self' wss:\/\/\$http_host/)
})
