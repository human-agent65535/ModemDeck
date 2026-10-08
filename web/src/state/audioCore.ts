// The shared C core owns clock, freshness and rebuffer decisions. Platform
// adapters only translate monotonic clock domains and supply native PCM.
export type AudioCoreExports = {
  md_clock_reset(): void
  md_clock_accept(sequence: number, timestamp: number, now: number): number
  md_clock_play_at(): number
  md_clock_age(): number
  md_clock_source_samples(): number
  md_clock_generation(): number
  md_audio_frame_expired(source: number, now: number): number
  md_audio_playback_start(source: number, now: number): number
  md_audio_source_slot(epochStart: number, sourceSamples: number): number
  md_audio_frame_seconds(): number
  md_audio_prebuffer_seconds(): number
  md_audio_max_age_seconds(): number
  md_audio_send_queue_capacity(): number
  md_audio_queue_capacity(): number
  md_audio_frame_samples(): number
}

export function instantiateAudioCore(module: WebAssembly.Module): AudioCoreExports {
  return new WebAssembly.Instance(module).exports as unknown as AudioCoreExports
}

let compiled: Promise<WebAssembly.Module> | undefined
export function loadAudioCore(): Promise<WebAssembly.Module> {
  // Fetch only after the user enables call audio. The same compiled module is
  // reused, but each receive clock/worklet has its own isolated instance.
  compiled ??= import('./audioCore.wasm?url&no-inline').then(async ({ default: url }) => {
    const response = await fetch(url)
    if (!response.ok) throw new Error('Call audio core unavailable')
    return WebAssembly.compile(await response.arrayBuffer())
  }).catch(error => {
    compiled = undefined
    throw error
  })
  return compiled
}
