<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertCircle,
  ArrowLeft,
  ChartNoAxesCombined,
  ChevronRight,
  House,
  Inbox,
  Mail,
  MailOpen,
  MessageSquareText,
  Phone,
  PhoneMissed,
  RadioTower,
  Star,
  Trash2,
  UserPlus
} from '@lucide/vue'
import BatchActionBar from '../components/BatchActionBar.vue'
import type {
  CallRecord,
  Contact,
  ContactInput,
  LineSummary,
  MessageThread
} from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import CallHistoryListItem from '../components/CallHistoryListItem.vue'
import ContactEditor from '../components/ContactEditor.vue'
import MessageThreadListItem from '../components/MessageThreadListItem.vue'
import ModuleCard from '../components/ModuleCard.vue'
import ListSkeleton from '../components/ListSkeleton.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import TrafficSummary from '../components/TrafficSummary.vue'
import LoadingSkeletonBoundary from '../components/skeletons/LoadingSkeletonBoundary.vue'
import SectionSkeleton from '../components/skeletons/SectionSkeleton.vue'
import WorkspaceDetailSkeleton from '../components/skeletons/WorkspaceDetailSkeleton.vue'
import WorkspaceListHeader from '../components/workspace/WorkspaceListHeader.vue'
import WorkspaceMasterDetail from '../components/workspace/WorkspaceMasterDetail.vue'
import { useInitialLoadBarrier } from '../composables/useInitialLoadBarrier'
import { useListSelection } from '../composables/useListSelection'
import { skeletonPreviewEnabled } from '../composables/useSkeletonPreview'
import { messageThreadReference } from '../router/messageRoute'
import { requestConfirmation } from '../state/confirmation'
import { showSuccess } from '../state/feedback'
import { selectDeviceConfiguration } from '../state/deviceConfiguration'
import { loadNetwork, networkState } from '../state/network'
import {
  forgetCallRecordings,
  loadRecordingEntries,
  recordingCatalogState
} from '../state/recording'
import { openDialer } from '../state/ui'
import CallsView from './CallsView.vue'
import MessagesView from './MessagesView.vue'
import {
  bootstrapResource,
  callsResource,
  capabilityReason,
  contactEditingAvailable,
  contactForNumber,
  contactsResource,
  deleteCall,
  deleteCalls,
  deleteMessageThread,
  deleteMessageThreads,
  devicesResource,
  displayPhoneNumber,
  displayModuleLines,
  lineCanPlaceVoiceCall,
  lineForKey,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadContacts,
  loadDevices,
  loadThreads,
  markMissedCallRead,
  markMissedCallUnread,
  markThreadRead,
  markThreadsRead,
  markThreadsUnread,
  presentModuleLines,
  recentIncomingThreadKeys,
  saveContact,
  setCallsFavorite,
  setThreadsFavorite,
  threadReadErrors,
  threadIsUnread,
  threadsResource,
  updateMissedCallsReadState
} from '../state/workspace'
import {
  isMessagingServiceReady,
  isRegisteredNetwork,
  isVoiceServiceReady
} from '../utils/operatorNetwork'
import { primaryPhone, primaryPhoneDestination } from '../utils/format'
import { lineTagFallback, lineTagLine } from '../utils/lineIdentity'

type DashboardActivity =
  | {
      key: string
      kind: 'call'
      timestamp: string
      call: CallRecord
    }
  | {
      key: string
      kind: 'message'
      timestamp: string
      thread: MessageThread
    }

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const composingMessage = ref(false)
const messageComposeRecipient = ref('')
const messageComposeRecipientName = ref('')
const messageComposeLineKey = ref('')

