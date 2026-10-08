import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { instantiateAudioCore } from '../src/state/audioCore.ts'
import { Application, createDecoder, createEncoder } from 'libopus-wasm'
import { CallAudioReceiveClock, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError } from '../src/state/callAudioProtocol.ts'

const audioCoreModule = new WebAssembly.Module(readFileSync(new URL('../src/state/audioCore.wasm', import.meta.url)))

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
  const clock = new CallAudioReceiveClock(audioCoreModule)
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

test('Go, Swift and Web share complete-window clock vectors including batches, mixed freshness and wraps', () => {
  const vectors = JSON.parse(readFileSync(new URL('../../internal/callmedia/testdata/socket_clock_vectors.json', import.meta.url), 'utf8'))
  for (const trace of vectors.cases) {
    const clock = new CallAudioReceiveClock(audioCoreModule)
    for (const frame of trace.frames) {
      const label = `${trace.name} sequence${frame.sequence}`
      assert.equal(clock.accept(frame.sequence, frame.timestamp, frame.arrival_us / 1000), frame.accepted, label)
      assert.equal(clock.generation, frame.generation, label + ' generation')
      if (frame.accepted) assert.ok(Math.abs(clock.playAt * 1000 - frame.play_us) < 0.01, label + ' source slot')
    }
  }
})

