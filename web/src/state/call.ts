import { reactive } from 'vue'
import { gateway } from '../api/client'
import type { CallSession } from '../api/types'
import { capabilityReason } from './workspace'
import { closeDialer } from './ui'

const TERMINAL_PHASES = new Set<CallSession['phase']>(['ended', 'failed'])
let pollTimer: number | undefined

export const callState = reactive<{
  session: CallSession | null
  busy: boolean
  error: string
}>({
  session: null,
  busy: false,
  error: ''
})

function stopPolling(): void {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = undefined
}

function schedulePoll(): void {
  stopPolling()
  if (!gateway.getCall || !callState.session || TERMINAL_PHASES.has(callState.session.phase)) return
  pollTimer = window.setTimeout(() => {
    void pollSession()
  }, 2000)
}

async function pollSession(): Promise<void> {
  const id = callState.session?.id
  if (!id || !gateway.getCall) return
  try {
    callState.session = await gateway.getCall(id)
    callState.error = ''
    schedulePoll()
  } catch (error) {
    callState.error = error instanceof Error ? error.message : '无法更新通话状态'
    stopPolling()
  }
}

export function initializeCallRuntime(): void {
  // The read-only API does not expose active calls. Fixture calls start from the dialer.
}

export async function dial(number: string, lineKey: string): Promise<boolean> {
  const unavailable = capabilityReason('dial')
  if (unavailable || !gateway.startCall) {
    callState.error = unavailable || '当前版本不支持拨号'
    return false
  }
  if (callState.session && !TERMINAL_PHASES.has(callState.session.phase)) {
    callState.error = '已有通话正在进行'
    return false
  }
  callState.busy = true
  callState.error = ''
  try {
    callState.session = await gateway.startCall(lineKey, number)
    closeDialer()
    schedulePoll()
    return true
  } catch (error) {
    callState.error = error instanceof Error ? error.message : '无法发起通话'
    return false
  } finally {
    callState.busy = false
  }
}

async function act(action: 'answer' | 'reject' | 'hangup'): Promise<void> {
  const id = callState.session?.id
  if (!id || callState.busy || !gateway.callAction) return
  callState.busy = true
  callState.error = ''
  try {
    callState.session = await gateway.callAction(id, action)
    schedulePoll()
  } catch (error) {
    callState.error = error instanceof Error ? error.message : '通话操作失败'
  } finally {
    callState.busy = false
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

export function dismissCall(): void {
  if (callState.session && !TERMINAL_PHASES.has(callState.session.phase)) return
  stopPolling()
  callState.session = null
  callState.error = ''
}
