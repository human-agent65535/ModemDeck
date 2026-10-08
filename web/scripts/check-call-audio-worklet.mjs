import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import vm from 'node:vm'

// Exercise the built module, including Vite's worker transform and generated
// Emscripten glue. WebKit's actual AudioWorklet has console, but no Dedicated-
// Worker self/location, URL or performance. Source-only factory tests miss this.
const assets = new URL('../dist/assets/', import.meta.url)
const names = await readdir(assets)
const candidates = await Promise.all(names.filter(name => /^callAudio\.worklet-.*\.js$/.test(name))
  .map(async name => ({ name, source: await readFile(new URL(name, assets), 'utf8') })))
const asset = candidates.find(({ source }) => source.includes('registerProcessor') && source.includes('modemdeck-call-audio'))
const wasm = names.find(name => /^audioCore-.*\.wasm$/.test(name))
assert.ok(asset && wasm, 'production processor and WASM must be separately emitted')
const wasmBinary = await readFile(new URL(wasm, assets))
let Processor
const messages = []
const context = vm.createContext({
  console: { log() {}, warn() {}, error() {} }, WebAssembly, sampleRate: 48000, currentTime: 0,
  AudioWorkletProcessor: class { port = { postMessage: message => messages.push(message), onmessage: undefined } },
  registerProcessor: (name, value) => { assert.equal(name, 'modemdeck-call-audio'); Processor = value }
})
for (const name of ['self', 'location', 'URL', 'performance', 'fetch', 'SharedArrayBuffer']) {
  // V8 exposes SharedArrayBuffer by default even in an otherwise empty context.
  context[name] = undefined
}
const module = new vm.SourceTextModule(asset.source, { context,
  initializeImportMeta(meta) { meta.url = new URL(asset.name, assets).href }
})
await module.link(() => { throw new Error('Unexpected external production worklet import') })
await module.evaluate()
const processor = new Processor({ processorOptions: { wasmBinary } })
assert.equal(messages[0]?.type, 'ready')
assert.equal(messages[0].shared, false)
processor.port.onmessage({ data: { type: 'activate', epoch: 1, nowUs: 0, contextTime: 0, sequence: 0 } })
const packets = []
let encoded = 0, received = 0, energy = 0
const rate = 48000, seconds = 3, quantum = 128
for (let offset = 0; offset < seconds * rate; offset += quantum) {
  context.currentTime = offset / rate
  while (packets.length && packets[0].arrival <= context.currentTime) {
    const packet = packets.shift()
    processor.port.onmessage({ data: { type: 'packet', epoch: 1, sequence: packet.sequence,
      timestamp: (packet.sequence * 320) >>> 0, payload: packet.payload, nowUs: packet.arrival * 1e6 } })
  }
  const count = Math.min(quantum, seconds * rate - offset)
  const input = Float32Array.from({ length: count }, (_, i) => Math.sin((offset + i) * 2 * Math.PI * 440 / rate) * .3)
  const output = new Float32Array(count)
  assert.equal(processor.process([[input]], [[output]]), true)
  assert.ok(output.every(Number.isFinite))
  if (offset >= rate) energy += output.reduce((sum, value) => sum + value * value, 0)
  for (const message of messages.splice(0)) {
    assert.notEqual(message.type, 'error', message.message)
    if (message.type === 'received') received++
    if (message.type === 'encoded') {
      assert.equal(message.sequence, encoded++)
      assert.ok(message.payload.length > 0 && message.payload.length <= 1275)
      packets.push({ ...message, arrival: (Math.floor(message.sequence / 5) + 1) / 10 + .05 })
      processor.port.onmessage({ data: { type: 'ack', epoch: 1 } })
    }
  }
}
processor.port.onmessage({ data: { type: 'stats', epoch: 1 } })
const snapshot = messages.find(message => message.type === 'stats')?.snapshot
assert.ok(snapshot, 'actual built receiver supplies its real shared stats')
const stats = new DataView(snapshot.buffer, snapshot.byteOffset, snapshot.byteLength)
assert.equal(encoded, seconds * 50)
assert.ok(received >= 140 && energy > 1000)
assert.equal(stats.getUint32(80, true), 48000)
assert.ok(stats.getBigUint64(104, true) >= 48000n, 'actual NetEq renders useful decoded audio')
assert.equal(stats.getBigUint64(120, true), 0n)
processor.port.onmessage({ data: { type: 'deactivate', epoch: 2 } })
console.log(`Built AudioWorklet ${asset.name}: native48k encode/render passed without self/location/URL/performance/SAB`)
