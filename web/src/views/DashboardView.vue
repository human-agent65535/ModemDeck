<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertCircle,
  ArrowLeft,
  ChartNoAxesCombined,
  ChevronRight,
  House,
  Inbox,
  LoaderCircle,
  MessageSquareText,
  Phone,
  PhoneMissed,
  RadioTower,
  Star,
  UserPlus
} from '@lucide/vue'
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
import StatePanel from '../components/StatePanel.vue'
import TrafficSummary from '../components/TrafficSummary.vue'
import { selectDeviceConfiguration } from '../state/deviceConfiguration'
import { loadNetwork, networkState } from '../state/network'
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
  devicesResource,
  displayModuleLines,
  lineHasCallControl,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadContacts,
  loadDevices,
  loadThreads,
  presentModuleLines,
  recentIncomingThreadKeys,
  saveContact,
  threadsResource
} from '../state/workspace'
import {
  isMessagingServiceReady,
  isRegisteredNetwork,
  isVoiceServiceReady
} from '../utils/operatorNetwork'
import { primaryPhone } from '../utils/format'
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
const unreadMessages = computed(() =>
  threadsResource.data.reduce((total, thread) => total + thread.unread_count, 0)
)
const missedCalls = computed(
  () => callsResource.data.filter(call => call.missed && !call.read).length
)
const onlineModules = computed(
  () => presentModules.value.filter(line => isRegisteredNetwork(line)).length
)
const callReadyLines = computed(
  () =>
    lines.value.filter(
      line =>
        isVoiceServiceReady(line) &&
        lineHasCallControl(line)
    ).length
)
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
    key: `message:${thread.key}`,
    kind: 'message',
    timestamp: thread.last_timestamp,
    thread
  }))
  return calls
    .concat(messages)
    .sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp))
    .slice(0, 30)
})

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
    (callsResource.status === 'idle' || callsResource.status === 'loading') &&
    (threadsResource.status === 'idle' || threadsResource.status === 'loading') &&
    activities.value.length === 0
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
    call.remote_number
  )
}

function threadName(thread: MessageThread): string {
  return (
    thread.contact_name ||
    contactForNumber(thread.peer)?.display_name ||
    thread.peer
  )
}

function lineForCall(call: CallRecord): LineSummary | undefined {
  return lines.value.find(line => lineKey(line) === call.line_id)
}

function lineForThread(thread: MessageThread): LineSummary | undefined {
  return lines.value.find(line => lineKey(line) === thread.line_id)
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
  const number = primaryPhone(contact.phones)
  if (number) callNumber(number, contact.display_name)
}

function messageContact(contact: Contact): void {
  const number = primaryPhone(contact.phones)
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
      query: { item: `message:${threadKey}` }
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
  void Promise.all([
    loadBootstrap(),
    loadCalls(),
    loadThreads(),
    loadContacts(),
    loadDevices(),
    loadNetwork()
  ])
}

onMounted(() => {
  loadDashboard()
})
</script>

<template>
  <section class="workspace dashboard-workspace" :class="{ 'has-selection': hasSelection }">
    <aside class="list-pane dashboard-activity-pane">
      <header class="pane-header">
        <div>
          <h1>{{ t('dashboard.activity') }}</h1>
          <span v-if="!activityLoading">{{ activities.length }}</span>
        </div>
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
      </header>

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

      <StatePanel
        v-if="activityLoading"
        state="loading"
        :title="t('dashboard.loadingActivities')"
      />
      <StatePanel
        v-else-if="activities.length === 0 && activityErrors.length > 0"
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
        <div v-if="activityErrors.length > 0" class="dashboard-inline-error" role="alert">
          <AlertCircle :size="15" />
          <span>{{ activityErrors.join('；') }}</span>
          <button v-if="activityRetryable" type="button" @click="retryActivities">
            {{ t('common.retry') }}
          </button>
        </div>
        <template v-for="activity in activities" :key="activity.key">
          <MessageThreadListItem
            v-if="activity.kind === 'message'"
            :thread="activity.thread"
            :name="threadName(activity.thread)"
            :avatar="avatarForNumber(activity.thread.peer)"
            :line="lineTagLine(lineForThread(activity.thread), activity.thread.line_id)"
            :line-fallback="threadLineFallback(activity.thread)"
            :selected="selectionKey === activity.key"
            :arriving="recentIncomingThreadKeys[activity.thread.key]"
            @select="selectActivity(activity)"
          />
          <CallHistoryListItem
            v-else
            :call="activity.call"
            :name="callName(activity.call)"
            :avatar="avatarForNumber(activity.call.remote_number)"
            :line="lineTagLine(lineForCall(activity.call), activity.call.line_id)"
            :line-fallback="callLineFallback(activity.call)"
            :selected="selectionKey === activity.key"
            @select="selectActivity(activity)"
          />
        </template>
      </div>
    </aside>

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
            <div
              v-if="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
              class="dashboard-section-state"
            >
              <LoaderCircle class="spin" :size="17" />
              {{ t('dashboard.loadingLines') }}
            </div>
            <div
              v-else-if="bootstrapResource.status === 'error' || bootstrapResource.status === 'forbidden'"
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
            <div
              v-if="networkState.status === 'loading' || networkState.status === 'idle'"
              class="dashboard-section-state"
            >
              <LoaderCircle class="spin" :size="17" />
              {{ t('dashboard.loadingTraffic') }}
            </div>
            <div
              v-else-if="networkState.status === 'error' || networkState.status === 'forbidden'"
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
            <div
              v-if="contactsResource.status === 'loading' || contactsResource.status === 'idle'"
              class="dashboard-section-state"
            >
              <LoaderCircle class="spin" :size="17" />
              {{ t('contacts.loading') }}
            </div>
            <div
              v-else-if="contactsResource.status === 'error' || contactsResource.status === 'forbidden'"
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
          </section>
        </div>
      </template>

      <StatePanel
        v-else-if="activityLoading"
        state="loading"
        :title="t('dashboard.loadingActivityDetail')"
      />
      <StatePanel
        v-else
        state="empty"
        :title="t('dashboard.activityMissing')"
        :detail="t('dashboard.activityMissingDetail')"
      />
    </article>

    <ContactEditor
      :open="contactEditorOpen"
      :lines="contactLines"
      :saving="contactSaving"
      :error="contactEditorError"
      @close="contactEditorOpen = false"
      @save="saveNewContact"
    />
  </section>
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

.dashboard-activity-pane > .pane-header {
  gap: 6px;
}

.dashboard-activity-pane > .pane-header > div {
  margin-right: auto;
}

.dashboard-activity-action {
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
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
  color: #7a4b00;
  background: #fff2d6;
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

@media (max-width: 1180px) {
  .dashboard-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 860px) {
  .dashboard-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .dashboard-detail-scroll {
    padding: 16px 16px 24px;
  }
}

@media (max-width: 640px) {
  .dashboard-activity-pane > .pane-header {
    justify-content: flex-end;
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

@media (max-width: 390px) {
  .dashboard-detail-scroll {
    padding-inline: 12px;
  }
}
</style>
