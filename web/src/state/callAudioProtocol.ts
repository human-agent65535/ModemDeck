export const CALL_AUDIO_FORMAT = {
  version: 1, codec: 'opus', sample_rate: 16000, channels: 1, frame_ms: 20
} as const
export const CALL_AUDIO_FRAME_SAMPLES = 320
export const CALL_AUDIO_MAX_PACKET_BYTES = 1275

export function callAudioWebSocketURL(callID: string, location: Pick<Location, 'href'>): string {
  const id = callID.trim()
  if (!id) throw new Error('Missing call ID')
  const url = new URL(`/api/v1/calls/${encodeURIComponent(id)}/media/ws`, location.href)
  // Development on localhost uses HTTP; deployed HTTPS always uses WSS.
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url.href
}

export function encodeCallAudioPacket(sequence: number, payload: Uint8Array): ArrayBuffer {
  if (!payload.length || payload.length > CALL_AUDIO_MAX_PACKET_BYTES) throw new Error('Invalid Opus packet size')
  const packet = new Uint8Array(12 + payload.length)
  packet.set([0x4d, 0x44, 1, 0])
  const view = new DataView(packet.buffer)
  view.setUint32(4, sequence >>> 0)
  view.setUint32(8, (sequence * CALL_AUDIO_FRAME_SAMPLES) >>> 0)
  packet.set(payload, 12)
  return packet.buffer
}

export function decodeCallAudioPacket(buffer: ArrayBuffer): { sequence: number; timestamp: number; payload: Uint8Array } {
  const bytes = new Uint8Array(buffer)
  if (bytes.length < 13 || bytes.length > 12 + CALL_AUDIO_MAX_PACKET_BYTES ||
      bytes[0] !== 0x4d || bytes[1] !== 0x44 || bytes[2] !== 1 || bytes[3] !== 0) {
    throw new Error('Invalid call audio frame')
  }
  const view = new DataView(buffer)
  return { sequence: view.getUint32(4), timestamp: view.getUint32(8), payload: bytes.slice(12) }
}

// MD12 transport integrity only: NetEq owns arrival/age/delay decisions.
export function validateCallAudioProgress(previous: { sequence: number; timestamp: number } | undefined, packet: { sequence: number; timestamp: number }): void {
  if (!previous) return
  const distance = (packet.sequence - previous.sequence) >>> 0
  if (!distance || distance >= 0x80000000 ||
      ((packet.timestamp - previous.timestamp) >>> 0) !== ((distance * CALL_AUDIO_FRAME_SAMPLES) >>> 0)) {
    throw new Error('Invalid call audio sequence')
  }
}

export function isCallAudioReady(value: Record<string, unknown>): boolean {
  return value.type === 'ready' && Object.entries(CALL_AUDIO_FORMAT).every(([key, expected]) => value[key] === expected)
}

export function isRecoverableCallAudioError(code: unknown): boolean {
  return code === 'transport_timeout' || code === 'transport_closed' || code === 'backpressure'
}
