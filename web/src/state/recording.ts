import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  CallRecording,
  CallRecordingState,
  CallSession,
  RecordingEntry,
  RecordingSettings,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'

type DialerRecordingStatus = 'idle' | 'loading' | 'ready' | 'error'
type ActiveRecordingStatus = 'idle' | 'initializing' | 'ready' | 'error'

export const recordingSettingsState = reactive<{
  status: ResourceStatus
  data: RecordingSettings | null
  saving: boolean
  error: string
}>({
  status: 'idle',
  data: null,
  saving: false,
  error: ''
})

export const dialerRecordingState = reactive<{
  status: DialerRecordingStatus
  enabled: boolean
  overridden: boolean
  error: string
}>({
  status: 'idle',
  enabled: false,
  overridden: false,
  error: ''
})

export const callRecordingState = reactive<{
  callID: string
  status: ActiveRecordingStatus
  enabled: boolean
  active: boolean
  startedAt: string
  busy: boolean
  error: string
}>({
  callID: '',
  status: 'idle',
  enabled: false,
  active: false,
  startedAt: '',
  busy: false,
  error: ''
})

export const recordingListState = reactive<{
  callID: string
  status: ResourceStatus
  data: CallRecording[]
  error: string
}>({
  callID: '',
  status: 'idle',
  data: [],
  error: ''
})

export const recordingCatalogState = reactive<{
  query: string
  status: ResourceStatus
  data: RecordingEntry[]
  error: string
}>({
  query: '',
  status: 'idle',
  data: [],
  error: ''
})

let settingsRequest: Promise<RecordingSettings | null> | undefined
let dialerResetGeneration = 0
let callSyncGeneration = 0
let attemptedCallID = ''
let preferredCallID = ''
let preferredCallEnabled = false
let recordingListGeneration = 0
let recordingCatalogGeneration = 0

function failureMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 403) {
    return translate('runtime.recordingForbidden')
  }
  return error instanceof Error ? error.message : fallback
}

function acceptRecordingSettings(settings: RecordingSettings): void {
  const followsDefault =
    dialerRecordingState.status === 'ready' &&
    !dialerRecordingState.overridden
  recordingSettingsState.data = settings
  if (followsDefault) dialerRecordingState.enabled = settings.default_enabled
  if (dialerRecordingState.status === 'ready') {
    dialerRecordingState.overridden =
      dialerRecordingState.enabled !== settings.default_enabled
  }
}

function acceptCallRecording(callID: string, state: CallRecordingState): void {
  if (state.call_id !== callID) {
    throw new Error(translate('runtime.recordingCallMismatch'))
  }
  callRecordingState.callID = callID
  callRecordingState.status = state.error ? 'error' : 'ready'
  callRecordingState.enabled = state.enabled
  callRecordingState.active = state.active
  callRecordingState.startedAt = state.started_at || ''
  callRecordingState.error = state.error || ''
}

export async function loadRecordingSettings(force = false): Promise<RecordingSettings | null> {
  if (!force && recordingSettingsState.status === 'ready' && recordingSettingsState.data) {
    return recordingSettingsState.data
  }
  if (settingsRequest) return settingsRequest

  recordingSettingsState.status = 'loading'
  recordingSettingsState.error = ''
  settingsRequest = gateway
    .getRecordingSettings()
    .then(settings => {
      acceptRecordingSettings(settings)
      recordingSettingsState.status = 'ready'
      return settings
    })
    .catch(error => {
      recordingSettingsState.status =
        error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
      recordingSettingsState.error = failureMessage(
        error,
        translate('runtime.recordingSettingsLoadFailed')
      )
      return null
    })
    .finally(() => {
      settingsRequest = undefined
    })
  return settingsRequest
}

export async function updateDefaultRecording(enabled: boolean): Promise<boolean> {
  if (recordingSettingsState.saving) return false
  const current =
    recordingSettingsState.data ||
    (await loadRecordingSettings())
  if (!current) return false

  recordingSettingsState.saving = true
  recordingSettingsState.error = ''
  try {
    const settings = await gateway.updateRecordingSettings({
      default_enabled: enabled,
      revision: current.revision
    })
    acceptRecordingSettings(settings)
    recordingSettingsState.status = 'ready'
    return true
  } catch (error) {
    recordingSettingsState.error = failureMessage(
      error,
      translate('runtime.recordingSettingsSaveFailed')
    )
    return false
  } finally {
    recordingSettingsState.saving = false
  }
}

