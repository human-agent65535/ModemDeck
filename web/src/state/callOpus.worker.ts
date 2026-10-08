import { Application, createDecoder, createEncoder, type OpusDecoderHandle, type OpusEncoderHandle } from 'libopus-wasm'

type CodecRequest = { type: 'encode' | 'decode'; pcm: Float32Array; payload: Uint8Array; sequence: number; time: number; generation: number; playAt: number; sourceSamples: number; contextEpoch: number }
const scope = globalThis as unknown as {
  onmessage: ((event: MessageEvent<CodecRequest>) => void) | null
  postMessage(value: unknown, transfer?: Transferable[]): void
}
let encoder: OpusEncoderHandle | undefined
let decoder: OpusDecoderHandle | undefined
let decodedGeneration: number | undefined
let decodedSequence: number | undefined
let decoding = Promise.resolve()

async function initialize(): Promise<void> {
  try {
    encoder = await createEncoder({ sampleRate: 16000, channels: 1, frameSize: 320, application: Application.Voip, bitrate: 24000, complexity: 5, dtx: false })
    decoder = await createDecoder({ sampleRate: 16000, channels: 1, maxFrameSize: 320 })
    scope.onmessage = ({ data }) => {
      try {
        if (data.type === 'encode') {
          const payload = encoder!.encodeFloat(data.pcm, { maxPacketBytes: 1275 })
          scope.postMessage({ type: 'encoded', payload, sequence: data.sequence, time: data.time, contextEpoch: data.contextEpoch }, [payload.buffer as ArrayBuffer])
        } else {
          // Main-thread admission bounds this serial queue. Reset predictive
          // state across re-anchors and omitted/dropped capture packets.
          decoding = decoding.then(async () => {
            if (decodedGeneration !== undefined && (data.generation !== decodedGeneration ||
                data.sequence !== ((decodedSequence! + 1) >>> 0))) {
              decoder!.free()
              decoder = await createDecoder({ sampleRate: 16000, channels: 1, maxFrameSize: 320 })
            }
            if ((data.payload[0] ?? 0) & 4) throw new Error('Expected mono Opus audio')
            const pcm = decoder!.decodeFloat(data.payload, { maxFrameSize: 320 })
            if (pcm.length !== 320) throw new Error('Expected one 20 ms Opus frame')
            decodedGeneration = data.generation
            decodedSequence = data.sequence
            scope.postMessage({ type: 'decoded', pcm, sequence: data.sequence,
              generation: data.generation, sourceSamples: data.sourceSamples, playAt: data.playAt, contextEpoch: data.contextEpoch }, [pcm.buffer as ArrayBuffer])
          }).catch(error => {
            scope.postMessage({ type: 'error', requestType: data.type, contextEpoch: data.contextEpoch, message: error instanceof Error ? error.message : 'Opus codec failed' })
          })
        }
      } catch (error) {
        scope.postMessage({ type: 'error', requestType: data.type, contextEpoch: data.contextEpoch, message: error instanceof Error ? error.message : 'Opus codec failed' })
      }
    }
    scope.postMessage({ type: 'ready' })
  } catch (error) {
    encoder?.free()
    decoder?.free()
    scope.postMessage({ type: 'error', message: error instanceof Error ? error.message : 'Opus codec unavailable' })
  }
}

void initialize()