const lines = computed(() => bootstrapResource.data?.lines || [])
const moduleLines = computed(() => displayModuleLines(lines.value, devicesResource.data))
const presentModules = computed(() =>
  presentModuleLines(lines.value, devicesResource.data)
)
const contactLines = computed(() => lines.value.filter(line => Boolean(lineKey(line))))
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const selectionKey = computed(() =>
  typeof route.query.item === 'string' ? route.query.item : ''
)
const backLabel = computed(() =>
  route.query.from === 'settings' ? t('settings.back') : t('dashboard.backHome')
)
const hasSelection = computed(() => Boolean(selectionKey.value) || composingMessage.value)
const overviewSelected = computed(
  () =>
    !composingMessage.value &&
    (!selectionKey.value || selectionKey.value === 'overview')
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const messageUnavailable = computed(() => capabilityReason('message'))
const contactEditorOpen = ref(false)
const contactSaving = ref(false)
const contactEditorError = ref('')
const activityMutationError = ref('')
const deletingActivityKey = ref('')
const batchBusy = ref(false)
const selection = useListSelection<DashboardActivity>(activity => activity.key)
const selecting = selection.active
const selectionCount = selection.count
const { loading: initialLoading, waitFor: waitForInitialLoad } = useInitialLoadBarrier()
const unreadMessages = computed(() =>
  threadsResource.data.reduce(
    (total, thread) =>
      total + Math.max(thread.unread_count, thread.marked_unread ? 1 : 0),
    0
  )
)
const missedCalls = computed(
  () => callsResource.data.filter(call => call.missed && !call.read).length
)
const onlineModules = computed(
  () => presentModules.value.filter(line => isRegisteredNetwork(line)).length
)
const callReadyLines = computed(() => {
  if (bootstrapResource.data?.capabilities.webrtc_audio !== true) return 0
  return lines.value.filter(
    line =>
      isVoiceServiceReady(line) &&
      lineCanPlaceVoiceCall(line)
  ).length
})
const messageReadyLines = computed(
  () =>
    lines.value.filter(
      line =>
        isMessagingServiceReady(line) &&
        (line.capabilities?.message === true ||
          line.capabilities?.messaging === true)
    ).length
)
const trafficSnapshot = computed(() => networkState.snapshot)
const todayTraffic = computed(
  () =>
    (trafficSnapshot.value?.today_total.rx_bytes || 0) +
    (trafficSnapshot.value?.today_total.tx_bytes || 0)
)
const monthTraffic = computed(
  () =>
    (trafficSnapshot.value?.month_total.rx_bytes || 0) +
    (trafficSnapshot.value?.month_total.tx_bytes || 0)
)
const connectedNetworkLines = computed(
  () => trafficSnapshot.value?.lines.filter(line => line.connected).length || 0
)
const runningProxies = computed(
  () => trafficSnapshot.value?.proxies.filter(proxy => proxy.running).length || 0
)

function networkRuntime(line: LineSummary) {
  const id = lineKey(line)
  return trafficSnapshot.value?.lines.find(runtime => runtime.line_id === id)
}

const activities = computed<DashboardActivity[]>(() => {
  const calls: DashboardActivity[] = callsResource.data.map(call => ({
    key: `call:${call.id}`,
    kind: 'call',
    timestamp: call.started_at,
    call
  }))
  const messages: DashboardActivity[] = threadsResource.data.map(thread => ({
    key: `message:${messageThreadReference(thread.key)}`,
    kind: 'message',
    timestamp: thread.last_timestamp,
    thread
  }))
  return calls
    .concat(messages)
    .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))
    .slice(0, 30)
})
const batchActivities = computed(() => selection.selected(activities.value))
const batchMessageActivities = computed(() =>
  batchActivities.value.filter(
    (activity): activity is Extract<DashboardActivity, { kind: 'message' }> =>
      activity.kind === 'message'
  )
)
const batchCallActivities = computed(() =>
  batchActivities.value.filter(
    (activity): activity is Extract<DashboardActivity, { kind: 'call' }> =>
      activity.kind === 'call'
  )
)
const batchMissedCallActivities = computed(() =>
  batchCallActivities.value.filter(activity => activity.call.missed)
)
const batchHasReadState = computed(
  () =>
    batchMessageActivities.value.length > 0 ||
    batchMissedCallActivities.value.length > 0
)
const batchHasUnread = computed(
  () =>
    batchMessageActivities.value.some(activity =>
      threadIsUnread(activity.thread)
    ) ||
    batchMissedCallActivities.value.some(activity => !activity.call.read)
)
const batchAllFavorite = computed(
  () =>
    batchActivities.value.length > 0 &&
    batchMessageActivities.value.every(activity => activity.thread.favorite) &&
    batchCallActivities.value.every(activity => activity.call.favorite)
)

const selectedActivity = computed(() =>
  activities.value.find(activity => activity.key === selectionKey.value)
)
const selectedCall = computed(() =>
  selectedActivity.value?.kind === 'call' ? selectedActivity.value.call : undefined
)
const selectedThread = computed(() =>
  selectedActivity.value?.kind === 'message' ? selectedActivity.value.thread : undefined
)
const activityLoading = computed(
  () =>
    initialLoading.value ||
    ((callsResource.status === 'idle' ||
      callsResource.status === 'loading' ||
      threadsResource.status === 'idle' ||
      threadsResource.status === 'loading') &&
      activities.value.length === 0)
)
const activityErrors = computed(() =>
  [
    callsResource.status === 'error' || callsResource.status === 'forbidden'
      ? callsResource.error || t('dashboard.loadCallsFailed')
      : '',
    threadsResource.status === 'error' || threadsResource.status === 'forbidden'
      ? threadsResource.error || t('dashboard.loadMessagesFailed')
      : ''
  ].filter(Boolean)
)
const activityRetryable = computed(
  () => callsResource.status === 'error' || threadsResource.status === 'error'
)