export async function resetDialerRecording(): Promise<void> {
  const token = ++dialerResetGeneration
  dialerRecordingState.status = 'loading'
  dialerRecordingState.overridden = false
  dialerRecordingState.error = ''

  const settings = await loadRecordingSettings()
  if (token !== dialerResetGeneration) return
  if (!settings) {
    dialerRecordingState.status = 'error'
    dialerRecordingState.enabled = false
    dialerRecordingState.error =
      recordingSettingsState.error || translate('runtime.recordingSettingsLoadFailed')
    return
  }
  dialerRecordingState.enabled = settings.default_enabled
  dialerRecordingState.status = 'ready'
}

export function setDialerRecording(enabled: boolean): void {
  if (dialerRecordingState.status !== 'ready') return
  dialerRecordingState.enabled = enabled
  dialerRecordingState.overridden =
    enabled !== Boolean(recordingSettingsState.data?.default_enabled)
}

export function rememberCallRecordingPreference(callID: string, enabled: boolean): void {
  preferredCallID = callID
  preferredCallEnabled = enabled
  if (callRecordingState.callID === callID && !callRecordingState.active) {
    callRecordingState.status = 'ready'
    callRecordingState.enabled = enabled
    callRecordingState.error = ''
  }
}

function clearActiveRecording(): void {
  callSyncGeneration += 1
  attemptedCallID = ''
  callRecordingState.callID = ''
  callRecordingState.status = 'idle'
  callRecordingState.enabled = false
  callRecordingState.active = false
  callRecordingState.startedAt = ''
  callRecordingState.busy = false
  callRecordingState.error = ''
}

export function syncCallRecording(session: CallSession | null): void {
  if (!session || session.phase === 'ended' || session.phase === 'failed') {
    if (!session || preferredCallID === session.id) preferredCallID = ''
    clearActiveRecording()
    return
  }

  if (callRecordingState.callID !== session.id) {
    callSyncGeneration += 1
    attemptedCallID = ''
    callRecordingState.callID = session.id
    callRecordingState.status = 'idle'
    callRecordingState.enabled = false
    callRecordingState.active = false
    callRecordingState.startedAt = ''
    callRecordingState.busy = false
    callRecordingState.error = ''
  }
  if (session.phase !== 'active') {
    if (preferredCallID === session.id) {
      callRecordingState.status = 'ready'
      callRecordingState.enabled = preferredCallEnabled
      return
    }
    if (callRecordingState.status !== 'idle') return

    const token = ++callSyncGeneration
    callRecordingState.status = 'initializing'
    void loadRecordingSettings().then(settings => {
      if (
        token !== callSyncGeneration ||
        callRecordingState.callID !== session.id
      ) {
        return
      }
      if (!settings) {
        callRecordingState.status = 'error'
        callRecordingState.error =
          recordingSettingsState.error ||
          translate('runtime.defaultRecordingReadFailed')
        return
      }
      callRecordingState.status = 'ready'
      callRecordingState.enabled = settings.default_enabled
    })
    return
  }
  if (attemptedCallID === session.id) return

  attemptedCallID = session.id
  const token = ++callSyncGeneration
  callRecordingState.status = 'initializing'
  callRecordingState.error = ''
  void (async () => {
    let enabled: boolean
    if (preferredCallID === session.id) {
      enabled = preferredCallEnabled
    } else {
      const settings = await loadRecordingSettings()
      if (!settings) {
        if (token !== callSyncGeneration || callRecordingState.callID !== session.id) return
        callRecordingState.status = 'error'
        callRecordingState.error =
          recordingSettingsState.error || translate('runtime.defaultRecordingReadFailed')
        return
      }
      enabled = settings.default_enabled
    }

    try {
      const state = await gateway.setCallRecording(session.id, enabled)
      if (token !== callSyncGeneration || callRecordingState.callID !== session.id) return
      acceptCallRecording(session.id, state)
    } catch (error) {
      if (token !== callSyncGeneration || callRecordingState.callID !== session.id) return
      callRecordingState.status = 'error'
      callRecordingState.error = failureMessage(
        error,
        translate('runtime.recordingApplyFailed')
      )
    }
  })()
}

