// Load only after call audio is enabled. The worklet owns the sole NetEq and
// Opus instance; initialization never runs inside its rendering callback.
export type AudioCoreModule = {
  HEAPU8: Uint8Array
  HEAPF32: Float32Array
  _malloc(size: number): number
  _free(pointer: number): void
  _md_audio_ingress_capacity(): number
  _md_audio_send_capacity(): number
  _md_neteq_create(rate: number): number
  _md_neteq_enqueue(handle: number, payload: number, size: number, sequence: number, timestamp48k: number, arrivalUs: bigint): number
  _md_neteq_render_float(handle: number, nowUs: bigint, output: number, frames: number): number
  _md_neteq_get_stats(handle: number, stats: number): number
  _md_neteq_destroy(handle: number): void
  _md_opus_encoder_create(rate: number, channels: number): number
  _md_opus_encode_float(handle: number, pcm: number, frames: number, output: number, capacity: number): number
  _md_opus_encoder_destroy(handle: number): void
}

let loaded: Promise<ArrayBuffer> | undefined
export function loadAudioCore(): Promise<ArrayBuffer> {
  loaded ??= import('./audioCore.wasm?url&no-inline').then(async ({ default: url }) => {
    const response = await fetch(url)
    if (!response.ok) throw new Error('Call audio core unavailable')
    return response.arrayBuffer()
  }).catch(error => {
    loaded = undefined
    throw error
  })
  return loaded
}