const favoriteContacts = computed(() =>
  contactsResource.data
    .filter(contact => contact.favorite)
    .slice(0, 6)
)

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let value = bytes / 1024
  let unit = units[0]
  for (let index = 1; index < units.length && value >= 1024; index += 1) {
    value /= 1024
    unit = units[index]
  }
  const precision = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${unit}`
}

function callName(call: CallRecord): string {
  return (
    call.display_name ||
    contactForNumber(call.remote_number)?.display_name ||
    displayPhoneNumber(call.remote_number, call.line_id)
  )
}

function threadName(thread: MessageThread): string {
  return (
    thread.contact_name ||
    contactForNumber(thread.peer)?.display_name ||
    displayPhoneNumber(thread.peer, thread.line_id)
  )
}

function lineForCall(call: CallRecord): LineSummary | undefined {
  return lineForKey(call.line_id)
}

function lineForThread(thread: MessageThread): LineSummary | undefined {
  return lineForKey(thread.line_id)
}

function avatarForNumber(number: string): string {
  return contactForNumber(number)?.avatar || ''
}

function callLineFallback(call: CallRecord): string {
  return lineTagFallback(
    lineForCall(call),
    lines.value,
    defaultLineID.value,
    call.line_id
  )
}

function threadLineFallback(thread: MessageThread): string {
  return lineTagFallback(
    lineForThread(thread),
    lines.value,
    defaultLineID.value,
    thread.line_id
  )
}

function activityHasReadState(activity: DashboardActivity): boolean {
  return activity.kind === 'message' || activity.call.missed
}

function activityIsUnread(activity: DashboardActivity): boolean {
  return activity.kind === 'message'
    ? threadIsUnread(activity.thread)
    : activity.call.missed && !activity.call.read
}

function hasPlayableRecording(call: CallRecord): boolean {
  return recordingCatalogState.data.some(
    recording => recording.call_id === call.id && recording.playable
  )
}

function recordingCount(call: CallRecord): number {
  return recordingCatalogState.data.filter(recording => recording.call_id === call.id).length
}

async function markActivityRead(activity: DashboardActivity): Promise<void> {
  activityMutationError.value = ''
  if (activity.kind === 'message') {
    const marked = await markThreadRead(activity.thread)
    if (!marked) {
      activityMutationError.value =
        threadReadErrors[activity.thread.key] || t('runtime.requestFailed')
    }
    return
  }

  try {
    await markMissedCallRead(activity.call)
  } catch (error) {
    activityMutationError.value = t('calls.markReadFailed', {
      error: error instanceof Error ? error.message : String(error)
    })
  }
}

async function markActivityUnread(activity: DashboardActivity): Promise<void> {
  activityMutationError.value = ''
  try {
    if (activity.kind === 'message') {
      await markThreadsUnread([activity.thread])
      if (selectionKey.value === activity.key) {
        await router.replace({ name: 'dashboard' })
      }
    } else {
      await markMissedCallUnread(activity.call)
    }
  } catch (error) {
    activityMutationError.value =
      error instanceof Error ? error.message : t('runtime.requestFailed')
  }
}

function toggleActivityRead(activity: DashboardActivity): Promise<void> {
  return activityIsUnread(activity)
    ? markActivityRead(activity)
    : markActivityUnread(activity)
}

async function removeActivity(activity: DashboardActivity): Promise<void> {
  const callRecordings =
    activity.kind === 'call' ? recordingCount(activity.call) : 0
  const confirmed = await requestConfirmation({
    title:
      activity.kind === 'message'
        ? t('messages.deleteConfirmTitle')
        : t('calls.deleteConfirmTitle'),
    message:
      activity.kind === 'message'
        ? t('messages.deleteConfirmMessage', {
            name: threadName(activity.thread)
          })
        : callRecordings
          ? t('calls.deleteConfirmWithRecordings', {
              name: callName(activity.call),
              count: callRecordings
            })
          : t('calls.deleteConfirmMessage', {
              name: callName(activity.call)
            }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return

  deletingActivityKey.value = activity.key
  activityMutationError.value = ''
  try {
    if (activity.kind === 'message') {
      await deleteMessageThread(activity.thread)
    } else {
      await deleteCall(activity.call)
      forgetCallRecordings(activity.call.id)
    }
    if (selectionKey.value === activity.key) {
      await router.replace({ name: 'dashboard' })
    }
  } catch (error) {
    activityMutationError.value =
      error instanceof Error
        ? error.message
        : activity.kind === 'message'
          ? t('messages.deleteFailed')
          : t('calls.deleteFailed')
  } finally {
    deletingActivityKey.value = ''
  }
}

async function batchSetRead(read: boolean): Promise<void> {
  if (batchBusy.value || batchActivities.value.length === 0) return
  const threads = batchMessageActivities.value.map(activity => activity.thread)
  const calls = batchMissedCallActivities.value.map(activity => activity.call)
  if (threads.length === 0 && calls.length === 0) return
  batchBusy.value = true
  activityMutationError.value = ''
  try {
    await Promise.all([
      read ? markThreadsRead(threads) : markThreadsUnread(threads),
      updateMissedCallsReadState(calls, read)
    ])
    if (
      !read &&
      batchMessageActivities.value.some(activity => activity.key === selectionKey.value)
    ) {
      await router.replace({ name: 'dashboard' })
    }
  } catch (error) {
    activityMutationError.value =
      error instanceof Error ? error.message : t('runtime.requestFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchSetFavorite(favorite: boolean): Promise<void> {
  if (batchBusy.value || batchActivities.value.length === 0) return
  batchBusy.value = true
  activityMutationError.value = ''
  try {
    await Promise.all([
      setThreadsFavorite(
        batchMessageActivities.value.map(activity => activity.thread),
        favorite
      ),
      setCallsFavorite(
        batchCallActivities.value.map(activity => activity.call),
        favorite
      )
    ])
  } catch (error) {
    activityMutationError.value =
      error instanceof Error ? error.message : t('common.favoriteFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchDelete(): Promise<void> {
  const selected = batchActivities.value
  if (batchBusy.value || selected.length === 0) return
  const calls = selected
    .filter(
      (activity): activity is Extract<DashboardActivity, { kind: 'call' }> =>
        activity.kind === 'call'
    )
    .map(activity => activity.call)
  const threads = selected
    .filter(
      (activity): activity is Extract<DashboardActivity, { kind: 'message' }> =>
        activity.kind === 'message'
    )
    .map(activity => activity.thread)
  const recordings = calls.reduce((total, call) => total + recordingCount(call), 0)
  const confirmed = await requestConfirmation({
    title: t('dashboard.deleteSelectedTitle'),
    message: recordings
      ? t('dashboard.deleteSelectedWithRecordings', {
          count: selected.length,
          recordings
        })
      : t('dashboard.deleteSelectedMessage', { count: selected.length }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  batchBusy.value = true
  activityMutationError.value = ''
  try {
    const deletedKeys = new Set(selected.map(activity => activity.key))
    await Promise.all([
      deleteMessageThreads(threads),
      deleteCalls(calls)
    ])
    for (const call of calls) forgetCallRecordings(call.id)
    if (deletedKeys.has(selectionKey.value)) {
      await router.replace({ name: 'dashboard' })
    }
    selection.exit()
  } catch (error) {
    activityMutationError.value =
      error instanceof Error ? error.message : t('runtime.requestFailed')
  } finally {
    batchBusy.value = false
  }
}

function onSelectionKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selection.active.value) selection.exit()
}

function selectOverview(): void {
  composingMessage.value = false
  void router.push({ name: 'dashboard', query: { item: 'overview' } })
}

function selectActivity(activity: DashboardActivity): void {
  composingMessage.value = false
  void router.push({ name: 'dashboard', query: { item: activity.key } })
}

function backToList(): void {
  if (route.query.from === 'settings') {
    void router.push({ name: 'settings' })
    return
  }
  void router.push({ name: 'dashboard' })
}

function callNumber(number: string, label = '', contextLineKey = ''): void {
  if (dialUnavailable.value) return
  openDialer(number, label, contextLineKey)
}

function startMessage(number: string, name = '', contextLineKey = ''): void {
  if (messageUnavailable.value) return
  messageComposeRecipient.value = number
  messageComposeRecipientName.value = name
  messageComposeLineKey.value = contextLineKey
  composingMessage.value = true
}

function deviceFor(line: LineSummary) {
  return devicesResource.data.find(device => device.imei === line.device_imei)
}

function openLineSettings(line: LineSummary): void {
  if (line.module_only) return
  if (line.id) selectDeviceConfiguration(line.id)
  void router.push({ name: 'settings', params: { section: 'devices' } })
}

function callContact(contact: Contact): void {
  const number = primaryPhoneDestination(contact.phones)
  if (number) callNumber(number, contact.display_name)
}

function messageContact(contact: Contact): void {
  const number = primaryPhoneDestination(contact.phones)
  if (number) startMessage(number, contact.display_name)
}

function composeMessage(): void {
  startMessage('')
}

function closeMessageComposer(): void {
  composingMessage.value = false
}

function finishMessageComposer(threadKey?: string): void {
  composingMessage.value = false
  void loadThreads(true)
  if (threadKey) {
    void router.replace({
      name: 'dashboard',
      query: { item: `message:${messageThreadReference(threadKey)}` }
    })
  }
}

function messageFromCall(request: {
  number: string
  name: string
  contextLineKey: string
}): void {
  startMessage(request.number, request.name, request.contextLineKey)
}

function createContact(): void {
  if (!contactEditingAvailable) return
  contactEditorError.value = ''
  contactEditorOpen.value = true
}

async function saveNewContact(input: ContactInput): Promise<void> {
  contactSaving.value = true
  contactEditorError.value = ''
  try {
    await saveContact(input)
    contactEditorOpen.value = false
    showSuccess(t('common.saved'))
  } catch (error) {
    contactEditorError.value =
      error instanceof Error ? error.message : t('contacts.saveFailed')
  } finally {
    contactSaving.value = false
  }
}

function retryActivities(): void {
  if (callsResource.status === 'error') void loadCalls(true)
  if (threadsResource.status === 'error') void loadThreads(true)
}

function loadDashboard(): void {
  void waitForInitialLoad([
    () => loadBootstrap(),
    () => loadCalls(),
    () => loadThreads(),
    () => loadContacts(),
    () => loadDevices(),
    () => loadRecordingEntries(),
    () => loadNetwork()
  ])
}

watch(activities, items => selection.reconcile(items))

onMounted(() => {
  window.addEventListener('keydown', onSelectionKeydown)
  loadDashboard()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
})
</script>

<template>
  <WorkspaceMasterDetail
    class="dashboard-workspace"
    :has-selection="hasSelection"
    :batch-selecting="selecting"
    list-class="dashboard-activity-pane"
  >
    <template #list>
      <WorkspaceListHeader
        :title="t('dashboard.activity')"
        :count="activityLoading ? undefined : activities.length"
        compact-mode="hidden"
      >
        <template #actions>
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="activities.length === 0"
            @toggle="selection.toggleMode"
          />
          <button
            class="icon-button dashboard-activity-action"
            type="button"
            :disabled="Boolean(messageUnavailable)"
            :title="messageUnavailable || t('dashboard.newMessage')"
            :aria-label="t('dashboard.newMessage')"
            @click="composeMessage"
          >
            <MessageSquareText :size="19" />
          </button>
          <button
            v-if="contactEditingAvailable"
            class="icon-button dashboard-activity-action"
            type="button"
            :title="t('contacts.new')"
            :aria-label="t('contacts.new')"
            @click="createContact"
          >
            <UserPlus :size="19" />
          </button>
        </template>
      </WorkspaceListHeader>

      <button
        class="list-item dashboard-overview-row"
        :class="{ 'is-selected': overviewSelected }"
        type="button"
        @click="selectOverview"
      >
        <span class="dashboard-activity-icon"><House :size="18" /></span>
        <span class="list-item__content">
          <strong>{{ t('dashboard.overview') }}</strong>
          <small>
            {{
              t('dashboard.modulesOnline', {
                online: onlineModules,
                total: presentModules.length
              })
            }}
          </small>
        </span>
        <ChevronRight :size="16" />
      </button>

      <div class="dashboard-list-label">{{ t('dashboard.recentActivity') }}</div>

      <LoadingSkeletonBoundary :loading="activityLoading">
        <template #skeleton>
          <ListSkeleton :label="t('dashboard.loadingActivities')" />
        </template>
        <StatePanel
          v-if="activities.length === 0 && activityErrors.length > 0"
          :state="activityRetryable ? 'error' : 'forbidden'"
          :title="t('dashboard.loadActivitiesFailed')"
          :detail="activityErrors.join('；')"
          :retryable="activityRetryable"
          @retry="retryActivities"
        />
        <StatePanel
          v-else-if="activities.length === 0"
          state="empty"
          :title="t('dashboard.noActivities')"
        />
        <div v-else class="item-list dashboard-activity-list">
        <div v-if="activityMutationError" class="dashboard-inline-error" role="alert">
          <AlertCircle :size="15" />
          <span>{{ activityMutationError }}</span>
        </div>
        <div v-if="activityErrors.length > 0" class="dashboard-inline-error" role="alert">
          <AlertCircle :size="15" />
          <span>{{ activityErrors.join('；') }}</span>
          <button v-if="activityRetryable" type="button" @click="retryActivities">
            {{ t('common.retry') }}
          </button>
        </div>
        <SelectableListRow
          v-for="activity in activities"
          :key="activity.key"
          :active="selecting"
          :selected="selection.has(activity)"
          :label="
            t('common.selectItem', {
              name:
                activity.kind === 'message'
                  ? threadName(activity.thread)
                  : callName(activity.call)
            })
          "
          @toggle="selection.toggle(activity)"
        >
          <SwipeActionRow
            :can-read="activityHasReadState(activity)"
            :read-mode="activityIsUnread(activity) ? 'read' : 'unread'"
            :read-label="
              activityIsUnread(activity)
                ? t('common.markRead')
                : t('common.markUnread')
            "
            :delete-label="t('common.delete')"
            :disabled="selecting || Boolean(deletingActivityKey)"
            @read="toggleActivityRead(activity)"
            @delete="removeActivity(activity)"
          >
            <MessageThreadListItem
              v-if="activity.kind === 'message'"
              :thread="activity.thread"
              :name="threadName(activity.thread)"
              :peer="displayPhoneNumber(activity.thread.peer, activity.thread.line_id)"
              :avatar="avatarForNumber(activity.thread.peer)"
              :line="lineTagLine(lineForThread(activity.thread), activity.thread.line_id)"
              :line-fallback="threadLineFallback(activity.thread)"
              :selected="selectionKey === activity.key"
              :arriving="recentIncomingThreadKeys[activity.thread.key]"
              :favorite-interactive="false"
              @select="selectActivity(activity)"
            />
            <CallHistoryListItem
              v-else
              :call="activity.call"
              :name="callName(activity.call)"
              :number="displayPhoneNumber(activity.call.remote_number, activity.call.line_id)"
              :avatar="avatarForNumber(activity.call.remote_number)"
              :line="lineTagLine(lineForCall(activity.call), activity.call.line_id)"
              :line-fallback="callLineFallback(activity.call)"
              :selected="selectionKey === activity.key"
              :has-recording="hasPlayableRecording(activity.call)"
              @select="selectActivity(activity)"
            />
          </SwipeActionRow>
        </SelectableListRow>
        </div>
      </LoadingSkeletonBoundary>
      <BatchActionBar
        v-if="selecting"
        :selected="selectionCount"
        :total="activities.length"
        :selected-label="t('common.selectedCount', { count: selectionCount })"
        :select-all-label="t('common.selectAll')"
        :clear-all-label="t('common.clearAll')"
        :done-label="t('common.done')"
        :busy="batchBusy"
        @select-all="selection.selectAll(activities)"
        @done="selection.exit"
      >
        <button
          v-if="batchHasReadState"
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
          v-if="batchActivities.length > 0"
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
          v-if="batchActivities.length > 0"
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
      <article
        v-if="initialLoading || skeletonPreviewEnabled"
        class="detail-pane dashboard-detail-pane"
      >
        <LoadingSkeletonBoundary :loading="initialLoading">
          <template #skeleton>
            <WorkspaceDetailSkeleton
              :label="t('dashboard.loadingActivityDetail')"
              shape="dashboard"
            />
          </template>
        </LoadingSkeletonBoundary>
      </article>

      <template v-else>
        <MessagesView
          v-if="composingMessage"
          embedded-compose
          :initial-recipient="messageComposeRecipient"
          :initial-recipient-name="messageComposeRecipientName"
          :context-line-key="messageComposeLineKey"
          @close="closeMessageComposer"
          @sent="finishMessageComposer"
        />

        <MessagesView
          v-else-if="selectedThread"
          :embedded-thread-key="selectedThread.key"
          @close="backToList"
        />

        <CallsView
          v-else-if="selectedCall"
          :embedded-call-id="selectedCall.id"
          @close="backToList"
          @message="messageFromCall"
        />

        <article v-else class="detail-pane dashboard-detail-pane">
          <template v-if="overviewSelected">
            <div class="dashboard-detail-scroll">
          <button
            class="icon-button mobile-back dashboard-overview-back"
            :class="{ 'is-shell-managed': route.query.from === 'settings' }"
            type="button"
            :title="backLabel"
            :aria-label="backLabel"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <section
            class="dashboard-summary-grid"
            :aria-label="t('dashboard.communicationSummary')"
          >
            <RouterLink
              class="dashboard-summary-card is-message"
              :to="{ name: 'messages', query: { filter: 'unread' } }"
            >
              <span class="dashboard-summary-icon">
                <MessageSquareText :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ unreadMessages }}</span>
              <span class="dashboard-summary-label">
                {{ t('dashboard.unreadMessages') }}
              </span>
              <ChevronRight :size="17" />
            </RouterLink>
            <RouterLink
              class="dashboard-summary-card is-missed"
              :to="{ name: 'calls', query: { filter: 'missed' } }"
            >
              <span class="dashboard-summary-icon">
                <PhoneMissed :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ missedCalls }}</span>
              <span class="dashboard-summary-label">
                {{ t('dashboard.missedCalls') }}
              </span>
              <ChevronRight :size="17" />
            </RouterLink>
            <RouterLink
              class="dashboard-summary-card is-lines"
              :to="{ name: 'settings', params: { section: 'devices' } }"
            >
              <span class="dashboard-summary-icon">
                <RadioTower :size="20" />
              </span>
              <span class="dashboard-summary-value">
                {{ onlineModules }}<small>/{{ presentModules.length }}</small>
              </span>
              <span class="dashboard-summary-label">
                {{ t('dashboard.onlineModules') }}
              </span>
              <ChevronRight :size="17" />
            </RouterLink>
            <RouterLink class="dashboard-summary-card is-traffic" :to="{ name: 'traffic' }">
              <span class="dashboard-summary-icon">
                <ChartNoAxesCombined :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ formatBytes(monthTraffic) }}</span>
              <span class="dashboard-summary-label">
                {{ t('dashboard.monthTraffic') }}
              </span>
              <ChevronRight :size="17" />
            </RouterLink>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-contacts-title">
            <header>
              <div>
                <h3 id="dashboard-contacts-title">
                  {{ t('dashboard.favoriteContacts') }}
                </h3>
              </div>
              <RouterLink :to="{ name: 'contacts' }">
                {{ t('dashboard.allContacts') }}
                <ChevronRight :size="15" />
              </RouterLink>
            </header>
            <LoadingSkeletonBoundary
              :loading="contactsResource.status === 'loading' || contactsResource.status === 'idle'"
            >
              <template #skeleton>
                <SectionSkeleton
                  :label="t('contacts.loading')"
                  variant="rows"
                  :rows="3"
                />
              </template>
              <div
                v-if="contactsResource.status === 'error' || contactsResource.status === 'forbidden'"
                class="dashboard-section-state dashboard-section-state--error"
                role="alert"
              >
              <AlertCircle :size="17" />
              <span>{{ contactsResource.error || t('contacts.loadFailed') }}</span>
              <button
                v-if="contactsResource.status === 'error'"
                type="button"
                @click="loadContacts(true)"
              >
                {{ t('common.retry') }}
              </button>
              </div>
              <div
                v-else-if="favoriteContacts.length === 0"
                class="dashboard-section-state dashboard-favorites-empty"
              >
              <Star :size="17" />
              {{ t('dashboard.noFavoriteContacts') }}
              </div>
              <div v-else class="dashboard-detail-list">
              <div
                v-for="contact in favoriteContacts"
                :key="contact.id"
                class="dashboard-contact-row"
              >
                <RouterLink :to="{ name: 'contacts', params: { contactId: contact.id } }">
                  <BaseAvatar
                    class="dashboard-favorite-avatar"
                    :name="contact.display_name"
                    :src="contact.avatar"
                  />
                  <span class="dashboard-contact-identity">
                    <strong>{{ contact.display_name }}</strong>
                    <small>
                      {{ primaryPhone(contact.phones) || t('contacts.noNumber') }}
                    </small>
                  </span>
                </RouterLink>
                <span class="dashboard-contact-actions">
                  <button
                    class="icon-button"
                    type="button"
                    :disabled="!primaryPhone(contact.phones) || Boolean(dialUnavailable)"
                    :title="dialUnavailable || t('dashboard.dial')"
                    :aria-label="t('dashboard.callContact', { name: contact.display_name })"
                    @click="callContact(contact)"
                  >
                    <Phone :size="17" />
                  </button>
                  <button
                    class="icon-button"
                    type="button"
                    :disabled="!primaryPhone(contact.phones) || Boolean(messageUnavailable)"
                    :title="messageUnavailable || t('dashboard.message')"
                    :aria-label="t('dashboard.messageContact', { name: contact.display_name })"
                    @click="messageContact(contact)"
                  >
                    <MessageSquareText :size="17" />
                  </button>
                </span>
              </div>
              </div>
            </LoadingSkeletonBoundary>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-traffic-title">
            <header>
              <div>
                <h3 id="dashboard-traffic-title">{{ t('shell.traffic') }}</h3>
              </div>
              <RouterLink :to="{ name: 'traffic' }">
                {{ t('dashboard.viewTraffic') }}
                <ChevronRight :size="15" />
              </RouterLink>
            </header>
            <LoadingSkeletonBoundary
              :loading="networkState.status === 'loading' || networkState.status === 'idle'"
            >
              <template #skeleton>
                <SectionSkeleton
                  :label="t('dashboard.loadingTraffic')"
                  variant="facts"
                  :rows="4"
                />
              </template>
              <div
                v-if="networkState.status === 'error' || networkState.status === 'forbidden'"
                class="dashboard-section-state dashboard-section-state--error"
                role="alert"
              >
              <AlertCircle :size="17" />
              <span>{{ networkState.error || t('dashboard.loadTrafficFailed') }}</span>
              <button
                v-if="networkState.status === 'error'"
                type="button"
                @click="loadNetwork(true)"
              >
                {{ t('common.retry') }}
              </button>
              </div>
              <div
                v-else-if="!trafficSnapshot?.available"
                class="dashboard-section-state"
              >
              <ChartNoAxesCombined :size="17" />
              {{ t('dashboard.trafficUnavailable') }}
              </div>
              <TrafficSummary
                v-else
                :today-bytes="todayTraffic"
                :month-bytes="monthTraffic"
                :connected-lines="connectedNetworkLines"
                :total-lines="trafficSnapshot.lines.length"
                :running-proxies="runningProxies"
                :total-proxies="trafficSnapshot.proxies.length"
              />
            </LoadingSkeletonBoundary>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-lines-title">
            <header>
              <div>
                <h3 id="dashboard-lines-title">{{ t('dashboard.moduleStatus') }}</h3>
                <span class="dashboard-section-metrics">
                  <span><b>{{ presentModules.length }}</b> {{ t('device.modules') }}</span>
                  <span><b>{{ onlineModules }}</b> {{ t('dashboard.online') }}</span>
                  <span><b>{{ callReadyLines }}</b> {{ t('dashboard.callReady') }}</span>
                  <span><b>{{ messageReadyLines }}</b> {{ t('dashboard.messageReady') }}</span>
                </span>
              </div>
              <RouterLink :to="{ name: 'settings', params: { section: 'devices' } }">
                {{ t('dashboard.manageDevices') }}
                <ChevronRight :size="15" />
              </RouterLink>
            </header>
            <LoadingSkeletonBoundary
              :loading="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
            >
              <template #skeleton>
                <SectionSkeleton
                  :label="t('dashboard.loadingLines')"
                  variant="cards"
                  :rows="3"
                />
              </template>
              <div
                v-if="bootstrapResource.status === 'error' || bootstrapResource.status === 'forbidden'"
                class="dashboard-section-state dashboard-section-state--error"
                role="alert"
              >
              <AlertCircle :size="17" />
              <span>{{ bootstrapResource.error || t('dashboard.loadLinesFailed') }}</span>
              <button
                v-if="bootstrapResource.status === 'error'"
                type="button"
                @click="loadBootstrap(true)"
              >
                {{ t('common.retry') }}
              </button>
              </div>
              <div v-else-if="moduleLines.length === 0" class="dashboard-section-state">
                <Inbox :size="17" />
                {{ t('dashboard.noLines') }}
              </div>
              <div v-else class="dashboard-module-grid">
              <ModuleCard
                v-for="line in moduleLines"
                :key="lineKey(line)"
                :line="line"
                :device="deviceFor(line)"
                :runtime="networkRuntime(line)"
                :selectable="!line.module_only"
                :default-line="lineKey(line) === defaultLineID"
                @select="openLineSettings(line)"
              />
              </div>
            </LoadingSkeletonBoundary>
          </section>
            </div>
          </template>

          <WorkspaceDetailSkeleton
            v-else-if="activityLoading"
            :label="t('dashboard.loadingActivityDetail')"
            shape="communication"
          />
          <StatePanel
            v-else
            state="empty"
            :title="t('dashboard.activityMissing')"
            :detail="t('dashboard.activityMissingDetail')"
          />
        </article>
      </template>
    </template>
  </WorkspaceMasterDetail>

  <ContactEditor
    :open="contactEditorOpen"
    :lines="contactLines"
    :default-line-id="defaultLineID"
    :saving="contactSaving"
    :error="contactEditorError"
    @close="contactEditorOpen = false"
    @save="saveNewContact"
  />
</template>

<style scoped>
.dashboard-workspace {
  grid-template-columns: minmax(300px, 344px) minmax(0, 1fr);
}

.dashboard-activity-pane {
  background: var(--surface);
}

.dashboard-overview-row {
  min-height: 70px;
  padding-inline: 16px;
}

.dashboard-list-label {
  min-height: 40px;
  padding: 13px 16px 9px;
  color: var(--muted);
  font-size: 12px;
  text-transform: none;
}

.dashboard-overview-row .list-item__content small {
  font-size: 12px;
}

.dashboard-inline-error {
  min-height: 48px;
  padding: 9px 14px;
  font-size: 12px;
}

.dashboard-detail-scroll {
  padding: 20px 24px 28px;
  container-type: inline-size;
}

.dashboard-overview-back {
  margin-bottom: 12px;
}

.dashboard-summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.dashboard-summary-card {
  display: grid;
  min-width: 0;
  min-height: 106px;
  align-items: center;
  grid-template-columns: 40px minmax(0, 1fr) auto;
  grid-template-rows: auto auto;
  column-gap: 12px;
  padding: 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
  transition:
    border-color 150ms ease,
    background 150ms ease;
}

.dashboard-summary-card:hover {
  background: var(--surface-hover);
  border-color: var(--border-strong);
}

.dashboard-summary-icon {
  display: inline-grid;
  width: 40px;
  height: 40px;
  grid-row: 1 / 3;
  place-items: center;
  color: var(--blue);
  background: var(--blue-soft);
  border-radius: 6px;
}

.dashboard-summary-card.is-missed .dashboard-summary-icon {
  color: var(--danger);
  background: var(--danger-soft);
}

.dashboard-summary-card.is-lines .dashboard-summary-icon {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.dashboard-summary-card.is-traffic .dashboard-summary-icon {
  color: var(--warning-strong);
  background: var(--warning-soft);
}

.dashboard-summary-value {
  align-self: end;
  overflow: hidden;
  font-size: 26px;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  line-height: 1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-summary-value small {
  color: var(--muted);
  font-size: 15px;
  font-weight: 600;
}

.dashboard-summary-label {
  align-self: start;
  color: var(--muted);
  font-size: 13px;
}

.dashboard-summary-card > svg {
  grid-column: 3;
  grid-row: 1 / 3;
  color: var(--faint);
}

.dashboard-detail-section {
  margin-top: 24px;
  background: transparent;
  border: 0;
}

.dashboard-detail-section > header {
  min-height: 52px;
  padding: 0 0 12px;
  border-bottom: 0;
}

.dashboard-detail-section > header > div {
  display: flex;
  min-width: 0;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px 18px;
}

.dashboard-detail-section h3 {
  font-size: 16px;
}

.dashboard-section-metrics {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 12px;
  color: var(--muted);
  font-size: 12px;
}

.dashboard-section-metrics b {
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.dashboard-detail-section > header a {
  min-height: 36px;
  font-size: 13px;
  white-space: nowrap;
}

.dashboard-module-grid,
.dashboard-detail-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 420px));
  justify-content: start;
  gap: 12px;
}

.dashboard-module-grid > :deep(.module-card),
.dashboard-detail-list > .dashboard-contact-row {
  width: 100%;
  max-width: 420px;
}

.dashboard-detail-list {
  gap: 10px;
}

.dashboard-contact-row {
  min-height: 72px;
  padding: 10px 12px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.dashboard-contact-row:last-child {
  border-bottom: 1px solid var(--border);
}

.dashboard-contact-row > a > :deep(.dashboard-favorite-avatar) {
  display: inline-grid;
  min-width: 0;
  place-items: center;
  flex-direction: initial;
  gap: 0;
}

.dashboard-contact-row strong {
  font-size: 14px;
}

.dashboard-contact-row small {
  font-size: 12px;
}

.dashboard-contact-actions {
  gap: 4px;
}

.dashboard-contact-actions .icon-button {
  width: 38px;
  height: 38px;
  flex-basis: 38px;
}

.dashboard-section-state {
  min-height: 92px;
  padding: 18px;
  font-size: 13px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.dashboard-favorites-empty {
  min-height: 72px;
}

@container (max-width: 920px) {
  .dashboard-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 1100px) {
  .dashboard-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 860px) {
  .dashboard-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .dashboard-workspace > :deep(.workspace) {
    grid-area: 1 / 1;
    min-width: 0;
    min-height: 0;
  }

  .dashboard-detail-scroll {
    padding: 16px 16px 24px;
  }

  .dashboard-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }

  .dashboard-summary-card {
    min-height: 92px;
    grid-template-columns: 34px minmax(0, 1fr);
    column-gap: 9px;
    padding: 12px;
  }

  .dashboard-summary-icon {
    width: 34px;
    height: 34px;
  }

  .dashboard-summary-card > svg {
    display: none;
  }

  .dashboard-summary-value {
    font-size: 22px;
  }

  .dashboard-summary-label {
    font-size: 12px;
  }

  .dashboard-module-grid,
  .dashboard-detail-list {
    grid-template-columns: minmax(0, 1fr);
  }

  .dashboard-module-grid > :deep(.module-card),
  .dashboard-detail-list > .dashboard-contact-row {
    max-width: none;
  }
}

@media (max-width: 420px) {
  .dashboard-detail-scroll {
    padding-inline: 12px;
  }
}
</style>
