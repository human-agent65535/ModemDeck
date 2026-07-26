import { reactive } from 'vue'
import type { Router } from 'vue-router'
import { gateway } from '../api/client'
import type { CallAction, CallSession, ResourceStatus } from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import { showBrowserNotification } from './browserNotifications'
import { syncCallSounds } from './browserSounds'
import { capabilityReason, contactForNumber, lineForKey, lineLabel } from './workspace'
import { closeDialer, showCallSurface } from './ui'
import { shutdownCallMedia, syncCallMedia } from './callMedia'

const TERMINAL_PHASES = new Set<CallSession['phase']>(['ended', 'failed'])
const NOTIFIED_CALL_HISTORY_LIMIT = 256

type PendingCallAction = '' | 'dial' | CallAction | 'dtmf'

let runtimeStarted = false
let pollInFlight = false
let mutationEpoch = 0
let activeRouter: Router | undefined
const notifiedIncomingCallIDs = new Set<string>()

export const callState = reactive<{
  session: CallSession | null
  dtmfDigits: string
  busy: boolean
  pendingAction: PendingCallAction
  error: string
  errorStatus: number
  syncStatus: ResourceStatus
  syncError: string
}>({
  session: null,
  dtmfDigits: '',
  busy: false,
  pendingAction: '',
  error: '',
  errorStatus: 0,
  syncStatus: 'idle',
  syncError: ''
})

function requestError(error: unknown, fallback: string): { message: string; status: number } {
  return {
    message: error instanceof Error ? error.message : fallback,
    status: error instanceof ApiError ? error.status : 0
  }
}

function acceptSession(session: CallSession): void {
  const newCall = callState.session?.id !== session.id
  if (newCall) callState.dtmfDigits = ''
  callState.session = session
  if (newCall) showCallSurface()
  syncCallSounds(session)
  syncCallMedia(session)
  showIncomingCallNotification(session)
}

export function claimIncomingCallNotification(
  session: CallSession,
  claimed: Set<string>
): boolean {
  if (session.direction !== 'incoming' || session.phase !== 'ringing' || !session.id) {
    return false
  }
  if (claimed.has(session.id)) return false
  claimed.add(session.id)
  while (claimed.size > NOTIFIED_CALL_HISTORY_LIMIT) {
    const oldest = claimed.values().next().value
    if (!oldest) break
    claimed.delete(oldest)
  }
  return true
}

export function incomingCallRoute(): { name: 'calls' } {
  return { name: 'calls' }
}

function showIncomingCallNotification(session: CallSession): void {
  if (!activeRouter || !claimIncomingCallNotification(session, notifiedIncomingCallIDs)) return

  const contact = contactForNumber(session.remote_number)
  const caller = session.display_name || contact?.display_name || session.remote_number
  const line = lineForKey(session.line_key)
  showBrowserNotification({
    title: caller,
    body: line
      ? translate('runtime.incomingCallOnLine', { line: lineLabel(line) })
      : translate('runtime.incomingCall'),
    tag: `modemdeck-call-${session.id}`,
    onClick: () => {
      window.focus()
      void activeRouter?.push(incomingCallRoute())
    }
  })
}

export async function refreshActiveCalls(): Promise<void> {
  if (!runtimeStarted) return
  if (pollInFlight) return
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
      callState.dtmfDigits = ''
      syncCallSounds(null)
      syncCallMedia(null)
    }
    callState.syncStatus = 'ready'
    callState.syncError = ''
  } catch (error) {
    if (!runtimeStarted || startedAtEpoch !== mutationEpoch) return
    const failure = requestError(error, translate('runtime.syncCallsFailed'))
    callState.syncStatus = failure.status === 403 ? 'forbidden' : 'error'
    callState.syncError = failure.message
  } finally {
    pollInFlight = false
  }
}

export function initializeCallRuntime(router?: Router): void {
  if (runtimeStarted) return
  runtimeStarted = true
  activeRouter = router
  callState.syncStatus = 'idle'
  callState.syncError = ''
  void refreshActiveCalls()
}

export function requestActiveCallRefresh(): void {
  if (!runtimeStarted) return
  void refreshActiveCalls()
}

export function shutdownCallRuntime(): void {
  runtimeStarted = false
  activeRouter = undefined
  mutationEpoch += 1
  syncCallSounds(null)
  shutdownCallMedia()
  callState.session = null
  callState.dtmfDigits = ''
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
    callState.error = translate('runtime.callInProgress')
    callState.errorStatus = 409
    return false
  }
  if (callState.busy) {
    callState.error = translate('runtime.dialPreparing')
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
    return true
  } catch (error) {
    const failure = requestError(error, translate('runtime.dialFailed'))
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
  const previousSession = callState.session

  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = action
  callState.error = ''
  callState.errorStatus = 0
  syncCallSounds(null)
  try {
    acceptSession(await gateway.callAction(id, action))
  } catch (error) {
    const failure = requestError(error, translate('runtime.callActionFailed'))
    callState.error = failure.message
    callState.errorStatus = failure.status
    syncCallSounds(previousSession)
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

  callState.dtmfDigits += digit
  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = 'dtmf'
  callState.error = ''
  callState.errorStatus = 0
  try {
    acceptSession(await gateway.sendDTMF(id, digit))
  } catch (error) {
    const failure = requestError(error, translate('runtime.dtmfFailed'))
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
  callState.dtmfDigits = ''
  syncCallSounds(null)
  syncCallMedia(null)
  callState.error = ''
  callState.errorStatus = 0
}