test('mixed fresh/stale batch tails cannot perpetually cancel higher-baseline recovery', () => {
  for (const step of [110, 130, 150, 170, 200]) {
    const clock = new CallAudioReceiveClock(audioCoreModule)
    const drops = []
    for (let batch = 0; batch < 100; batch++) {
      let dropped = 0
      for (let i = 0; i < 5; i++) {
        const seq = batch * 5 + i
        if (!clock.accept(seq, (seq * 320) >>> 0, batch * 100 + (batch >= 5 ? step : 0))) dropped++
      }
      drops.push(dropped)
    }
    assert.ok(drops.slice(10).every(n => n === 0), `persistent step ${step}ms settled`)
    assert.equal(clock.generation, 2, 'one recovery, no recurring re-anchor')
  }
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
  const context = vm.createContext({ sampleRate: rate, currentTime: 0, Float32Array, instantiateAudioCore,
    AudioWorkletProcessor: class { port = { postMessage: message => messages.push(message), onmessage: null } },
    registerProcessor: (_name, value) => { Processor = value } })
  vm.runInContext(ts.transpileModule(source.replace(/^import .*\n/gm, ''), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  return { processor: new Processor({ processorOptions: { coreModule: audioCoreModule } }), messages, context }
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
    assert.ok(messages.every((message, i) => Math.abs(message.time - (i + 1) * 0.02) <= 1 / rate + 1e-9), 'capture time is sample-end, independent of render-block phase')
  }
})

function enqueuePCM(processor, sequence, playAt, value = 0.25, generation = 1) {
  processor.port.onmessage({ data: { type: 'play', pcm: new Float32Array(320).fill(value), sequence, playAt, generation, sourceSamples: sequence * 320 } })
}

function renderUntil(fixture, seconds, rate = 48000) {
  const samples = []
  for (; fixture.context.currentTime < seconds - 1e-9; fixture.context.currentTime += 128 / rate) {
    const output = new Float32Array(128)
    fixture.processor.process([], [[output]])
    samples.push(...output)
  }
  return samples
}

test('worklet preserves all healthy40/100ms burst slots at native sample rates', () => {
  for (const rate of [8000, 16000, 44100, 48000]) {
    for (const batch of [2, 5]) {
      const fixture = worklet(rate)
      const { processor, context } = fixture
      for (let block = 0; block < Math.ceil(rate / 128); block++) {
        context.currentTime = block * 128 / rate
        const first = Math.floor((context.currentTime + 1e-9) / (batch * 0.02)) * batch
        if (first !== fixture.lastFirst) {
          fixture.lastFirst = first
          for (let j = 0; j < batch; j++) enqueuePCM(processor, first + j, (first + j) * 0.02 + 0.04)
        }
        const output = new Float32Array(128)
        processor.process([], [[output]])
        for (let i = 0; i < output.length; i++) {
          const now = context.currentTime + i / rate
          assert.ok(Math.abs(output[i] - (now >= 0.04 - 0.5 / rate ? 0.25 : 0)) < 1e-5,
            `rate${rate}/batch${batch}ms at${now}: ${output[i]}`)
        }
        assert.ok(processor.playback.length <= 7)
      }
    }
  }
})

test('worklet keeps omitted source slots silent and clears old-generation PCM immediately', () => {
  const fixture = worklet(48000)
  enqueuePCM(fixture.processor, 0, 0.04)
  enqueuePCM(fixture.processor, 2, 0.08, 0.5)
  const samples = renderUntil(fixture, 0.1)
  const at = t => samples[Math.round(t * 48000)]
  assert.equal(at(0.05), 0.25)
  assert.equal(at(0.07), 0, 'missing sequence1 must not collapse')
  assert.equal(at(0.09), 0.5)
  fixture.processor.port.onmessage({ data: { type: 'clear', generation: 2 } })
  enqueuePCM(fixture.processor, 3, 0.12, 0.9, 1)
  enqueuePCM(fixture.processor, 50, 0.14, 0.75, 2)
  assert.equal(fixture.processor.playback.length, 1, 'late old worker reply cannot enter a new epoch')
  const output = renderUntil(fixture, 0.17)
  assert.ok(output.every(value => value === 0 || value === 0.75))
})

test('worklet bounds the queue, expires original source deadlines and replaces underrun prefill', () => {
  const fixture = worklet(48000)
  const { processor, context } = fixture
  for (let i = 0; i < 20; i++) enqueuePCM(processor, i, i * 0.02 + 0.04, i / 20)
  assert.equal(processor.playback.length, 7)
  assert.equal(processor.playback[0].sequence, 13)
  processor.port.onmessage({ data: { type: 'clear' } })
  enqueuePCM(processor, 0, 0.04)
  renderUntil(fixture, 0.07)
  assert.equal(processor.starving, true)
  for (let epoch = 0; epoch < 10; epoch++) {
    context.currentTime = 0.1 + epoch * 0.1
    enqueuePCM(processor, epoch + 1, context.currentTime - 0.02)
    const output = new Float32Array(128)
    processor.process([], [[output]])
    assert.ok(output.every(value => value === 0))
    assert.ok(Math.abs(processor.epochStartedAt - context.currentTime - 0.04) < 1e-9, 'new epoch replaces rather than accumulates prefill')
    renderUntil(fixture, context.currentTime + 0.07)
  }
  context.currentTime += 1
  enqueuePCM(processor, 99, context.currentTime - 0.09)
  processor.process([], [[new Float32Array(128)]])
  assert.equal(processor.playback.length, 0, '40ms rebuffer cannot renew a90ms-old deadline')
  enqueuePCM(processor, 100, context.currentTime - 0.2)
  assert.equal(processor.playback.length, 0, 'stale worker messages never queue')
})

test('fixed worklet epoch keeps continuous samples despite receive-clock drift updates', () => {
  const rate = 48000, fixture = worklet(rate)
  const { processor, context } = fixture
  let next = 0
  for (let block = 0; block < Math.ceil(3 * rate / 128); block++) {
    context.currentTime = block * 128 / rate
    while (next * 0.02 <= context.currentTime + 1e-9) {
      enqueuePCM(processor, next, next * 0.02 + 0.04 + Math.floor(next / 5) * 0.0001)
      next++
    }
    const output = new Float32Array(128)
    processor.process([], [[output]])
    for (let i = 0; i < output.length; i++) {
      assert.equal(output[i], context.currentTime + i / rate >= 0.04 - 1e-9 ? 0.25 : 0,
        'per-window anchor corrections must not add periodic silence to a running epoch')
    }
  }
})

test('a worklet stall skips the old half-frame and resumes inside the current fixed source slot', () => {
  const { processor, context } = worklet(16000)
  enqueuePCM(processor, 0, 0.04, 0.25)
  enqueuePCM(processor, 2, 0.08, 0.5)
  processor.process([], [[new Float32Array(128)]]) // establish40ms epoch
  context.currentTime = 0.04
  const firstHalf = new Float32Array(160)
  processor.process([], [[firstHalf]])
  assert.ok(firstHalf.every(value => value === 0.25))
  context.currentTime = 0.08
  const afterStall = new Float32Array(128)
  processor.process([], [[afterStall]])
  assert.ok(afterStall.every(value => value === 0.5), 'expired half-frame cannot delay source2')
  assert.equal(processor.epochStartedAt, 0.04, 'queue remained nonempty; no false underrun epoch')
  // Derive the interpolation position from the actual cursor even mid-packet.
  processor.playback[0].pcm = Float32Array.from({ length: 320 }, (_, i) => i / 320)
  context.currentTime = 0.09
  processor.process([], [[afterStall]])
  assert.equal(afterStall[0], 0.5, '90ms lies160 source samples into the80ms slot')
})

test('WASM clocks are isolated and source sample expansion survives full uint32 wrap', () => {
  const clone = structuredClone(audioCoreModule)
  const clock = new CallAudioReceiveClock(clone)
  const other = new CallAudioReceiveClock(audioCoreModule)
  let sequence = 0, sourceFrames = 0
  assert.equal(clock.accept(sequence, 0, 0), true)
  for (const advance of [0x7ffffff0, 0x7ffffff0, 100]) {
    sequence = (sequence + advance) >>> 0
    sourceFrames += advance
    assert.equal(clock.accept(sequence, (sourceFrames * 320) >>> 0, sourceFrames * 20), true)
    assert.equal(clock.sourceSamples, sourceFrames * 320)
  }
  assert.throws(() => clock.accept((sequence + 1) >>> 0, ((sourceFrames + 1) * 320) >>> 0, NaN))
  assert.equal(clock.sourceSamples, sourceFrames * 320, 'invalid wall clock cannot mutate source progress')
  assert.equal(other.generation, 0, 'cloned module creates isolated instances')
  assert.equal(other.accept(0, 0, 0), true)
})

test('production WebSocket dispatch admits a five-frame burst and rejects old-generation decoder replies', async () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const code = ast.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === 'openAudioSocket')
    .getText(ast).replace('import.meta.url', '"https://calls.example.test/assets/app.js"')
  let worker, connection, now = 0
  const decoded = [], played = []
  const runtime = { context: { currentTime: 0, state: 'running' }, node: { port: { postMessage: message => played.push(message) } },
    ready: false, contextEpoch: 0, encodePending: 0, decodePending: 0, encodeInFlight: 0, decodeInFlight: 0, clock: new CallAudioReceiveClock(audioCoreModule), receivedFrames: 0 }
  const context = vm.createContext({ URL, ArrayBuffer, performance: { now: () => now },
    audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined, socketWatchdogID: undefined,
    MAX_CODEC_PENDING: 3, MEDIA_CONNECT_TIMEOUT_MS: 5000, CALL_AUDIO_MAX_AGE_MS: 100,
    CALL_AUDIO_FORMAT: { version: 1 }, callMediaState: {}, isCurrent: () => true,
    isCallAudioReady, isRecoverableCallAudioError, callAudioWebSocketURL, decodeCallAudioPacket,
    translate: key => key, playRemoteAudio: async () => {},
    failConnection: (_id, _token, error) => { throw error },
    window: { location: { href: 'https://calls.example.test/' }, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 2 },
    Worker: class { constructor() { worker = this } postMessage(message) { decoded.push(message) } },
    WebSocket: class { constructor() { connection = this } send() {} }
  })
  vm.runInContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  await context.openAudioSocket('call-a', 1, { ownerToken: 'owner-a' })
  worker.onmessage({ data: { type: 'ready' } })
  connection.onmessage({ data: JSON.stringify({ type: 'ready', version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20 }) })
  for (let seq = 0; seq < 5; seq++) connection.onmessage({ data: encodeCallAudioPacket(seq, new Uint8Array([0xf8])) })
  assert.equal(decoded.length, 5, 'codec admission must accommodate a healthy transport batch')
  assert.deepEqual(decoded.map(packet => Math.round(packet.playAt * 1000)), [40, 60, 80, 100, 120])
  for (const packet of decoded) worker.onmessage({ data: { ...packet, type: 'decoded', pcm: new Float32Array(320) } })
  assert.equal(runtime.decodePending, 0)
  assert.equal(played.filter(message => message.type === 'play').length, 5)
  // A reply from a prior source timeline cannot refill the queue after clear.
  for (let seq = 5; seq < 30; seq++) {
    now = Math.floor(seq / 5) * 100 + 130
    runtime.context.currentTime = now / 1000
    connection.onmessage({ data: encodeCallAudioPacket(seq, new Uint8Array([0xf8])) })
    const request = decoded.at(-1)
    if (request?.sequence === seq) worker.onmessage({ data: { ...request, type: 'decoded', pcm: new Float32Array(320) } })
  }
  assert.equal(runtime.clock.generation, 2)
  const count = played.length
  worker.onmessage({ data: { ...decoded[0], type: 'decoded', pcm: new Float32Array(320) } })
  assert.equal(played.length, count)
  assert.ok(played.some(message => message.type === 'clear' && message.generation === 2))
})

