import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  CallRecordingSegment,
  CallRecordingSnapshot,
  CallRecordingState,
  CallRecordingStatus,
  CallSession,
  RecordingEntry,
  RecordingSettings,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'
import { translate } from '../i18n'
import {
  acceptFirstPage,
  acceptNextPage,
  mergeUnique,
  paginationState,
  resetPagination
} from './pagination'

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
  recordingStatus: CallRecordingStatus
  activeSegmentID: string
  busy: boolean
  error: string
  segmentsStatus: ResourceStatus
  segments: CallRecordingSegment[]
  segmentsError: string
}>({
  callID: '',
  status: 'idle',
  enabled: false,
  recordingStatus: 'off',
  activeSegmentID: '',
  busy: false,
  error: '',
  segmentsStatus: 'idle',
  segments: [],
  segmentsError: ''
})

export const recordingListState = reactive<{
  callID: string
  status: ResourceStatus
  data: CallRecordingSegment[]
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
export const recordingCatalogPagination = reactive(paginationState())

let settingsRequest: Promise<RecordingSettings | null> | undefined
let settingsGeneration = 0
let dialerResetGeneration = 0
let callSyncGeneration = 0
let activeSnapshotCallID = ''
let activeSnapshotEligible = false
let preferredCallID = ''
let preferredCallEnabled = false
let activeSnapshotRequest: Promise<void> | undefined
let activeSnapshotDirty = false
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
  callRecordingState.status = 'ready'
  callRecordingState.enabled = state.enabled
  callRecordingState.recordingStatus = state.status
  callRecordingState.activeSegmentID = state.active_segment_id || ''
  callRecordingState.error = ''
}

export function acceptRuntimeCallRecordings(
  snapshots: CallRecordingSnapshot[]
): void {
  const callID = callRecordingState.callID
  if (!callID || !activeSnapshotEligible || callRecordingState.busy) return
  const snapshot = snapshots.find(item => item.state.call_id === callID)
  if (!snapshot) return

  callSyncGeneration += 1
  activeSnapshotDirty = false
  acceptCallRecording(callID, snapshot.state)
  callRecordingState.segments = snapshot.segments
    .slice()
    .sort((left, right) => left.segment_index - right.segment_index)
  callRecordingState.segmentsStatus = 'ready'
  callRecordingState.segmentsError = ''
}

export async function loadRecordingSettings(force = false): Promise<RecordingSettings | null> {
  if (!force && recordingSettingsState.status === 'ready' && recordingSettingsState.data) {
    return recordingSettingsState.data
  }
  if (settingsRequest) return settingsRequest

  recordingSettingsState.status = 'loading'
  recordingSettingsState.error = ''
  const token = settingsGeneration
  const request = gateway
    .getRecordingSettings()
    .then(settings => {
      if (token !== settingsGeneration) return null
      acceptRecordingSettings(settings)
      recordingSettingsState.status = 'ready'
      return settings
    })
    .catch(error => {
      if (token !== settingsGeneration) return null
      recordingSettingsState.status =
        error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
      recordingSettingsState.error = failureMessage(
        error,
        translate('runtime.recordingSettingsLoadFailed')
      )
      return null
    })
    .finally(() => {
      if (token === settingsGeneration) settingsRequest = undefined
    })
  settingsRequest = request
  return request
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
  if (
    callRecordingState.callID === callID &&
    !activeSnapshotEligible
  ) {
    callSyncGeneration += 1
    callRecordingState.status = 'ready'
    callRecordingState.enabled = enabled
    callRecordingState.error = ''
  }
}

export function preferredCallRecording(callID: string): boolean | undefined {
  return preferredCallID === callID ? preferredCallEnabled : undefined
}

function clearActiveRecording(): void {
  callSyncGeneration += 1
  activeSnapshotCallID = ''
  activeSnapshotEligible = false
  activeSnapshotDirty = false
  callRecordingState.callID = ''
  callRecordingState.status = 'idle'
  callRecordingState.enabled = false
  callRecordingState.recordingStatus = 'off'
  callRecordingState.activeSegmentID = ''
  callRecordingState.busy = false
  callRecordingState.error = ''
  callRecordingState.segmentsStatus = 'idle'
  callRecordingState.segments = []
  callRecordingState.segmentsError = ''
}

