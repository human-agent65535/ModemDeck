<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  LoaderCircle,
  Mail,
  MailOpen,
  MessageSquareText,
  Phone,
  Star,
  Trash2
} from '@lucide/vue'
import BatchActionBar from '../components/BatchActionBar.vue'
import type { CallFilter, CallRecord } from '../api/types'
import CallHistoryListItem from '../components/CallHistoryListItem.vue'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import FavoriteFilterButton from '../components/FavoriteFilterButton.vue'
import InfiniteScrollTrigger from '../components/InfiniteScrollTrigger.vue'
import LineSelector from '../components/LineSelector.vue'
import LineTag from '../components/LineTag.vue'
import ListSkeleton from '../components/ListSkeleton.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import RecordingList from '../components/RecordingList.vue'
import SearchField from '../components/SearchField.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import LoadingSkeletonBoundary from '../components/skeletons/LoadingSkeletonBoundary.vue'
import WorkspaceDetailSkeleton from '../components/skeletons/WorkspaceDetailSkeleton.vue'
import CommunicationListToolbar from '../components/workspace/CommunicationListToolbar.vue'
import FavoriteActionButton from '../components/workspace/FavoriteActionButton.vue'
import WorkspaceDetailActions from '../components/workspace/WorkspaceDetailActions.vue'
import WorkspaceDetailHeader from '../components/workspace/WorkspaceDetailHeader.vue'
import WorkspaceDetailPane from '../components/workspace/WorkspaceDetailPane.vue'
import WorkspaceListHeader from '../components/workspace/WorkspaceListHeader.vue'
import WorkspaceMasterDetail from '../components/workspace/WorkspaceMasterDetail.vue'
import { useInitialLoadBarrier } from '../composables/useInitialLoadBarrier'
import { useListSelection } from '../composables/useListSelection'
import { skeletonPreviewEnabled } from '../composables/useSkeletonPreview'
import { messageComposeRoute } from '../router/messageRoute'
import { requestConfirmation } from '../state/confirmation'
import { callState } from '../state/call'
import {
  loadRecordingEntries,
  forgetCallRecordings,
  recordingCatalogState
} from '../state/recording'
import { openDialerAndCall } from '../state/ui'
import {
  bootstrapResource,
  callsResource,
  callsPagination,
  capabilityReason,
  contactForNumber,
  deleteCall,
  deleteCalls,
  displayPhoneNumber,
  lineForKey,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadMoreCalls,
  loadContacts,
  markMissedCallRead,
  markMissedCallUnread,
  setCallsFavorite,
  updateMissedCallsReadState
} from '../state/workspace'
import { formatDateTime, formatDuration } from '../utils/format'
import { isContactPhoneCandidate } from '../utils/communicationAddress'
import { lineTagFallback, lineTagLine } from '../utils/lineIdentity'

const props = withDefaults(
  defineProps<{
    embeddedCallId?: string
  }>(),
  {
    embeddedCallId: ''
  }
)
const emit = defineEmits<{
  close: []
  message: [request: { number: string; name: string; contextLineKey: string }]
}>()

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const search = ref('')
const filter = ref<CallFilter>(
  props.embeddedCallId ? 'all' : callFilterFromRoute(route.query.filter)
)
const lineFilterKey = ref('all')
const favoriteOnly = ref(
  !props.embeddedCallId && route.query.favorite === '1'
)
const callMutationError = ref('')
const deletingCallID = ref('')
const favoritePendingCallID = ref('')
const batchBusy = ref(false)
const manuallyUnreadCallIDs = ref(new Set<string>())
const selection = useListSelection<CallRecord>(call => call.id)
const selecting = selection.active
const selectionCount = selection.count
const { loading: initialLoading, waitFor: waitForInitialLoad } = useInitialLoadBarrier()
const lines = computed(() => bootstrapResource.data?.lines || [])
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)

const filters = computed<Array<{ value: CallFilter; label: string }>>(() => [
  { value: 'all', label: t('common.all') },
  { value: 'missed', label: t('calls.missed') },
  { value: 'incoming', label: t('dashboard.incoming') },
  { value: 'outgoing', label: t('dashboard.outgoing') }
])