test('non-running audio contexts isolate interrupted, suspended and closed media epochs without renewing the receive clock', async () => {
  const source = readFileSync(new URL('../src/state/callMedia.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('media.ts', source, ts.ScriptTarget.Latest, true)
  const functions = ast.statements.filter(statement => ts.isFunctionDeclaration(statement) &&
    ['openAudioSocket', 'playRemoteAudio'].includes(statement.name?.text)).map(statement => statement.getText(ast)).join('\n')
    .replaceAll('import.meta.url', '"https://calls.example.test/assets/app.js"')
  const callbacks = []
  function visit(node) {
    if (ts.isBinaryExpression(node) && ['context.onstatechange', 'node.port.onmessage'].includes(node.left.getText(ast))) {
      callbacks.push(node.getText(ast))
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
  for (const interruptedState of ['interrupted', 'suspended', 'closed']) {
    let worker, connection, now = 0, resumes = 0
    const requests = [], posted = [], sent = []
    const node = { port: { postMessage: message => posted.push(message) } }
    const audioContext = { currentTime: 0, state: 'running', resume: async () => { resumes++; audioContext.state = 'running' } }
    const runtime = { context: audioContext, node, ready: false,
      contextEpoch: 0, captureCutoff: -Infinity, encodePending: 0, decodePending: 0,
      encodeInFlight: 0, decodeInFlight: 0,
      clock: new CallAudioReceiveClock(audioCoreModule), receivedFrames: 0, sentFrames: 0 }
    const bindings = { URL, ArrayBuffer, performance: { now: () => now },
      context: audioContext, runtime, node, callID: 'call-a', token: 1,
      audioRuntime: runtime, socket: undefined, connectTimeoutID: undefined, socketWatchdogID: undefined, socketBufferedSince: undefined,
      MEDIA_CONNECT_TIMEOUT_MS: 5000, CALL_AUDIO_MAX_AGE_MS: 100, MAX_SOCKET_BUFFER_BYTES: 6435,
      CALL_AUDIO_FORMAT: { version: 1 }, callMediaState: {}, isCurrent: () => true,
      isCallAudioReady, isRecoverableCallAudioError, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket,
      translate: key => key, failConnection: (_id, _token, error) => { throw error },
      remoteAudio: { play: async () => {}, pause() {} }, audioState: { callVolume: 100 }, applySelectedAudioOutput: async () => true,
      window: { location: { href: 'https://calls.example.test/' }, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 2 },
      Worker: class { constructor() { worker = this } postMessage(message) { requests.push(message) } },
      WebSocket: class { static OPEN = 1; readyState = 1; bufferedAmount = 0; constructor() { connection = this } send(data) { sent.push(data) } }
    }
    const fixture = vm.createContext(bindings)
    vm.runInContext(ts.transpileModule(functions + '\n' + callbacks.join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, fixture)
    await fixture.openAudioSocket('call-a', 1, { ownerToken: 'owner-a' })
    worker.onmessage({ data: { type: 'ready' } })
    connection.onmessage({ data: JSON.stringify({ type: 'ready', version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20 }) })
    connection.onmessage({ data: encodeCallAudioPacket(0, new Uint8Array([0xf8])) })
    audioContext.currentTime = 0.02
    node.port.onmessage({ data: { type: 'capture', pcm: new Float32Array(320), index: 0, time: 0.02 } })
    const oldDecode = requests.find(request => request.type === 'decode')
    const oldEncode = requests.find(request => request.type === 'encode')
    now = 20
    connection.onmessage({ data: encodeCallAudioPacket(1, new Uint8Array([0xf8])) })
    audioContext.currentTime = 0.04
    node.port.onmessage({ data: { type: 'capture', pcm: new Float32Array(320), index: 1, time: 0.04 } })
    const oldDecodeError = requests.findLast(request => request.type === 'decode')
    const oldEncodeError = requests.findLast(request => request.type === 'encode')
    const clearCount = posted.filter(message => message.type === 'clear').length
    audioContext.state = interruptedState
    audioContext.onstatechange()
    assert.equal(fixture.callMediaState.playbackBlocked, true, interruptedState + ' must expose Resume/blocked audio')
    assert.equal(posted.filter(message => message.type === 'clear').length, clearCount + 1)
    assert.equal(runtime.encodePending, 0)
    assert.equal(runtime.decodePending, 0)
    for (let seq = 2; seq < 100; seq++) {
      now = seq * 20
      connection.onmessage({ data: encodeCallAudioPacket(seq, new Uint8Array([0xf8])) })
    }
    assert.equal(requests.filter(request => request.type === 'decode').length, 2, 'frozen audio time cannot admit PCM')
    assert.equal(runtime.clock.sourceSamples, 99 * 320, 'performance-source clock continues during interruption')
    assert.equal(runtime.clock.generation, 1, 'interruption must not replace the source clock anchor')
    const beforeCapture = requests.length
    node.port.onmessage({ data: { type: 'capture', pcm: new Float32Array(320), index: 1, time: 0.02 } })
    assert.equal(requests.length, beforeCapture)
    // Resume may deliver old worker results after state is running again.
    audioContext.state = 'running'
    audioContext.onstatechange()
    audioContext.currentTime = 0.06
    node.port.onmessage({ data: { type: 'capture', pcm: new Float32Array(320), index: 2, time: 0.04 } })
    assert.equal(requests.length, beforeCapture, 'queued pre-interruption capture includes the cutoff boundary')
    node.port.onmessage({ data: { type: 'capture', pcm: new Float32Array(320), index: 3, time: 0.06 } })
    assert.equal(runtime.encodePending, 1)
    now = 2000
    connection.onmessage({ data: encodeCallAudioPacket(100, new Uint8Array([0xf8])) })
    assert.equal(runtime.decodePending, 1)
    const plays = posted.filter(message => message.type === 'play').length
    worker.onmessage({ data: { ...oldDecode, type: 'decoded', pcm: new Float32Array(320) } })
    worker.onmessage({ data: { ...oldEncode, type: 'encoded', payload: new Uint8Array([0xf8]) } })
    assert.equal(posted.filter(message => message.type === 'play').length, plays)
    assert.equal(sent.length, 0, 'pre-interruption encoder result cannot upload after Resume')
    assert.equal(runtime.encodePending, 1, 'old callback cannot decrement current epoch pending')
    assert.equal(runtime.decodePending, 1)
    assert.equal(runtime.receivedFrames, 0)
    assert.equal(runtime.sentFrames, 0)
    worker.onmessage({ data: { type: 'error', requestType: 'decode', contextEpoch: oldDecodeError.contextEpoch, message: 'old decode failed' } })
    worker.onmessage({ data: { type: 'error', requestType: 'encode', contextEpoch: oldEncodeError.contextEpoch, message: 'old encode failed' } })
    assert.equal(runtime.encodePending, 1, 'old error cannot decrement current epoch pending or fail the new epoch')
    assert.equal(runtime.decodePending, 1)
    assert.equal(runtime.encodeInFlight, 1, 'old errors return only their physical worker capacity')
    assert.equal(runtime.decodeInFlight, 1)
    const currentDecode = requests.findLast(request => request.type === 'decode')
    const currentEncode = requests.findLast(request => request.type === 'encode')
    worker.onmessage({ data: { ...currentDecode, type: 'decoded', pcm: new Float32Array(320) } })
    worker.onmessage({ data: { ...currentEncode, type: 'encoded', payload: new Uint8Array([0xf8]) } })
    assert.equal(posted.filter(message => message.type === 'play').length, plays + 1)
    assert.equal(sent.length, 1)
    audioContext.state = interruptedState
    audioContext.onstatechange()
    await fixture.playRemoteAudio()
    assert.equal(resumes, interruptedState === 'closed' ? 0 : 1)
    assert.equal(fixture.callMediaState.playbackBlocked, interruptedState === 'closed')
  }
})

test('Opus worker resets predictive state only across source discontinuities and retains slot metadata', async () => {
  const source = readFileSync(new URL('../src/state/callOpus.worker.ts', import.meta.url), 'utf8').replace(/^import .*\n/gm, '')
  let resolvesReady, nextResponse, decoderCreations = 0
  const ready = new Promise(resolve => { resolvesReady = resolve })
  const context = vm.createContext({ Application, createEncoder,
    createDecoder: async options => { decoderCreations++; return createDecoder(options) },
    postMessage: data => { if (data.type === 'ready') resolvesReady(); else nextResponse(data) } })
  vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context)
  await ready
  const encoder = await createEncoder({ sampleRate: 16000, channels: 1, frameSize: 320 })
  try {
    for (const [sequence, generation, expectedCreations] of [[0, 1, 1], [1, 1, 1], [3, 1, 2], [4, 2, 3]]) {
      const response = new Promise(resolve => { nextResponse = resolve })
      const payload = encoder.encodeFloat(new Float32Array(320).fill(0.1))
      context.onmessage({ data: { type: 'decode', payload, sequence, generation, sourceSamples: sequence * 320, playAt: sequence * 0.02 + 0.04, contextEpoch: 7 } })
      const decoded = await response
      assert.equal(decoded.type, 'decoded')
      assert.equal(decoded.sequence, sequence)
      assert.equal(decoded.generation, generation)
      assert.equal(decoded.sourceSamples, sequence * 320)
      assert.equal(decoded.playAt, sequence * 0.02 + 0.04)
      assert.equal(decoded.contextEpoch, 7)
      assert.equal(decoded.pcm.length, 320)
      assert.equal(decoderCreations, expectedCreations)
    }
    const response = new Promise(resolve => { nextResponse = resolve })
    context.onmessage({ data: { type: 'decode', payload: new Uint8Array([4]), sequence: 5, generation: 2, contextEpoch: 8 } })
    const failed = await response
    assert.equal(failed.type, 'error')
    assert.equal(failed.requestType, 'decode')
    assert.equal(failed.contextEpoch, 8)
  } finally { encoder.free() }
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
