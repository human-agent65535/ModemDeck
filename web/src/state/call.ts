import { reactive } from 'vue'
import { gateway } from '../api/client'
import type { CallAction, CallSession, ResourceStatus } from '../api/types'
import { ApiError } from '../api/types'
import { capabilityReason } from './workspace'
import { closeDialer } from './ui'
import { shutdownCallMedia, syncCallMedia } from './callMedia'

const TERMINAL_PHASES = new Set<CallSession['phase']>(['ended', 'failed'])
const ACTIVE_POLL_MS = 2000
const IDLE_POLL_MS = 5000
const ERROR_POLL_MS = 10000

type PendingCallAction = '' | 'dial' | CallAction | 'dtmf'

let pollTimer: number | undefined
let runtimeStarted = false
let pollInFlight = false
let mutationEpoch = 0

export const callState = reactive<{
  session: CallSession | null
  busy: boolean
  pendingAction: PendingCallAction
  error: string
  errorStatus: number
  syncStatus: ResourceStatus
  syncError: string
}>({
  session: null,
  busy: false,
  pendingAction: '',
  error: '',
  errorStatus: 0,
  syncStatus: 'idle',
  syncError: ''
})

function stopPolling(): void {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = undefined
}

function schedulePoll(delay: number): void {
  stopPolling()
  if (!runtimeStarted) return
  pollTimer = window.setTimeout(() => {
    pollTimer = undefined
    void pollActiveCalls()
  }, delay)
}

function requestError(error: unknown, fallback: string): { message: string; status: number } {
  return {
    message: error instanceof Error ? error.message : fallback,
    status: error instanceof ApiError ? error.status : 0
  }
}

function acceptSession(session: CallSession): void {
  callState.session = session
  syncCallMedia(session)
}

async function pollActiveCalls(): Promise<void> {
  if (!runtimeStarted) return
  if (pollInFlight) {
    schedulePoll(ACTIVE_POLL_MS)
    return
  }
  pollInFlight = true
  const startedAtEpoch = mutationEpoch
  if (callState.syncStatus === 'idle') callState.syncStatus = 'loading'

  try {
    const calls = await gateway.getActiveCalls()
    if (!runtimeStarted || startedAtEpoch !== mutationEpoch) return

    const active = calls[0]
    if (active) {
      acceptSession(active)
    } else if (callState.session && !TERMINAL_PHASES.has(callState.session.phase)) {
      callState.session = null
      syncCallMedia(null)
    }
    callState.syncStatus = 'ready'
    callState.syncError = ''
    schedulePoll(active ? ACTIVE_POLL_MS : IDLE_POLL_MS)
  } catch (error) {
    if (!runtimeStarted || startedAtEpoch !== mutationEpoch) return
    const failure = requestError(error, '无法同步活动通话')
    callState.syncStatus = failure.status === 403 ? 'forbidden' : 'error'
    callState.syncError = failure.message
    if (failure.status === 403) {
      stopPolling()
    } else {
      schedulePoll(ERROR_POLL_MS)
    }
  } finally {
    pollInFlight = false
  }
}

export function initializeCallRuntime(): void {
  if (runtimeStarted) return
  runtimeStarted = true
  callState.syncStatus = 'idle'
  callState.syncError = ''
  schedulePoll(0)
}

export function shutdownCallRuntime(): void {
  runtimeStarted = false
  mutationEpoch += 1
  stopPolling()
  shutdownCallMedia()
  callState.session = null
  callState.busy = false
  callState.pendingAction = ''
  callState.error = ''
  callState.errorStatus = 0
  callState.syncStatus = 'idle'
  callState.syncError = ''
}

export async function dial(
  number: string,
  lineKey: string,
  recordingEnabled?: boolean
): Promise<boolean> {
  const unavailable = capabilityReason('dial')
  if (unavailable) {
    callState.error = unavailable
    callState.errorStatus = 0
    return false
  }
  if (callState.session && !TERMINAL_PHASES.has(callState.session.phase)) {
    callState.error = '已有通话正在进行'
    callState.errorStatus = 409
    return false
  }
  if (callState.busy) {
    callState.error = '已有拨号请求正在准备'
    callState.errorStatus = 409
    return false
  }

  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = 'dial'
  callState.error = ''
  callState.errorStatus = 0
  try {
    acceptSession(await gateway.startCall(lineKey, number, recordingEnabled))
    closeDialer()
    schedulePoll(ACTIVE_POLL_MS)
    return true
  } catch (error) {
    const failure = requestError(error, '无法发起通话')
    callState.error = failure.message
    callState.errorStatus = failure.status
    return false
  } finally {
    callState.busy = false
    callState.pendingAction = ''
  }
}

async function act(action: CallAction): Promise<void> {
  const id = callState.session?.id
  if (!id || callState.busy) return

  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = action
  callState.error = ''
  callState.errorStatus = 0
  try {
    acceptSession(await gateway.callAction(id, action))
    schedulePoll(ACTIVE_POLL_MS)
  } catch (error) {
    const failure = requestError(error, '通话操作失败')
    callState.error = failure.message
    callState.errorStatus = failure.status
  } finally {
    callState.busy = false
    callState.pendingAction = ''
  }
}

export function answerCall(): Promise<void> {
  return act('answer')
}

export function rejectCall(): Promise<void> {
  return act('reject')
}

export function hangupCall(): Promise<void> {
  return act('hangup')
}

export async function sendDTMF(digit: string): Promise<void> {
  const id = callState.session?.id
  if (!id || callState.session?.phase !== 'active' || callState.busy) return

  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = 'dtmf'
  callState.error = ''
  callState.errorStatus = 0
  try {
    acceptSession(await gateway.sendDTMF(id, digit))
    schedulePoll(ACTIVE_POLL_MS)
  } catch (error) {
    const failure = requestError(error, '无法发送按键音')
    callState.error = failure.message
    callState.errorStatus = failure.status
  } finally {
    callState.busy = false
    callState.pendingAction = ''
  }
}

export function dismissCall(): void {
  if (callState.session && !TERMINAL_PHASES.has(callState.session.phase)) return
  callState.session = null
  syncCallMedia(null)
  callState.error = ''
  callState.errorStatus = 0
}