const filteredCalls = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  const filteredLine =
    lineFilterKey.value === 'all'
      ? undefined
      : lines.value.find(line => lineKey(line) === lineFilterKey.value)
  return callsResource.data
    .filter(call => {
      const callLine = lineForCall(call)
      const matchesFilter =
        filter.value === 'all' ||
        (filter.value === 'missed' && call.missed) ||
        (filter.value === 'incoming' && call.direction === 'incoming') ||
        (filter.value === 'outgoing' && call.direction === 'outgoing')
      const matchesLine =
        !filteredLine || (callLine ? lineKey(callLine) === lineFilterKey.value : false)
      const matchesSearch =
        !query ||
        (call.display_name || '').toLocaleLowerCase().includes(query) ||
        (digits.length > 0 && call.remote_number.replace(/\D/g, '').includes(digits))
      return (
        matchesFilter &&
        matchesLine &&
        matchesSearch &&
        (!favoriteOnly.value || call.favorite)
      )
    })
    .slice()
    .sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))
})
const embedded = computed(() => Boolean(props.embeddedCallId))
const selectedId = computed(() =>
  props.embeddedCallId ||
  (typeof route.query.selected === 'string' ? route.query.selected : '')
)
const selected = computed(() => callsResource.data.find(call => call.id === selectedId.value))
const selectedIsContactable = computed(() =>
  Boolean(selected.value && isContactPhoneCandidate(selected.value.remote_number))
)
const selectedContact = computed(() =>
  selected.value && selectedIsContactable.value
    ? contactForNumber(selected.value.remote_number)
    : undefined
)
const playableRecordingCallIDs = computed(
  () =>
    new Set(
      recordingCatalogState.data
        .filter(recording => recording.playable)
        .map(recording => recording.call_id)
    )
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const messageUnavailable = computed(() => capabilityReason('message'))
const batchCalls = computed(() => selection.selected(filteredCalls.value))
const batchMissedCalls = computed(() => batchCalls.value.filter(call => call.missed))
const batchHasUnread = computed(() =>
  batchMissedCalls.value.some(call => !call.read)
)
const batchAllFavorite = computed(
  () =>
    batchCalls.value.length > 0 &&
    batchCalls.value.every(call => call.favorite)
)

function callFilterFromRoute(value: unknown): CallFilter {
  return value === 'missed' || value === 'incoming' || value === 'outgoing'
    ? value
    : 'all'
}

function callFilterQuery(
  value = filter.value,
  favorite = favoriteOnly.value
): { filter?: CallFilter; favorite?: '1' } {
  return {
    ...(value === 'all' ? {} : { filter: value }),
    ...(favorite ? { favorite: '1' as const } : {})
  }
}

function setFilter(value: CallFilter): void {
  filter.value = value
  if (embedded.value) return
  void router.replace({
    name: 'calls',
    query: {
      ...(selectedId.value ? { selected: selectedId.value } : {}),
      ...callFilterQuery(value)
    }
  })
}

function setFavoriteFilter(value: boolean): void {
  favoriteOnly.value = value
  if (embedded.value) return
  void router.replace({
    name: 'calls',
    query: {
      ...(selectedId.value ? { selected: selectedId.value } : {}),
      ...callFilterQuery(filter.value, value)
    }
  })
}

function displayName(call: CallRecord): string {
  return (
    call.display_name ||
    contactForNumber(call.remote_number)?.display_name ||
    callDisplayNumber(call)
  )
}

function callDisplayNumber(call: CallRecord): string {
  return displayPhoneNumber(call.remote_number, call.line_id)
}

function avatarForCall(call: CallRecord): string {
  return contactForNumber(call.remote_number)?.avatar || ''
}

function directionLabel(call: CallRecord): string {
  if (call.missed) return t('dashboard.missedCall')
  return call.direction === 'incoming'
    ? t('dashboard.incoming')
    : t('dashboard.outgoing')
}

function hasPlayableRecording(call: CallRecord): boolean {
  return playableRecordingCallIDs.value.has(call.id)
}

function recordingCount(call: CallRecord): number {
  return recordingCatalogState.data.filter(recording => recording.call_id === call.id).length
}

function lineForCall(call: CallRecord) {
  return lineForKey(call.line_id)
}

function callLineFallback(call: CallRecord): string {
  const line = lineForCall(call)
  return lineTagFallback(
    line,
    lines.value,
    defaultLineID.value,
    call.line_id
  )
}

function actionLineKey(call: CallRecord): string {
  const line = lines.value.find(candidate => lineKey(candidate) === call.line_id)
  return line ? call.line_id : ''
}

function selectCall(call: CallRecord): void {
  manuallyUnreadCallIDs.value.delete(call.id)
  void router
    .push({
      name: 'calls',
      query: { selected: call.id, ...callFilterQuery() }
    })
    .then(() => acknowledgeSelectedMissedCall())
}

async function acknowledgeMissedCall(call: CallRecord): Promise<void> {
  callMutationError.value = ''
  try {
    await markMissedCallRead(call)
  } catch (error) {
    callMutationError.value = t('calls.markReadFailed', {
      error: error instanceof Error ? error.message : String(error)
    })
  }
}

async function markMissedCallUnreadInView(call: CallRecord): Promise<void> {
  callMutationError.value = ''
  const alreadyManual = manuallyUnreadCallIDs.value.has(call.id)
  manuallyUnreadCallIDs.value.add(call.id)
  try {
    await markMissedCallUnread(call)
  } catch (error) {
    if (!alreadyManual) manuallyUnreadCallIDs.value.delete(call.id)
    callMutationError.value = t('calls.markUnreadFailed', {
      error: error instanceof Error ? error.message : String(error)
    })
  }
}

function toggleMissedCallRead(call: CallRecord): Promise<void> {
  return call.read
    ? markMissedCallUnreadInView(call)
    : acknowledgeMissedCall(call)
}

async function toggleCallFavorite(call: CallRecord): Promise<void> {
  if (favoritePendingCallID.value) return
  favoritePendingCallID.value = call.id
  callMutationError.value = ''
  const favorite = !call.favorite
  try {
    await setCallsFavorite([call], favorite)
    if (!favorite && favoriteOnly.value && selectedId.value === call.id) {
      await router.replace({ name: 'calls', query: callFilterQuery() })
    }
  } catch (error) {
    callMutationError.value =
      error instanceof Error ? error.message : t('common.favoriteFailed')
  } finally {
    favoritePendingCallID.value = ''
  }
}

async function removeCall(call: CallRecord): Promise<void> {
  const recordings = recordingCount(call)
  const confirmed = await requestConfirmation({
    title: t('calls.deleteConfirmTitle'),
    message: recordings
      ? t('calls.deleteConfirmWithRecordings', {
          name: displayName(call),
          count: recordings
        })
      : t('calls.deleteConfirmMessage', { name: displayName(call) }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  deletingCallID.value = call.id
  callMutationError.value = ''
  try {
    await deleteCall(call)
    forgetCallRecordings(call.id)
    if (selectedId.value === call.id) {
      if (embedded.value) emit('close')
      else await router.replace({ name: 'calls', query: callFilterQuery() })
    }
  } catch (error) {
    callMutationError.value =
      error instanceof Error ? error.message : t('calls.deleteFailed')
  } finally {
    deletingCallID.value = ''
  }
}

async function batchSetRead(read: boolean): Promise<void> {
  const calls = batchMissedCalls.value
  if (batchBusy.value || calls.length === 0) return
  batchBusy.value = true
  callMutationError.value = ''
  const insertedManualIDs: string[] = []
  if (!read) {
    for (const call of calls) {
      if (!manuallyUnreadCallIDs.value.has(call.id)) insertedManualIDs.push(call.id)
      manuallyUnreadCallIDs.value.add(call.id)
    }
  }
  try {
    await updateMissedCallsReadState(calls, read)
    if (read) {
      for (const call of calls) manuallyUnreadCallIDs.value.delete(call.id)
    }
  } catch (error) {
    for (const id of insertedManualIDs) manuallyUnreadCallIDs.value.delete(id)
    callMutationError.value = read
      ? t('calls.markReadFailed', {
          error: error instanceof Error ? error.message : String(error)
        })
      : t('calls.markUnreadFailed', {
          error: error instanceof Error ? error.message : String(error)
        })
  } finally {
    batchBusy.value = false
  }
}

async function batchSetFavorite(favorite: boolean): Promise<void> {
  const calls = batchCalls.value
  if (batchBusy.value || calls.length === 0) return
  batchBusy.value = true
  callMutationError.value = ''
  try {
    await setCallsFavorite(calls, favorite)
    if (!favorite && favoriteOnly.value) {
      if (calls.some(call => call.id === selectedId.value)) {
        await router.replace({ name: 'calls', query: callFilterQuery() })
      }
      selection.clear()
    }
  } catch (error) {
    callMutationError.value =
      error instanceof Error ? error.message : t('common.favoriteFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchDelete(): Promise<void> {
  const calls = batchCalls.value
  if (batchBusy.value || calls.length === 0) return
  const recordings = calls.reduce((total, call) => total + recordingCount(call), 0)
  const confirmed = await requestConfirmation({
    title: t('calls.deleteSelectedTitle'),
    message: recordings
      ? t('calls.deleteSelectedWithRecordings', {
          count: calls.length,
          recordings
        })
      : t('calls.deleteSelectedMessage', { count: calls.length }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  batchBusy.value = true
  callMutationError.value = ''
  try {
    const deleted = new Set(calls.map(call => call.id))
    await deleteCalls(calls)
    for (const call of calls) forgetCallRecordings(call.id)
    if (deleted.has(selectedId.value)) {
      await router.replace({ name: 'calls', query: callFilterQuery() })
    }
    selection.exit()
  } catch (error) {
    callMutationError.value =
      error instanceof Error ? error.message : t('calls.deleteFailed')
  } finally {
    batchBusy.value = false
  }
}

function onSelectionKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selection.active.value) selection.exit()
}

function callBack(call: CallRecord): void {
  if (dialUnavailable.value || !isContactPhoneCandidate(call.remote_number)) return
  openDialerAndCall(call.remote_number, displayName(call), actionLineKey(call))
}

function callActionLabel(call: CallRecord): string {
  return call.direction === 'outgoing'
    ? t('calls.callAgain')
    : t('dashboard.callBack')
}

function callActionAriaLabel(call: CallRecord): string {
  return call.direction === 'outgoing'
    ? t('calls.callAgainName', { name: displayName(call) })
    : t('calls.callBackName', { name: displayName(call) })
}

function sendMessage(call: CallRecord): void {
  if (messageUnavailable.value || !isContactPhoneCandidate(call.remote_number)) return
  const selectedLineKey = actionLineKey(call)
  if (embedded.value) {
    emit('message', {
      number: call.remote_number,
      name: displayName(call),
      contextLineKey: selectedLineKey
    })
    return
  }
  void router.push(
    messageComposeRoute({
      recipient: call.remote_number,
      name: displayName(call),
      lineKey: selectedLineKey
    })
  )
}

watch(lines, availableLines => {
  if (
    lineFilterKey.value !== 'all' &&
    !availableLines.some(line => lineKey(line) === lineFilterKey.value)
  ) {
    lineFilterKey.value = 'all'
  }
})

watch([search, lineFilterKey, favoriteOnly], () => selection.clear())

watch(filteredCalls, calls => selection.reconcile(calls))

watch(
  () => route.query.filter,
  value => {
    if (!embedded.value) {
      filter.value = callFilterFromRoute(value)
      selection.clear()
    }
  }
)

watch(
  () => route.query.favorite,
  value => {
    if (!embedded.value) favoriteOnly.value = value === '1'
  }
)

watch(
  [
    selectedId,
    selected,
    () => callsResource.status,
    () => callsPagination.hasMore,
    () => callsPagination.loadingMore
  ],
  ([id, call, status, hasMore, loadingMore]) => {
    if (
      !embedded.value &&
      id &&
      !call &&
      status === 'ready' &&
      hasMore &&
      !loadingMore
    ) {
      void loadMoreCalls()
    }
  }
)

function acknowledgeSelectedMissedCall(): void {
  const call = selected.value
  if (
    !call ||
    !call.missed ||
    call.read ||
    manuallyUnreadCallIDs.value.has(call.id) ||
    document.visibilityState !== 'visible' ||
    !document.hasFocus()
  ) return
  void acknowledgeMissedCall(call)
}

function onCallDocumentVisibilityChange(): void {
  if (document.visibilityState === 'visible') acknowledgeSelectedMissedCall()
}

function onCallWindowFocus(): void {
  acknowledgeSelectedMissedCall()
}

watch(
  [selectedId, selected],
  ([id], [previousID]) => {
    if (id && id !== previousID) manuallyUnreadCallIDs.value.delete(id)
    acknowledgeSelectedMissedCall()
  },
  { immediate: true }
)

watch(
  () =>
    callState.sessions
      .map(session => session.id)
      .sort()
      .join('\u0000'),
  (activeCallIDs, previousActiveCallIDs) => {
    if (
      previousActiveCallIDs &&
      previousActiveCallIDs
        .split('\u0000')
        .some(callID => callID && !activeCallIDs.split('\u0000').includes(callID))
    ) {
      void loadCalls(true)
    }
  }
)

onMounted(() => {
  window.addEventListener('keydown', onSelectionKeydown)
  window.addEventListener('focus', onCallWindowFocus)
  document.addEventListener('visibilitychange', onCallDocumentVisibilityChange)
  void waitForInitialLoad([
    () => loadBootstrap(),
    () => loadCalls(),
    () => loadContacts(),
    () => loadRecordingEntries()
  ])
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
  window.removeEventListener('focus', onCallWindowFocus)
  document.removeEventListener('visibilitychange', onCallDocumentVisibilityChange)
})
</script>

<template>
  <WorkspaceMasterDetail
    class="calls-workspace"
    :has-selection="Boolean(selected)"
    :embedded="embedded"
    :batch-selecting="selecting"
  >
    <template #list>
      <WorkspaceListHeader
        :title="t('shell.calls')"
        compact-mode="hidden"
        :count="
          !initialLoading && callsResource.status === 'ready'
            ? callsResource.data.length
            : undefined
        "
      />
      <CommunicationListToolbar
        class="pane-search--calls"
        :has-line-filter="lines.length > 1"
      >
        <template #primary>
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="callsResource.status !== 'ready' || callsResource.data.length === 0"
            @toggle="selection.toggleMode"
          />
          <SearchField
            v-model="search"
            :placeholder="t('common.search')"
          />
          <LineSelector
            v-if="lines.length > 1"
            v-model="lineFilterKey"
            class="call-line-filter"
            :lines="lines"
            :default-line-id="defaultLineID"
            :label="t('calls.lineFilter')"
            include-all
            filter-mode
            :all-label="t('calls.allLines')"
            :all-description="t('calls.allLinesDescription')"
          />
        </template>
        <template #filters>
          <div class="segmented-control" :aria-label="t('calls.filter')">
            <button
              v-for="item in filters"
              :key="item.value"
              type="button"
              :class="{ 'is-active': filter === item.value }"
              @click="setFilter(item.value)"
            >
              {{ item.label }}
            </button>
          </div>
          <FavoriteFilterButton
            :active="favoriteOnly"
            :label="t('common.favoriteOnly')"
            @toggle="setFavoriteFilter(!favoriteOnly)"
          />
        </template>
      </CommunicationListToolbar>
      <div
        v-if="callState.syncStatus === 'loading'"
        class="call-sync-status"
        role="status"
      >
        <LoaderCircle class="spin" :size="14" />
        {{ t('calls.connecting') }}
      </div>
      <div
        v-else-if="callState.syncStatus === 'error' || callState.syncStatus === 'forbidden'"
        class="call-sync-status call-sync-status--error"
        role="alert"
      >
        {{ callState.syncError }}
      </div>
      <div
        v-if="callMutationError"
        class="call-sync-status call-sync-status--error"
        role="alert"
      >
        {{ callMutationError }}
      </div>

      <LoadingSkeletonBoundary
        :loading="initialLoading || callsResource.status === 'loading'"
      >
        <template #skeleton>
          <ListSkeleton :label="t('calls.loading')" />
        </template>
        <StatePanel
          v-if="callsResource.status === 'forbidden'"
          state="forbidden"
          :title="t('calls.forbidden')"
          :detail="callsResource.error"
        />
        <StatePanel
          v-else-if="callsResource.status === 'error'"
          state="error"
          :title="t('calls.loadFailed')"
          :detail="callsResource.error"
          retryable
          @retry="loadCalls(true)"
        />
        <div v-else class="item-list">
        <StatePanel
          v-if="
            filteredCalls.length === 0 &&
            !callsPagination.hasMore &&
            !callsPagination.loadingMore
          "
          state="empty"
          :title="
            search || filter !== 'all' || favoriteOnly || lineFilterKey !== 'all'
              ? t('calls.noMatches')
              : t('calls.empty')
          "
        />
        <template v-else>
          <SelectableListRow
            v-for="call in filteredCalls"
            :key="call.id"
            :active="selecting"
            :selected="selection.has(call)"
            :label="t('common.selectItem', { name: displayName(call) })"
            @toggle="selection.toggle(call)"
          >
            <SwipeActionRow
              :can-read="call.missed"
              :read-mode="call.read ? 'unread' : 'read'"
              :read-label="
                call.read
                  ? t('common.markUnread')
                  : t('common.markRead')
              "
              :delete-label="t('common.delete')"
              :disabled="
                selecting ||
                Boolean(deletingCallID) ||
                favoritePendingCallID === call.id
              "
              @read="toggleMissedCallRead(call)"
              @delete="removeCall(call)"
            >
              <CallHistoryListItem
                :call="call"
                :name="displayName(call)"
                :number="callDisplayNumber(call)"
                :avatar="avatarForCall(call)"
                :line="lineTagLine(lineForCall(call), call.line_id)"
                :line-fallback="callLineFallback(call)"
                :selected="call.id === selectedId"
                :has-recording="hasPlayableRecording(call)"
                @select="selectCall"
              />
            </SwipeActionRow>
          </SelectableListRow>
        </template>
        <InfiniteScrollTrigger
          :has-more="callsPagination.hasMore"
          :loading="callsPagination.loadingMore"
          :error="callsPagination.error"
          :loading-label="t('calls.loading')"
          :retry-label="t('common.retry')"
          @load="loadMoreCalls"
        />
        </div>
      </LoadingSkeletonBoundary>
      <BatchActionBar
        v-if="selecting"
        :selected="selectionCount"
        :total="filteredCalls.length"
        :selected-label="t('common.selectedCount', { count: selectionCount })"
        :select-all-label="t('common.selectAll')"
        :clear-all-label="t('common.clearAll')"
        :done-label="t('common.done')"
        :busy="batchBusy"
        @select-all="selection.selectAll(filteredCalls)"
        @done="selection.exit"
      >
        <button
          v-if="batchMissedCalls.length > 0"
          type="button"
          :disabled="batchBusy"
          :title="batchHasUnread ? t('common.markRead') : t('common.markUnread')"
          @click="batchSetRead(batchHasUnread)"
        >
          <MailOpen v-if="batchHasUnread" :size="17" />
          <Mail v-else :size="17" />
          <span>
            {{ batchHasUnread ? t('common.markRead') : t('common.markUnread') }}
          </span>
        </button>
        <button
          v-if="batchCalls.length > 0"
          type="button"
          :disabled="batchBusy"
          :title="
            batchAllFavorite
              ? t('common.unfavorite')
              : t('common.favorite')
          "
          @click="batchSetFavorite(!batchAllFavorite)"
        >
          <Star
            :size="17"
            :fill="batchAllFavorite ? 'currentColor' : 'none'"
          />
          <span>
            {{
              batchAllFavorite
                ? t('common.unfavorite')
                : t('common.favorite')
            }}
          </span>
        </button>
        <button
          v-if="batchCalls.length > 0"
          class="is-danger"
          type="button"
          :disabled="batchBusy"
          :title="t('common.delete')"
          @click="batchDelete"
        >
          <Trash2 :size="17" />
          <span>{{ t('common.delete') }}</span>
        </button>
      </BatchActionBar>
    </template>

    <template #detail>
      <WorkspaceDetailPane
        :content-key="initialLoading || skeletonPreviewEnabled ? null : selected?.id"
      >
        <template v-if="selected">
          <WorkspaceDetailHeader>
          <template #identity>
            <ContactHeaderIdentity
              :name="displayName(selected)"
              :number="callDisplayNumber(selected)"
              :avatar="selectedContact?.avatar"
              :line="lineTagLine(lineForCall(selected), selected.line_id)"
              :line-fallback="callLineFallback(selected)"
            />
          </template>
          <template #actions>
            <WorkspaceDetailActions>
              <template #primary>
                <button
                  v-if="selectedIsContactable"
                  class="workspace-detail-command workspace-detail-command--primary"
                  type="button"
                  :disabled="Boolean(dialUnavailable)"
                  :title="dialUnavailable || callActionLabel(selected)"
                  :aria-label="callActionAriaLabel(selected)"
                  @click="callBack(selected)"
                >
                  <Phone :size="17" />
                  <span>{{ callActionLabel(selected) }}</span>
                </button>
                <button
                  v-if="selectedIsContactable"
                  class="workspace-detail-command"
                  type="button"
                  :disabled="Boolean(messageUnavailable)"
                  :title="messageUnavailable || t('messages.sendMessage')"
                  :aria-label="t('calls.messageName', { name: displayName(selected) })"
                  @click="sendMessage(selected)"
                >
                  <MessageSquareText :size="17" />
                  <span>{{ t('shell.messages') }}</span>
                </button>
              </template>
              <template #secondary>
                <ContactNumberActions
                  :number="selected.remote_number"
                  :contact="selectedContact"
                  compact
                />
                <button
                  class="icon-button icon-button--danger desktop-delete-action"
                  type="button"
                  :disabled="Boolean(deletingCallID)"
                  :title="t('calls.delete')"
                  @click="removeCall(selected)"
                >
                  <Trash2 :size="18" />
                </button>
                <FavoriteActionButton
                  :active="selected.favorite"
                  :disabled="Boolean(favoritePendingCallID)"
                  :activate-label="t('common.favorite')"
                  :deactivate-label="t('common.unfavorite')"
                  @toggle="toggleCallFavorite(selected)"
                />
              </template>
            </WorkspaceDetailActions>
          </template>
        </WorkspaceDetailHeader>

        <div class="call-detail">
          <section class="detail-section detail-facts">
            <h3>{{ t('dashboard.callDetails') }}</h3>
            <dl>
              <div>
                <dt>{{ t('dashboard.direction') }}</dt>
                <dd>{{ directionLabel(selected) }}</dd>
              </div>
              <div>
                <dt>{{ t('dashboard.time') }}</dt>
                <dd>{{ formatDateTime(selected.started_at) }}</dd>
              </div>
              <div>
                <dt>{{ t('dashboard.duration') }}</dt>
                <dd>
                  {{
                    selected.missed
                      ? t('dashboard.notConnected')
                      : formatDuration(selected.duration_seconds)
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t('dashboard.line') }}</dt>
                <dd>
                  <LineTag
                    :line="lineTagLine(lineForCall(selected), selected.line_id)"
                    :fallback="callLineFallback(selected)"
                  />
                </dd>
              </div>
              <div v-if="selected.failure_reason">
                <dt>{{ t('dashboard.result') }}</dt>
                <dd>{{ selected.failure_reason }}</dd>
              </div>
            </dl>
          </section>

          <RecordingList :call-id="selected.id" />

          <p v-if="dialUnavailable || messageUnavailable" class="unavailable-note">
            {{ dialUnavailable || messageUnavailable }}
          </p>
          </div>
        </template>
        <template #empty>
          <LoadingSkeletonBoundary :loading="initialLoading">
            <template #skeleton>
              <WorkspaceDetailSkeleton
                :label="t('calls.loading')"
                shape="communication"
              />
            </template>
            <StatePanel
              state="empty"
              :title="t('calls.select')"
              :detail="t('calls.detailPlaceholder')"
            />
          </LoadingSkeletonBoundary>
        </template>
      </WorkspaceDetailPane>
    </template>
  </WorkspaceMasterDetail>
</template>

<style scoped>
.calls-workspace.is-embedded {
  grid-template-columns: minmax(0, 1fr);
}

.call-sync-status span {
  flex: 1 1 auto;
}

.call-sync-status button {
  flex: 0 0 auto;
  color: inherit;
  font-weight: 700;
}

@media (max-width: 1100px) {
  .desktop-delete-action {
    display: none;
  }
}

</style>
