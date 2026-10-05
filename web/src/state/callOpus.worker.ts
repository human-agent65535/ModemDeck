import { Application, createDecoder, createEncoder, type OpusDecoderHandle, type OpusEncoderHandle } from 'libopus-wasm'

type CodecRequest = { type: 'encode' | 'decode'; pcm: Float32Array; payload: Uint8Array; sequence: number; time: number }
const scope = globalThis as unknown as {
  onmessage: ((event: MessageEvent<CodecRequest>) => void) | null
  postMessage(value: unknown, transfer?: Transferable[]): void
}
let encoder: OpusEncoderHandle | undefined
let decoder: OpusDecoderHandle | undefined

async function initialize(): Promise<void> {
  try {
    encoder = await createEncoder({ sampleRate: 16000, channels: 1, frameSize: 320, application: Application.Voip, bitrate: 24000, complexity: 5, dtx: false })
    decoder = await createDecoder({ sampleRate: 16000, channels: 1, maxFrameSize: 320 })
    scope.onmessage = ({ data }) => {
      try {
        if (data.type === 'encode') {
          const payload = encoder!.encodeFloat(data.pcm, { maxPacketBytes: 1275 })
          scope.postMessage({ type: 'encoded', payload, sequence: data.sequence, time: data.time }, [payload.buffer as ArrayBuffer])
        } else {
          if ((data.payload[0] ?? 0) & 4) throw new Error('Expected mono Opus audio')
          const pcm = decoder!.decodeFloat(data.payload, { maxFrameSize: 320 })
          if (pcm.length !== 320) throw new Error('Expected one 20 ms Opus frame')
          scope.postMessage({ type: 'decoded', pcm, time: data.time }, [pcm.buffer as ArrayBuffer])
        }
      } catch (error) {
        scope.postMessage({ type: 'error', message: error instanceof Error ? error.message : 'Opus codec failed' })
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
