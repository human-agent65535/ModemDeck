import { instantiateAudioCore, type AudioCoreExports } from './audioCore'

declare const sampleRate: number
declare const currentTime: number
declare class AudioWorkletProcessor {
  readonly port: MessagePort
  constructor()
}
declare function registerProcessor(name: string, processor: new (options: AudioWorkletNodeOptions) => AudioWorkletProcessor): void

// One worklet owns capture framing and bounded playback, at the device's native
// sample rate. Web Audio handles AEC/AGC/NS on the getUserMedia capture stream.
class CallAudioProcessor extends AudioWorkletProcessor {
  private capture = new Float32Array(320)
  private captureSize = 0
  private captureIndex = 0
  private capturePhase = 0
  private previousInput = 0
  // Five transport frames plus the two-frame prebuffer. Source deadlines,
  // rather than time spent in the worker/FIFO, bound stale playback.
  private playback: { pcm: Float32Array; playAt: number; sequence: number; generation: number; sourceSamples: number }[] = []
  private generation = 0
  private lastSequence: number | undefined
  private starving = false
  private epochStartedAt: number | undefined
  private epochSourceSamples = 0
  private readonly core: AudioCoreExports

  constructor(options: AudioWorkletNodeOptions) {
    super()
    this.core = instantiateAudioCore(options.processorOptions.coreModule as WebAssembly.Module)
    this.port.onmessage = ({ data }: MessageEvent<{ type: string; pcm?: Float32Array; playAt: number; sequence: number; generation?: number; sourceSamples: number }>) => {
      if (data.type === 'clear') {
        this.clearPlayback(data.generation ?? 0)
      } else if (data.type === 'play' && data.pcm?.length === this.core.md_audio_frame_samples() &&
          Number.isFinite(data.playAt) && !this.core.md_audio_frame_expired(data.playAt, currentTime)) {
        const generation = data.generation ?? 0
        if (generation < this.generation) return
        if (generation !== this.generation) this.clearPlayback(generation)
        if (this.playback.length === this.core.md_audio_queue_capacity()) {
          this.playback.shift()
        }
        this.playback.push({ pcm: data.pcm, playAt: data.playAt, sequence: data.sequence, generation, sourceSamples: data.sourceSamples })
      }
    }
  }

  private clearPlayback(generation: number): void {
    this.playback.length = 0
    this.generation = generation
    this.lastSequence = undefined
    this.starving = false
    this.epochStartedAt = undefined
    this.epochSourceSamples = 0
  }

  private render(now: number): number {
    const deviceSample = Math.round(now * sampleRate)
    while (this.playback.length) {
      const packet = this.playback[0]!
      // Rebuffering and delayed processing cannot renew source deadlines.
      const prefill = this.starving || this.epochStartedAt === undefined ? this.core.md_audio_prebuffer_seconds() : 0
      if (this.core.md_audio_frame_expired(packet.playAt, now + prefill)) {
        this.playback.shift()
        continue
      }
      if (this.starving || this.epochStartedAt === undefined) {
        // A fixed device epoch, shared with native playback. Slow receive-clock
        // drift never moves individual packets within an established epoch.
        this.epochStartedAt = this.core.md_audio_playback_start(packet.playAt, now)
        this.epochSourceSamples = packet.sourceSamples
        this.starving = false
      }
      const relativeSamples = packet.sourceSamples - this.epochSourceSamples
      const start = this.core.md_audio_source_slot(this.epochStartedAt, relativeSamples)
      const end = this.core.md_audio_source_slot(this.epochStartedAt, relativeSamples + this.core.md_audio_frame_samples())
      const firstDeviceSample = Math.round(start * sampleRate)
      if (deviceSample < firstDeviceSample) return 0
      if (deviceSample >= Math.round(end * sampleRate)) {
        // A rendering stall skips elapsed source samples, including a partly
        // rendered frame. It cannot push later packets out of their own slots.
        this.playback.shift()
        this.lastSequence = packet.sequence
        continue
      }
      const phase = (deviceSample - firstDeviceSample) * 16000 / sampleRate
      const index = Math.floor(phase)
      const fraction = phase - index
      const first = packet.pcm[index] ?? 0
      const second = packet.pcm[Math.min(index + 1, 319)] ?? first
      return first + (second - first) * fraction
    }
    if (this.lastSequence !== undefined) this.starving = true
    return 0
  }

  process(inputs: Float32Array[][], outputs: Float32Array[][]): boolean {
    const output = outputs[0]?.[0]
    if (!output) return true
    const input = inputs[0]?.[0]
    const ratio = 16000 / sampleRate
    for (let i = 0; i < output.length; i += 1) {
      const value = input?.[i] ?? 0
      this.capturePhase += ratio
      while (this.capturePhase >= 1) {
        this.capturePhase -= 1
        const fraction = 1 - this.capturePhase / ratio
        this.capture[this.captureSize++] = this.previousInput + (value - this.previousInput) * fraction
        if (this.captureSize === 320) {
          const pcm = this.capture
          this.port.postMessage({ type: 'capture', pcm, index: this.captureIndex++, time: currentTime + (i + 1) / sampleRate }, [pcm.buffer])
          this.capture = new Float32Array(320)
          this.captureSize = 0
        }
      }
      this.previousInput = value

      output[i] = this.render(currentTime + i / sampleRate)
    }
    return true
  }
}

registerProcessor('modemdeck-call-audio', CallAudioProcessor)
