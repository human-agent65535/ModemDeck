import { reactive, watch } from 'vue'
import type { Router } from 'vue-router'
import { gateway } from '../api/client'
import type { CallAction, CallSession, ResourceStatus } from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import { showBrowserNotification } from './browserNotifications'
import { syncCallSounds } from './browserSounds'
import {
  capabilityReason,
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineLabel
} from './workspace'
import { closeDialer, showCallSurface } from './ui'
import { callMediaState, shutdownCallMedia, syncCallMedia } from './callMedia'
import { syncCallRecording } from './recording'

const TERMINAL_PHASES = new Set<CallSession['phase']>(['ended', 'failed'])
const LEASED_PHASES = new Set<CallSession['phase']>([
  'dialing',
  'ringing',
  'connecting',
  'active'
])
const LEASED_MEDIA_STATES = new Set([
  'requesting',
  'connecting',
  'active'
])
const NOTIFIED_CALL_HISTORY_LIMIT = 256

type PendingCallAction = '' | 'dial' | CallAction | 'dtmf'

let runtimeStarted = false
let activeCallRefreshRequested = false
let activeCallRefreshLoop: Promise<void> | undefined
let mutationEpoch = 0
let activeRouter: Router | undefined
let callLeaseRenewal: Promise<void> | undefined
let callLeaseRenewalCallID = ''
let callLeaseRenewalGeneration = 0
const notifiedIncomingCallIDs = new Set<string>()