async function readActiveCallRecording(callID: string): Promise<void> {
	const normalizedCallID = callID.trim()
	const generation = callSyncGeneration
  if (
    !normalizedCallID ||
    !activeSnapshotEligible ||
    callRecordingState.callID !== normalizedCallID
  ) {
    return
  }

  if (callRecordingState.status === 'idle') {
    callRecordingState.status = 'initializing'
  }
  callRecordingState.segmentsStatus = 'loading'
  callRecordingState.segmentsError = ''
  try {
    const snapshot = await gateway.getCallRecording(normalizedCallID)
    if (
      generation !== callSyncGeneration ||
      !activeSnapshotEligible ||
      callRecordingState.callID !== normalizedCallID
    ) {
      return
    }
    acceptCallRecording(normalizedCallID, snapshot.state)
    callRecordingState.segments = snapshot.segments
      .slice()
      .sort((left, right) => left.segment_index - right.segment_index)
    callRecordingState.segmentsStatus = 'ready'
  } catch (error) {
    if (
      generation !== callSyncGeneration ||
      !activeSnapshotEligible ||
      callRecordingState.callID !== normalizedCallID
    ) {
      return
    }
    callRecordingState.segmentsStatus =
      error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
    callRecordingState.segmentsError = failureMessage(
      error,
      translate('runtime.callRecordingsLoadFailed')
    )
    callRecordingState.status = 'error'
    callRecordingState.error = callRecordingState.segmentsError
  }
}

function requestActiveCallRecordingRefresh(): Promise<void> {
  if (!callRecordingState.callID || !activeSnapshotEligible) {
    return Promise.resolve()
  }

  activeSnapshotDirty = true
  if (callRecordingState.busy || activeSnapshotRequest) {
    return activeSnapshotRequest || Promise.resolve()
  }

  const run = async () => {
    while (activeSnapshotDirty && !callRecordingState.busy) {
      activeSnapshotDirty = false
      const callID = callRecordingState.callID
      if (!callID) return
      await readActiveCallRecording(callID)
    }
  }
  const request = run().finally(() => {
    if (activeSnapshotRequest === request) activeSnapshotRequest = undefined
    if (
      activeSnapshotDirty &&
      !callRecordingState.busy &&
      activeSnapshotEligible &&
      callRecordingState.callID
    ) {
      void requestActiveCallRecordingRefresh()
    }
  })
  activeSnapshotRequest = request
  return request
}

