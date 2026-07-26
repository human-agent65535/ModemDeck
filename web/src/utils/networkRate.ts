export type NetworkCounterSample = {
  bootEpoch: string
  observedAt: string
  interface: string
  connected: boolean
  rxBytes: number
  txBytes: number
}

export type NetworkRate = {
  rxBytesPerSecond: number
  txBytesPerSecond: number
}

export function calculateNetworkRate(
  previous: NetworkCounterSample | undefined,
  current: NetworkCounterSample
): NetworkRate | undefined {
  if (
    !previous ||
    !previous.connected ||
    !current.connected ||
    !current.interface ||
    previous.bootEpoch !== current.bootEpoch ||
    previous.interface !== current.interface ||
    current.rxBytes < previous.rxBytes ||
    current.txBytes < previous.txBytes
  ) {
    return undefined
  }

  const elapsedSeconds =
    (Date.parse(current.observedAt) - Date.parse(previous.observedAt)) / 1000
  if (!Number.isFinite(elapsedSeconds) || elapsedSeconds <= 0) return undefined

  return {
    rxBytesPerSecond: (current.rxBytes - previous.rxBytes) / elapsedSeconds,
    txBytesPerSecond: (current.txBytes - previous.txBytes) / elapsedSeconds
  }
}

export function formatNetworkRate(bytesPerSecond: number): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond < 0) return '—'
  const bitsPerSecond = bytesPerSecond * 8
  if (bitsPerSecond < 1000) return `${Math.round(bitsPerSecond)} bps`

  const units = ['Kbps', 'Mbps', 'Gbps', 'Tbps']
  let value = bitsPerSecond / 1000
  let unit = units[0]
  for (let index = 1; index < units.length && value >= 1000; index += 1) {
    value /= 1000
    unit = units[index]
  }
  const precision = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${unit}`
}
