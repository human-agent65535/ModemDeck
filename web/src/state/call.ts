import { reactive, watch } from 'vue'
import type { Router } from 'vue-router'
import { gateway } from '../api/client'
import type {
  ActiveCallSnapshot,
  CallAction,
  CallSession,
  OutgoingCallReservation,
  ResourceStatus
} from '../api/types'
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
import {
  callMediaState,
  retryCallMedia,
  shutdownCallMedia,
  syncCallMedia
} from './callMedia'
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
  'recovering',
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
  sessions: CallSession[]
  reservations: OutgoingCallReservation[]
  selectedCallID: string
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
  sessions: [],
  reservations: [],
  selectedCallID: '',
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

export function isLiveCallSession(session: CallSession): boolean {
  return !TERMINAL_PHASES.has(session.phase)
}

function foregroundPriority(session: CallSession): number {
  if (session.control_state === 'owned') return 0
  if (
    session.control_state === 'available' &&
    session.direction === 'incoming' &&
    session.phase === 'ringing'
  ) {
    return 1
  }
  if (session.control_state === 'occupied') return 2
  return 3
}

export function selectForegroundSession(
  sessions: CallSession[],
  selectedCallID = ''
): CallSession | null {
  const liveSessions = sessions.filter(isLiveCallSession)
  if (liveSessions.length === 0) return null

  const priority = Math.min(...liveSessions.map(foregroundPriority))
  const selected = liveSessions.find(session => session.id === selectedCallID)
  if (selected && foregroundPriority(selected) === priority) return selected
  return liveSessions.find(session => foregroundPriority(session) === priority) || null
}

export function occupiedLineIDs(
  sessions: readonly CallSession[] = callState.sessions,
  reservations: readonly OutgoingCallReservation[] = callState.reservations
): Set<string> {
  const result = new Set<string>()
  for (const session of sessions) {
    const lineID = session.line_id.trim()
    if (isLiveCallSession(session) && lineID) result.add(lineID)
  }
  for (const reservation of reservations) {
    const lineID = reservation.line_id.trim()
    if (lineID) result.add(lineID)
  }
  return result
}

export function lineHasActiveCall(
  lineID: string,
  sessions: readonly CallSession[] = callState.sessions
): boolean {
  const normalizedLineID = lineID.trim()
  return Boolean(
    normalizedLineID &&
      sessions.some(
        session =>
          isLiveCallSession(session) && session.line_id.trim() === normalizedLineID
      )
  )
}

export function lineIsOccupied(
  lineID: string,
  sessions: readonly CallSession[] = callState.sessions,
  reservations: readonly OutgoingCallReservation[] = callState.reservations
): boolean {
  const normalizedLineID = lineID.trim()
  return Boolean(
    normalizedLineID &&
      (lineHasActiveCall(normalizedLineID, sessions) ||
        reservations.some(
          reservation => reservation.line_id.trim() === normalizedLineID
        ))
  )
}

export function showActiveCallForLine(lineID: string): boolean {
  const normalizedLineID = lineID.trim()
  const selected = callState.sessions.find(
    session =>
      isLiveCallSession(session) &&
      session.line_id.trim() === normalizedLineID
  )
  if (!selected) return false

  const foreground = selectForegroundSession(callState.sessions, selected.id)
  if (!foreground) return false
  applyForegroundSession(foreground)
  showCallSurface()
  return foreground.id === selected.id
}

function requestError(error: unknown, fallback: string): { message: string; status: number } {
  return {
    message: error instanceof Error ? error.message : fallback,
    status: error instanceof ApiError ? error.status : 0
  }
}

function ownedSession(): CallSession | null {
  if (
    callState.session &&
    isLiveCallSession(callState.session) &&
    callState.session.control_state === 'owned'
  ) {
    return callState.session
  }
  return (
    callState.sessions.find(
      session => isLiveCallSession(session) && session.control_state === 'owned'
    ) || null
  )
}

function clearForegroundSession(): void {
  callState.selectedCallID = ''
  callState.session = null
  callState.owned = false
  callState.dtmfDigits = ''
  syncCallSounds(null)
  syncCallMedia(null)
  syncCallRecording(null)
}

function applyForegroundSession(session: CallSession): void {
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
  if (newCall) {
    callState.dtmfDigits = ''
    callState.error = ''
    callState.errorStatus = 0
  }
  callState.selectedCallID = session.id
  callState.session = session
  callState.owned = owned
  if (newCall && (owned || incomingAvailable)) showCallSurface()
  syncCallSounds(
    (owned && !claimedIncomingRinging) || incomingAvailable ? session : null
  )
  syncCallMedia(owned ? session : null)
  syncCallRecording(owned || incomingAvailable ? session : null)
  if (owned) void renewActiveCallLease()
}

function reconcileActiveSnapshot(
  snapshot: ActiveCallSnapshot,
  preferredCallID = callState.selectedCallID
): void {
  const liveSessions = snapshot.calls.filter(isLiveCallSession)
  callState.sessions = liveSessions
  callState.reservations = snapshot.reservations.slice()
  for (const session of liveSessions) showIncomingCallNotification(session)

  const foreground = selectForegroundSession(liveSessions, preferredCallID)
  if (foreground) applyForegroundSession(foreground)
  else clearForegroundSession()
}

function acceptSession(session: CallSession): void {
  const sessions = callState.sessions.slice()
  const index = sessions.findIndex(candidate => candidate.id === session.id)
  if (index >= 0) sessions[index] = session
  else sessions.push(session)
  reconcileActiveSnapshot(
    {
      calls: sessions,
      reservations: callState.reservations
    },
    session.id
  )
}

function sessionCanRenewBrowserLease(session: CallSession): boolean {
  if (!LEASED_PHASES.has(session.phase)) return false
  if (session.phase !== 'active' || !session.media_available) return true
  return LEASED_MEDIA_STATES.has(callMediaState.status)
}

function handleCallLeaseRenewalFailure(callID: string, error: unknown): void {
  if (
    ownedSession()?.id === callID &&
    error instanceof ApiError &&
    (error.status === 404 || error.status === 409)
  ) {
    void requestActiveCallRefresh()
  }
}

export function renewActiveCallLease(): Promise<void> {
  const session = ownedSession()
  if (!session || !sessionCanRenewBrowserLease(session)) {
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
      handleCallLeaseRenewalFailure(callID, error)
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

export async function retryActiveCallMedia(session: CallSession | null): Promise<void> {
  if (!session || session.phase !== 'active' || !session.media_available) return
  if (ownedSession()?.id !== session.id) return
  try {
    await gateway.renewCallLease(session.id)
  } catch (error) {
    handleCallLeaseRenewalFailure(session.id, error)
    return
  }
  if (ownedSession()?.id !== session.id) return
  retryCallMedia(session)
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
    const snapshot = await gateway.getActiveCallSnapshot()
    if (!runtimeStarted || startedAtEpoch !== mutationEpoch) return

    reconcileActiveSnapshot(snapshot)
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
  callState.sessions = []
  callState.reservations = []
  callState.selectedCallID = ''
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
  if (ownedSession()) {
    callState.error = translate('runtime.callInProgress')
    callState.errorStatus = 409
    return false
  }
  if (lineIsOccupied(lineKey)) {
    callState.error = translate('calls.lineInUse')
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
  const selectedCallID = callState.selectedCallID
  reconcileActiveSnapshot({
    calls: callState.sessions.filter(session => session.id !== selectedCallID),
    reservations: callState.reservations
  })
  callState.error = ''
  callState.errorStatus = 0
}
