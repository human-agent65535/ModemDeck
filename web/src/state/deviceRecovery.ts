export type DeviceRecoveryResult<T> =
  | { status: 'recovered'; value: T }
  | { status: 'timeout'; observedUnavailable: boolean }
  | { status: 'superseded' }

type DeviceRecoveryProbe<T> = {
  read: () => Promise<T>
  isCurrent: () => boolean
  isUnavailable: (error: unknown) => boolean
  timeoutMs: number
  intervalMs: number
  now?: () => number
  wait?: (milliseconds: number) => Promise<void>
}

export function isExpectedDeviceRecoveryOutage(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const candidate = error as { status?: unknown; code?: unknown }
  return (
    candidate.status === 404 ||
    candidate.status === 503 ||
    candidate.code === 'not_found' ||
    candidate.code === 'communications_unavailable'
  )
}

function defaultWait(milliseconds: number): Promise<void> {
  return new Promise(resolve => globalThis.setTimeout(resolve, milliseconds))
}

export async function waitForUnavailableThenReadable<T>(
  probe: DeviceRecoveryProbe<T>
): Promise<DeviceRecoveryResult<T>> {
  const now = probe.now || Date.now
  const wait = probe.wait || defaultWait
  const deadline = now() + probe.timeoutMs
  let sawUnavailable = false

  while (probe.isCurrent() && now() < deadline) {
    try {
      const value = await probe.read()
      if (sawUnavailable) return { status: 'recovered', value }
    } catch (error) {
      if (!probe.isUnavailable(error)) throw error
      sawUnavailable = true
    }

    if (!probe.isCurrent()) return { status: 'superseded' }
    const remaining = deadline - now()
    if (remaining <= 0) break
    await wait(Math.min(probe.intervalMs, remaining))
  }

  return probe.isCurrent()
    ? { status: 'timeout', observedUnavailable: sawUnavailable }
    : { status: 'superseded' }
}