export async function setActiveCallRecording(enabled: boolean): Promise<void> {
  const callID = callRecordingState.callID
  if (!callID || callRecordingState.busy) return

  const token = ++callSyncGeneration
  callRecordingState.busy = true
  callRecordingState.error = ''
  try {
    const state = await gateway.setCallRecording(callID, enabled)
    if (token !== callSyncGeneration || callRecordingState.callID !== callID) return
    acceptCallRecording(callID, state)
  } catch (error) {
    if (token !== callSyncGeneration || callRecordingState.callID !== callID) return
    callRecordingState.status = 'error'
    callRecordingState.error = failureMessage(
      error,
      translate('runtime.recordingToggleFailed')
    )
  } finally {
    if (token === callSyncGeneration && callRecordingState.callID === callID) {
      callRecordingState.busy = false
    }
  }
}

export async function loadCallRecordings(callID: string, force = false): Promise<void> {
  const normalizedCallID = callID.trim()
  if (!normalizedCallID) {
    recordingListGeneration += 1
    recordingListState.callID = ''
    recordingListState.status = 'idle'
    recordingListState.data = []
    recordingListState.error = ''
    return
  }
  if (
    !force &&
    recordingListState.callID === normalizedCallID &&
    recordingListState.status === 'ready'
  ) {
    return
  }

  const token = ++recordingListGeneration
  recordingListState.callID = normalizedCallID
  recordingListState.status = 'loading'
  recordingListState.data = []
  recordingListState.error = ''
  try {
    const recordings = await gateway.listCallRecordings(normalizedCallID)
    if (token !== recordingListGeneration) return
    recordingListState.data = recordings
      .slice()
      .sort((left, right) => Date.parse(left.started_at) - Date.parse(right.started_at))
    recordingListState.status = 'ready'
  } catch (error) {
    if (token !== recordingListGeneration) return
    recordingListState.status =
      error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
    recordingListState.error = failureMessage(
      error,
      translate('runtime.callRecordingsLoadFailed')
    )
  }
}

export async function loadRecordingEntries(
  query = '',
  force = false
): Promise<RecordingEntry[] | null> {
  const normalizedQuery = query.trim()
  if (
    !force &&
    recordingCatalogState.query === normalizedQuery &&
    recordingCatalogState.status === 'ready'
  ) {
    return recordingCatalogState.data
  }

  const token = ++recordingCatalogGeneration
  recordingCatalogState.query = normalizedQuery
  recordingCatalogState.status = 'loading'
  recordingCatalogState.data = []
  recordingCatalogState.error = ''
  try {
    const recordings = await gateway.listRecordings(
      normalizedQuery ? { q: normalizedQuery } : undefined
    )
    if (token !== recordingCatalogGeneration) return null
    recordingCatalogState.data = recordings
    recordingCatalogState.status = 'ready'
    return recordings
  } catch (error) {
    if (token !== recordingCatalogGeneration) return null
    recordingCatalogState.status =
      error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
    recordingCatalogState.error =
      error instanceof ApiError && error.status === 403
        ? translate('runtime.callRecordingsForbidden')
        : failureMessage(error, translate('runtime.callRecordingsLoadFailed'))
    return null
  }
}

export async function refreshRecordingWorkspace(): Promise<void> {
  const requests: Promise<unknown>[] = [
    loadRecordingEntries(recordingCatalogState.query, true)
  ]
  if (recordingListState.callID) {
    requests.push(loadCallRecordings(recordingListState.callID, true))
  }
  await Promise.all(requests)
}

export async function deleteRecording(
  callID: string,
  recordingID: string
): Promise<void> {
  await gateway.deleteRecording(callID, recordingID)
  recordingCatalogState.data = recordingCatalogState.data.filter(
    recording => recording.id !== recordingID
  )
  if (recordingListState.callID === callID) {
    recordingListState.data = recordingListState.data.filter(
      recording => recording.id !== recordingID
    )
  }
}

export function forgetCallRecordings(callID: string): void {
  recordingCatalogState.data = recordingCatalogState.data.filter(
    recording => recording.call_id !== callID
  )
  if (recordingListState.callID === callID) {
    recordingListState.callID = ''
    recordingListState.status = 'idle'
    recordingListState.data = []
    recordingListState.error = ''
  }
}
