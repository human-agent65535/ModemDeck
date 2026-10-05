declare const sampleRate: number
declare const currentTime: number
declare class AudioWorkletProcessor {
  readonly port: MessagePort
  constructor()
}
declare function registerProcessor(name: string, processor: typeof AudioWorkletProcessor): void

// One worklet owns capture framing and bounded playback, at the device's native
// sample rate. Web Audio handles AEC/AGC/NS on the getUserMedia capture stream.
class CallAudioProcessor extends AudioWorkletProcessor {
  private capture = new Float32Array(320)
  private captureSize = 0
  private captureIndex = 0
  private capturePhase = 0
  private previousInput = 0
  private playback = new Float32Array(1600)
  private playbackHead = 0
  private playbackSize = 0
  private playbackPhase = 0
  private playing = false

  constructor() {
    super()
    this.port.onmessage = ({ data }: MessageEvent<{ type: string; pcm?: Float32Array; sentAt?: number }>) => {
      if (data.type === 'clear') {
        this.playbackHead = 0
        this.playbackSize = 0
        this.playbackPhase = 0
        this.playing = false
      } else if (data.type === 'play' && data.pcm && currentTime - (data.sentAt ?? currentTime) <= 0.1) {
        // A fixed ring avoids allocation/copying on the realtime audio thread.
        // Latest audio wins after a stall, with at most 100 ms queued.
        for (const sample of data.pcm) {
          if (this.playbackSize === this.playback.length) {
            this.playbackHead = (this.playbackHead + 1) % this.playback.length
            this.playbackSize -= 1
            this.playbackPhase = 0
          }
          this.playback[(this.playbackHead + this.playbackSize) % this.playback.length] = sample
          this.playbackSize += 1
        }
      }
    }
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
          this.port.postMessage({ type: 'capture', pcm, index: this.captureIndex++, time: currentTime }, [pcm.buffer])
          this.capture = new Float32Array(320)
          this.captureSize = 0
        }
      }
      this.previousInput = value

      // A 40 ms prebuffer absorbs network/worker scheduling jitter. Underrun
      // emits silence and starts a new short prebuffer instead of growing delay.
      if (!this.playing && this.playbackSize >= 640) this.playing = true
      if (this.playing && this.playbackSize >= 2) {
        const first = this.playback[this.playbackHead] ?? 0
        const second = this.playback[(this.playbackHead + 1) % this.playback.length] ?? 0
        output[i] = first + (second - first) * this.playbackPhase
        this.playbackPhase += ratio
        if (this.playbackPhase >= 1) {
          const count = Math.floor(this.playbackPhase)
          this.playbackHead = (this.playbackHead + count) % this.playback.length
          this.playbackSize -= count
          this.playbackPhase -= count
        }
      } else {
        output[i] = 0
        this.playing = false
      }
    }
    return true
  }
}

registerProcessor('modemdeck-call-audio', CallAudioProcessor)