export function syncCallRecording(session: CallSession | null): void {
  if (!session || session.phase === 'ended' || session.phase === 'failed') {
    if (!session || preferredCallID === session.id) preferredCallID = ''
    clearActiveRecording()
    return
  }

  const snapshotEligible = session.control_state === 'owned'
  let snapshotRequested = false
  if (callRecordingState.callID !== session.id) {
    callSyncGeneration += 1
    activeSnapshotCallID = ''
    activeSnapshotEligible = snapshotEligible
    activeSnapshotDirty = false
    callRecordingState.callID = session.id
    callRecordingState.status = 'idle'
    callRecordingState.enabled = false
    callRecordingState.recordingStatus = 'off'
    callRecordingState.activeSegmentID = ''
    callRecordingState.busy = false
    callRecordingState.error = ''
    callRecordingState.segmentsStatus = 'idle'
    callRecordingState.segments = []
    callRecordingState.segmentsError = ''
    if (snapshotEligible) {
      void requestActiveCallRecordingRefresh()
      snapshotRequested = true
    }
  } else if (snapshotEligible && !activeSnapshotEligible) {
    callSyncGeneration += 1
    activeSnapshotCallID = ''
    activeSnapshotEligible = true
    callRecordingState.status = 'initializing'
    callRecordingState.error = ''
    void requestActiveCallRecordingRefresh()
    snapshotRequested = true
  } else {
    activeSnapshotEligible = snapshotEligible
    if (!snapshotEligible) activeSnapshotDirty = false
  }
  if (session.phase !== 'active') {
    if (snapshotEligible) return
    if (preferredCallID === session.id) {
      callRecordingState.status = 'ready'
      callRecordingState.enabled = preferredCallEnabled
      return
    }
    if (
      session.direction !== 'incoming' ||
      session.control_state !== 'available'
    ) {
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
  if (!snapshotEligible) return
  if (activeSnapshotCallID === session.id) return
  activeSnapshotCallID = session.id
  if (!snapshotRequested) void requestActiveCallRecordingRefresh()
}

export async function setCallRecordingEnabled(enabled: boolean): Promise<void> {
  const callID = callRecordingState.callID
  if (
    !callID ||
    !activeSnapshotEligible ||
    callRecordingState.busy ||
    callRecordingState.status !== 'ready' ||
    callRecordingState.segmentsStatus === 'loading'
  ) {
    return
  }

  let mutationError = ''
  callRecordingState.busy = true
  callRecordingState.error = ''
  try {
    const state = await gateway.setCallRecording(callID, enabled)
    if (callRecordingState.callID !== callID) return
    acceptCallRecording(callID, state)
  } catch (error) {
    if (callRecordingState.callID !== callID) return
    mutationError = failureMessage(
      error,
      translate('runtime.recordingToggleFailed')
    )
  } finally {
    if (callRecordingState.callID === callID) {
      callRecordingState.busy = false
      activeSnapshotDirty = true
      await requestActiveCallRecordingRefresh()
      if (callRecordingState.callID === callID && mutationError) {
        callRecordingState.error = mutationError
      }
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
      .sort((left, right) => Date.parse(left.recorded_at) - Date.parse(right.recorded_at))
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
  resetPagination(recordingCatalogPagination)
  const pageGeneration = recordingCatalogPagination.generation
  recordingCatalogState.query = normalizedQuery
  recordingCatalogState.status = 'loading'
  recordingCatalogState.data = []
  recordingCatalogState.error = ''
  try {
    const page = await gateway.listRecordings(
      normalizedQuery ? { q: normalizedQuery } : undefined
    )
    if (
      token !== recordingCatalogGeneration ||
      pageGeneration !== recordingCatalogPagination.generation
    ) return null
    recordingCatalogState.data = page.items
    recordingCatalogState.status = 'ready'
    acceptFirstPage(recordingCatalogPagination, page.meta)
    return page.items
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

export async function loadMoreRecordingEntries(): Promise<RecordingEntry[] | null> {
  const cursor = recordingCatalogPagination.nextCursor
  if (
    recordingCatalogState.status !== 'ready' ||
    !recordingCatalogPagination.hasMore ||
    !cursor ||
    recordingCatalogPagination.loadingMore
  ) return recordingCatalogState.data

  const token = recordingCatalogGeneration
  const pageGeneration = recordingCatalogPagination.generation
  const query = recordingCatalogState.query
  recordingCatalogPagination.loadingMore = true
  recordingCatalogPagination.error = ''
  try {
    const page = await gateway.listRecordings({
      ...(query ? { q: query } : {}),
      cursor
    })
    if (
      token !== recordingCatalogGeneration ||
      pageGeneration !== recordingCatalogPagination.generation ||
      recordingCatalogPagination.nextCursor !== cursor
    ) return null
    recordingCatalogState.data = mergeUnique(
      recordingCatalogState.data,
      page.items,
      recording => recording.id
    )
    acceptNextPage(recordingCatalogPagination, page.meta)
    return recordingCatalogState.data
  } catch (error) {
    if (
      token !== recordingCatalogGeneration ||
      pageGeneration !== recordingCatalogPagination.generation
    ) return null
    if (
      error instanceof ApiError &&
      error.code === 'invalid_argument' &&
      error.field === 'cursor'
    ) {
      return loadRecordingEntries(query, true)
    }
    recordingCatalogPagination.error = failureMessage(
      error,
      translate('runtime.callRecordingsLoadFailed')
    )
    return null
  } finally {
    if (
      token === recordingCatalogGeneration &&
      pageGeneration === recordingCatalogPagination.generation
    ) {
      recordingCatalogPagination.loadingMore = false
    }
  }
}

async function refreshRecordingEntries(): Promise<RecordingEntry[] | null> {
  const token = recordingCatalogGeneration
  const pageGeneration = recordingCatalogPagination.generation
  const query = recordingCatalogState.query
  try {
    const page = await gateway.listRecordings(query ? { q: query } : undefined)
    if (
      token !== recordingCatalogGeneration ||
      pageGeneration !== recordingCatalogPagination.generation
    ) return null
    if (recordingCatalogPagination.pages > 1) {
      recordingCatalogState.data = mergeUnique(
        page.items,
        recordingCatalogState.data,
        recording => recording.id
      )
    } else {
      recordingCatalogState.data = page.items
      acceptFirstPage(recordingCatalogPagination, page.meta)
    }
    recordingCatalogState.status = 'ready'
    recordingCatalogState.error = ''
    return recordingCatalogState.data
  } catch (error) {
    if (
      token !== recordingCatalogGeneration ||
      pageGeneration !== recordingCatalogPagination.generation
    ) return null
    recordingCatalogState.error = failureMessage(
      error,
      translate('runtime.callRecordingsLoadFailed')
    )
    return null
  }
}

export async function refreshRecordingWorkspace(includeActive = true): Promise<void> {
  const requests: Promise<unknown>[] = [
    refreshRecordingEntries()
  ]
  if (includeActive && callRecordingState.callID) {
    if (activeSnapshotEligible) {
      requests.push(requestActiveCallRecordingRefresh())
    }
  }
  if (recordingListState.callID) {
    requests.push(loadCallRecordings(recordingListState.callID, true))
  }
  await Promise.all(requests)
}

export async function deleteRecording(
  callID: string,
  recordingID: string
): Promise<void> {
  const recording = recordingCatalogState.data.find(item => item.id === recordingID)
  await deleteRecordings(
    recording
      ? [recording]
      : [{ call_id: callID, id: recordingID }]
  )
}

export async function deleteRecordings(
  recordings: Array<{ call_id: string; id: string }>
): Promise<void> {
  const unique = Array.from(
    new Map(
      recordings.map(recording => [
        `${recording.call_id}\u0000${recording.id}`,
        recording
      ])
    ).values()
  )
  if (unique.length === 0) return
  await gateway.updateRecordings('delete', unique)
  const deleted = new Set(unique.map(recording => recording.id))
  recordingCatalogState.data = recordingCatalogState.data.filter(
    recording => !deleted.has(recording.id)
  )
  if (recordingListState.callID) {
    recordingListState.data = recordingListState.data.filter(
      recording => !deleted.has(recording.id)
    )
  }
}

export async function setRecordingsFavorite(
  recordings: Array<{ call_id: string; id: string }>,
  favorite: boolean
): Promise<void> {
  const unique = Array.from(
    new Map(
      recordings.map(recording => [
        `${recording.call_id}\u0000${recording.id}`,
        recording
      ])
    ).values()
  )
  if (unique.length === 0) return
  await gateway.updateRecordings(
    favorite ? 'favorite' : 'unfavorite',
    unique
  )
  const selectedCallIDs = new Set(unique.map(recording => recording.call_id))
  for (const recording of recordingCatalogState.data) {
    if (selectedCallIDs.has(recording.call_id)) recording.favorite = favorite
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

export function resetRecordingState(): void {
  settingsGeneration += 1
  dialerResetGeneration += 1
  callSyncGeneration += 1
  recordingListGeneration += 1
  recordingCatalogGeneration += 1
  settingsRequest = undefined
  activeSnapshotCallID = ''
  activeSnapshotEligible = false
  preferredCallID = ''
  preferredCallEnabled = false

  recordingSettingsState.status = 'idle'
  recordingSettingsState.data = null
  recordingSettingsState.saving = false
  recordingSettingsState.error = ''

  dialerRecordingState.status = 'idle'
  dialerRecordingState.enabled = false
  dialerRecordingState.overridden = false
  dialerRecordingState.error = ''

  callRecordingState.callID = ''
  callRecordingState.status = 'idle'
  callRecordingState.enabled = false
  callRecordingState.recordingStatus = 'off'
  callRecordingState.activeSegmentID = ''
  callRecordingState.busy = false
  callRecordingState.error = ''
  callRecordingState.segmentsStatus = 'idle'
  callRecordingState.segments = []
  callRecordingState.segmentsError = ''
  activeSnapshotDirty = false

  recordingListState.callID = ''
  recordingListState.status = 'idle'
  recordingListState.data = []
  recordingListState.error = ''

  recordingCatalogState.query = ''
  recordingCatalogState.status = 'idle'
  recordingCatalogState.data = []
  recordingCatalogState.error = ''
  resetPagination(recordingCatalogPagination)
}
