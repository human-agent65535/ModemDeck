import type { NetworkLineStatus } from '../api/types'

export type TrafficLineState = {
  kind: 'connected' | 'idle' | 'error'
  label: string
}

const noConnectedDataBearer = 'line has no connected data bearer'

export function trafficLineState(
  runtime?: NetworkLineStatus
): TrafficLineState {
  if (!runtime) {
    return { kind: 'idle', label: '未启用' }
  }

  const error = runtime.error.trim()
  if (error && error !== noConnectedDataBearer) {
    return { kind: 'error', label: '状态异常' }
  }
  if (runtime.connected) {
    return { kind: 'connected', label: '已联网' }
  }
  return { kind: 'idle', label: '未连接' }
}
