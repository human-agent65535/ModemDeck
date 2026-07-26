import type { NetworkLineStatus } from '../api/types'

export type TrafficLineState = {
  kind: 'connected' | 'idle' | 'error'
  labelKey:
    | 'traffic.lineDisabled'
    | 'traffic.lineError'
    | 'traffic.lineConnected'
    | 'traffic.lineDisconnected'
}

const noConnectedDataBearer = 'line has no connected data bearer'

export function trafficLineState(
  runtime?: NetworkLineStatus
): TrafficLineState {
  if (!runtime) {
    return { kind: 'idle', labelKey: 'traffic.lineDisabled' }
  }

  const error = runtime.error.trim()
  if (error && error !== noConnectedDataBearer) {
    return { kind: 'error', labelKey: 'traffic.lineError' }
  }
  if (runtime.connected) {
    return { kind: 'connected', labelKey: 'traffic.lineConnected' }
  }
  return { kind: 'idle', labelKey: 'traffic.lineDisconnected' }
}
