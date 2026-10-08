import createAudioCore from './audioCore.mjs'
import type { AudioCoreModule } from './audioCore.ts'

declare const sampleRate: number
declare const currentTime: number
declare class AudioWorkletProcessor {
  readonly port: MessagePort
  constructor()
}
declare function registerProcessor(name: string, processor: new (options: AudioWorkletNodeOptions) => AudioWorkletProcessor): void

type AudioMessage = {
  type: string
  epoch: number
  nowUs: number
  contextTime: number
  sequence?: number
  timestamp: number
  payload: Uint8Array
}

// One rendering owner runs the real shared NetEq receiver and Opus encoder.
// Only transport packets cross MessagePort; there is no second PCM jitter queue.
class CallAudioProcessor extends AudioWorkletProcessor {
  private readonly core: AudioCoreModule
  private receiver = 0
  private encoder = 0
  private readonly input: number
  private readonly encoded: number
  private readonly output: number
  private readonly packet: number
  private readonly stats: number
  private readonly statsView: DataView
  private healthFrames = 0
  private epoch = 0
  private active = false
  private failed = false
  private captureSize = 0
  private sequence = 0
  private pendingSends = 0
  private originUs = 0
  private originContextTime = 0

  constructor(options: AudioWorkletNodeOptions) {
    super()
    // The supported synchronous wasmBinary entry compiles during setup. No
    // fetch, compile, allocation of PCM blocks or decoder creation in process.
    if (sampleRate !== 48000) throw new Error('Call audio requires a 48 kHz AudioContext')
    this.core = createAudioCore({ locateFile: name => name, wasmBinary: options.processorOptions.wasmBinary as ArrayBuffer, print() {}, printErr() {} })
    this.input = this.core._malloc(960 * 4)
    this.encoded = this.core._malloc(1275)
    this.output = this.core._malloc(2048 * 4)
    this.packet = this.core._malloc(1275)
    this.stats = this.core._malloc(256)
    this.statsView = new DataView(this.core.HEAPU8.buffer, this.stats, 136)
    if (!this.input || !this.encoded || !this.output || !this.packet || !this.stats) throw new Error('Call audio allocation failed')
    this.port.onmessage = ({ data }: MessageEvent<AudioMessage>) => {
      try {
        if (data.type === 'activate') {
          this.deactivate()
          this.epoch = data.epoch
          this.originUs = data.nowUs
          this.originContextTime = data.contextTime
          if (data.sequence !== undefined) this.sequence = data.sequence >>> 0
          this.receiver = this.core._md_neteq_create(sampleRate)
          this.encoder = this.core._md_opus_encoder_create(48000, 1)
          if (!this.receiver || !this.encoder) throw new Error('Call audio initialization failed')
          this.failed = false
          this.active = true
        } else if (data.type === 'deactivate') {
          this.deactivate()
          this.epoch = data.epoch
        } else if (data.epoch === this.epoch && this.active) {
          if (data.type === 'stats') {
            if (this.core._md_neteq_get_stats(this.receiver, this.stats) !== 0) throw new Error('Call audio stats failed')
            // Copy the C ABI snapshot; never transfer the Wasm heap.
            this.port.postMessage({ type: 'stats', epoch: this.epoch, snapshot: this.core.HEAPU8.slice(this.stats, this.stats + 136) })
          } else if (data.type === 'ack') this.pendingSends = Math.max(0, this.pendingSends - 1)
          else if (data.type === 'packet') {
            if (!data.payload?.length || data.payload.length > 1275) throw new Error('Invalid Opus packet')
            this.core.HEAPU8.set(data.payload, this.packet)
            const result = this.core._md_neteq_enqueue(this.receiver, this.packet, data.payload.length,
              (data.sequence ?? 0) & 0xffff, (data.timestamp * 3) >>> 0, BigInt(Math.round(data.nowUs)))
            if (result !== 0) throw new Error(result === -5 ? 'Call audio ingress backpressure' : 'Invalid call audio packet')
            this.port.postMessage({ type: 'received', epoch: this.epoch })
          }
        }
      } catch (error) { this.fail(error) }
    }
    this.port.postMessage({ type: 'ready', ingressCapacity: this.core._md_audio_ingress_capacity(),
      sendCapacity: this.core._md_audio_send_capacity(), memoryBytes: this.core.HEAPU8.buffer.byteLength,
      shared: typeof SharedArrayBuffer !== 'undefined' && this.core.HEAPU8.buffer instanceof SharedArrayBuffer,
      initializationTime: currentTime })
  }

  private deactivate(): void {
    this.active = false
    if (this.receiver) this.core._md_neteq_destroy(this.receiver)
    if (this.encoder) this.core._md_opus_encoder_destroy(this.encoder)
    this.receiver = this.encoder = 0
    this.captureSize = this.pendingSends = this.healthFrames = 0
  }

  private fail(error: unknown): void {
    if (this.failed) return
    this.failed = true
    this.deactivate()
    this.port.postMessage({ type: 'error', epoch: this.epoch, message: String(error) })
  }

  process(inputs: Float32Array[][], outputs: Float32Array[][]): boolean {
    const output = outputs[0]?.[0]
    if (!output) return true
    output.fill(0)
    if (!this.active) return true
    try {
      if (output.length > 2048) throw new Error('Unsupported audio render quantum')
      const nowUs = this.originUs + (currentTime - this.originContextTime) * 1e6
      const result = this.core._md_neteq_render_float(this.receiver, BigInt(Math.round(nowUs)), this.output, output.length)
      if (result !== output.length) throw new Error('Call audio render failed')
      output.set(this.core.HEAPF32.subarray(this.output >> 2, (this.output >> 2) + output.length))
      this.healthFrames += output.length
      if (this.healthFrames >= sampleRate / 10) {
        this.healthFrames %= sampleRate / 10
        if (this.core._md_neteq_get_stats(this.receiver, this.stats) !== 0) throw new Error('Call audio stats failed')
        this.port.postMessage({ type: 'health', epoch: this.epoch,
          realOutputSamples: Number(this.statsView.getBigUint64(104, true)),
          lastRealRenderUs: Number(this.statsView.getBigUint64(112, true)),
          renderErrors: Number(this.statsView.getBigUint64(120, true)) })
      }
      const input = inputs[0]?.[0]
      for (let i = 0; i < output.length; i += 1) {
        this.core.HEAPF32[(this.input >> 2) + this.captureSize++] = input?.[i] ?? 0
        if (this.captureSize === 960) {
          if (this.pendingSends >= this.core._md_audio_send_capacity()) throw new Error('Call audio sender backpressure')
          const size = this.core._md_opus_encode_float(this.encoder, this.input, 960, this.encoded, 1275)
          if (size <= 0) throw new Error('Call audio encoding failed')
          const payload = this.core.HEAPU8.slice(this.encoded, this.encoded + size)
          this.port.postMessage({ type: 'encoded', epoch: this.epoch, sequence: this.sequence++, captureTime: currentTime + (i + 1) / sampleRate, payload }, [payload.buffer])
          this.pendingSends += 1
          this.captureSize = 0
        }
      }
    } catch (error) { this.fail(error) }
    return true
  }
}

registerProcessor('modemdeck-call-audio', CallAudioProcessor)