export const callState = reactive<{
  session: CallSession | null
  owned: boolean
  dtmfDigits: string
  busy: boolean
  pendingAction: PendingCallAction
  error: string
  errorStatus: number
  syncStatus: ResourceStatus
  syncError: string
}>({
  session: null,
  owned: false,
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
  const owned = session.control_state === 'owned'
  const incomingAvailable =
    session.direction === 'incoming' &&
    session.phase === 'ringing' &&
    session.control_state === 'available'
  const claimedIncomingRinging =
    owned &&
    session.direction === 'incoming' &&
    session.phase === 'ringing'
  if (newCall) callState.dtmfDigits = ''
  callState.session = session
  callState.owned = owned
  if (newCall && (owned || incomingAvailable)) showCallSurface()
  syncCallSounds(
    (owned && !claimedIncomingRinging) || incomingAvailable ? session : null
  )
  syncCallMedia(owned ? session : null)
  syncCallRecording(owned || incomingAvailable ? session : null)
  if (owned) void renewActiveCallLease()
  showIncomingCallNotification(session)
}

function sessionCanRenewBrowserLease(session: CallSession): boolean {
  if (!LEASED_PHASES.has(session.phase)) return false
  if (session.phase !== 'active' || !session.media_available) return true
  return LEASED_MEDIA_STATES.has(callMediaState.status)
}

export function renewActiveCallLease(): Promise<void> {
  const session = callState.session
  if (
    !session ||
    !callState.owned ||
    !sessionCanRenewBrowserLease(session)
  ) {
    return Promise.resolve()
  }
  if (callLeaseRenewal && callLeaseRenewalCallID === session.id) {
    return callLeaseRenewal
  }

  const callID = session.id
  const generation = ++callLeaseRenewalGeneration
  const operation = gateway
    .renewCallLease(callID)
    .then(() => undefined)
    .catch(error => {
      if (
        callState.session?.id === callID &&
        error instanceof ApiError &&
        (error.status === 404 || error.status === 409)
      ) {
        void requestActiveCallRefresh()
      }
    })
    .finally(() => {
      if (callLeaseRenewalGeneration === generation) {
        callLeaseRenewal = undefined
        callLeaseRenewalCallID = ''
      }
    })
  callLeaseRenewalCallID = callID
  callLeaseRenewal = operation
  return operation
}

watch(
  () => callMediaState.status,
  status => {
    if (LEASED_MEDIA_STATES.has(status)) void renewActiveCallLease()
  }
)

export function claimIncomingCallNotification(
  session: CallSession,
  claimed: Set<string>
): boolean {
  if (
    session.direction !== 'incoming' ||
    session.phase !== 'ringing' ||
    session.control_state !== 'available' ||
    !session.id
  ) {
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
  const caller =
    session.display_name ||
    contact?.display_name ||
    displayPhoneNumber(session.remote_number, session.line_id)
  const line = lineForKey(session.line_id)
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

async function refreshActiveCallsOnce(): Promise<void> {
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
      callState.owned = false
      callState.dtmfDigits = ''
      syncCallSounds(null)
      syncCallMedia(null)
      syncCallRecording(null)
    }
    callState.syncStatus = 'ready'
    callState.syncError = ''
  } catch (error) {
    if (!runtimeStarted || startedAtEpoch !== mutationEpoch) return
    const failure = requestError(error, translate('runtime.syncCallsFailed'))
    callState.syncStatus = failure.status === 403 ? 'forbidden' : 'error'
    callState.syncError = failure.message
  }
}

async function drainActiveCallRefreshes(): Promise<void> {
  while (runtimeStarted && activeCallRefreshRequested) {
    activeCallRefreshRequested = false
    await refreshActiveCallsOnce()
  }
}

export function refreshActiveCalls(): Promise<void> {
  if (!runtimeStarted) return Promise.resolve()
  activeCallRefreshRequested = true
  if (!activeCallRefreshLoop) {
    const loop = drainActiveCallRefreshes()
    activeCallRefreshLoop = loop.finally(() => {
      activeCallRefreshLoop = undefined
      if (runtimeStarted && activeCallRefreshRequested) void refreshActiveCalls()
    })
  }
  return activeCallRefreshLoop
}

export function initializeCallRuntime(router?: Router): void {
  if (runtimeStarted) return
  runtimeStarted = true
  activeRouter = router
  callState.syncStatus = 'idle'
  callState.syncError = ''
  void refreshActiveCalls()
}

export function requestActiveCallRefresh(): Promise<void> {
  return refreshActiveCalls()
}

export function shutdownCallRuntime(): void {
  runtimeStarted = false
  activeCallRefreshRequested = false
  activeRouter = undefined
  mutationEpoch += 1
  callLeaseRenewalGeneration += 1
  callLeaseRenewal = undefined
  callLeaseRenewalCallID = ''
  syncCallSounds(null)
  shutdownCallMedia()
  syncCallRecording(null)
  callState.session = null
  callState.owned = false
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
    const session = await gateway.startCall(lineKey, number, recordingEnabled)
    acceptSession(session)
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
  if (action === 'hangup' && !callState.owned) {
    callState.error = translate('runtime.callOwnedElsewhere')
    callState.errorStatus = 409
    return
  }
  const previousSession = callState.session

  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = action
  callState.error = ''
  callState.errorStatus = 0
  syncCallSounds(null)
  try {
    await gateway.callAction(id, action)
    if (action === 'answer' && previousSession) {
      acceptSession({
        ...previousSession,
        control_state: 'owned'
      })
    }
    await requestActiveCallRefresh()
  } catch (error) {
    const failure = requestError(error, translate('runtime.callActionFailed'))
    callState.error = failure.message
    callState.errorStatus = failure.status
    if (action === 'answer' || action === 'reject') {
      await requestActiveCallRefresh()
    } else {
      syncCallSounds(previousSession)
    }
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
  if (
    !id ||
    !callState.owned ||
    callState.session?.phase !== 'active' ||
    callState.busy
  ) {
    return
  }

  callState.dtmfDigits += digit
  mutationEpoch += 1
  callState.busy = true
  callState.pendingAction = 'dtmf'
  callState.error = ''
  callState.errorStatus = 0
  try {
    await gateway.sendDTMF(id, digit)
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
  callState.owned = false
  callState.dtmfDigits = ''
  syncCallSounds(null)
  syncCallMedia(null)
  syncCallRecording(null)
  callState.error = ''
  callState.errorStatus = 0
}
