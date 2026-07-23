<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertCircle,
  ArrowLeft,
  ChevronRight,
  House,
  Inbox,
  LoaderCircle,
  MessageSquareText,
  Phone,
  PhoneIncoming,
  PhoneMissed,
  PhoneOutgoing,
  RadioTower,
  Settings,
  Users
} from '@lucide/vue'
import type { CallRecord, Contact, LineSummary, MessageThread } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import LineTag from '../components/LineTag.vue'
import ModuleCard from '../components/ModuleCard.vue'
import StatePanel from '../components/StatePanel.vue'
import { selectDeviceConfiguration } from '../state/deviceConfiguration'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  callsResource,
  capabilityReason,
  contactForNumber,
  contactsResource,
  devicesResource,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadContacts,
  loadDevices,
  loadThreads,
  threadsResource
} from '../state/workspace'
import {
  formatDateTime,
  formatDuration,
  formatRelativeDate,
  primaryPhone
} from '../utils/format'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine
} from '../utils/lineIdentity'

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

const lines = computed(() => bootstrapResource.data?.lines || [])
const lineLookup = computed(() => createLineLookup(lines.value))
const defaultDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
)
const selectionKey = computed(() =>
  typeof route.query.item === 'string' ? route.query.item : ''
)
const hasSelection = computed(() => Boolean(selectionKey.value))
const overviewSelected = computed(
  () => !selectionKey.value || selectionKey.value === 'overview'
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const messageUnavailable = computed(() => capabilityReason('message'))
const unreadMessages = computed(() =>
  threadsResource.data.reduce((total, thread) => total + thread.unread_count, 0)
)
const missedCalls = computed(
  () => callsResource.data.filter(call => call.missed).length
)
const onlineLines = computed(
  () =>
    lines.value.filter(line =>
      ['registered', 'connected'].includes((line.state || '').toLocaleLowerCase())
    ).length
)
const callReadyLines = computed(
  () =>
    lines.value.filter(
      line => line.capabilities?.dial === true || line.capabilities?.voice === true
    ).length
)
const messageReadyLines = computed(
  () =>
    lines.value.filter(
      line =>
        line.capabilities?.message === true ||
        line.capabilities?.messaging === true
    ).length
)
const attentionCount = computed(() => unreadMessages.value + missedCalls.value)

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
      ? callsResource.error || '无法载入通话'
      : '',
    threadsResource.status === 'error' || threadsResource.status === 'forbidden'
      ? threadsResource.error || '无法载入消息'
      : ''
  ].filter(Boolean)
)
const activityRetryable = computed(
  () => callsResource.status === 'error' || threadsResource.status === 'error'
)

const quickContacts = computed(() => {
  const selected: Contact[] = []
  const selectedIDs = new Set<string>()
  const addNumber = (number: string): void => {
    const contact = contactForNumber(number)
    if (!contact || selectedIDs.has(contact.id)) return
    selectedIDs.add(contact.id)
    selected.push(contact)
  }

  activities.value.forEach(activity => {
    addNumber(
      activity.kind === 'call'
        ? activity.call.remote_number
        : activity.thread.peer
    )
  })
  contactsResource.data
    .slice()
    .sort((a, b) => a.display_name.localeCompare(b.display_name))
    .forEach(contact => {
      if (selected.length >= 6 || selectedIDs.has(contact.id)) return
      selectedIDs.add(contact.id)
      selected.push(contact)
    })
  return selected.slice(0, 6)
})

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
  return findLine(lineLookup.value, call.device_id)
}

function lineForThread(thread: MessageThread): LineSummary | undefined {
  return findLine(lineLookup.value, thread.line_id, thread.iccid)
}

function lineForActivity(activity: DashboardActivity): LineSummary | undefined {
  return activity.kind === 'call'
    ? lineForCall(activity.call)
    : lineForThread(activity.thread)
}

function activityLineFallback(activity: DashboardActivity): string {
  const line = lineForActivity(activity)
  const identifiers =
    activity.kind === 'call'
      ? [activity.call.device_id]
      : [activity.thread.line_id, activity.thread.iccid]
  return lineTagFallback(
    line,
    lines.value,
    defaultDeviceIMEI.value,
    ...identifiers
  )
}

function activityLineTagLine(activity: DashboardActivity) {
  const line = lineForActivity(activity)
  return activity.kind === 'call'
    ? lineTagLine(line, activity.call.device_id)
    : lineTagLine(
        line,
        activity.thread.line_id,
        activity.thread.iccid
      )
}

function callLineFallback(call: CallRecord): string {
  return lineTagFallback(
    lineForCall(call),
    lines.value,
    defaultDeviceIMEI.value,
    call.device_id
  )
}

function threadLineFallback(thread: MessageThread): string {
  return lineTagFallback(
    lineForThread(thread),
    lines.value,
    defaultDeviceIMEI.value,
    thread.line_id,
    thread.iccid
  )
}

function activityName(activity: DashboardActivity): string {
  return activity.kind === 'call'
    ? callName(activity.call)
    : threadName(activity.thread)
}

function activityDescription(activity: DashboardActivity): string {
  if (activity.kind === 'message') {
    return activity.thread.last_content || activity.thread.peer
  }
  if (activity.call.missed) return `未接来电 · ${activity.call.remote_number}`
  return `${
    activity.call.direction === 'incoming' ? '呼入' : '呼出'
  } · ${activity.call.remote_number}`
}

function activityIcon(activity: DashboardActivity) {
  if (activity.kind === 'message') return MessageSquareText
  if (activity.call.missed) return PhoneMissed
  return activity.call.direction === 'incoming' ? PhoneIncoming : PhoneOutgoing
}

function selectOverview(): void {
  void router.push({ name: 'dashboard', query: { item: 'overview' } })
}

function selectActivity(activity: DashboardActivity): void {
  void router.push({ name: 'dashboard', query: { item: activity.key } })
}

function backToList(): void {
  void router.push({ name: 'dashboard' })
}

function callNumber(number: string, label = '', contextLineKey = ''): void {
  if (dialUnavailable.value) return
  openDialer(number, label, contextLineKey)
}

function startMessage(number: string, name = '', contextLineKey = ''): void {
  if (messageUnavailable.value) return
  void router.push({
    name: 'messages',
    query: {
      compose: number,
      ...(name ? { name } : {}),
      ...(contextLineKey ? { line: contextLineKey } : {})
    }
  })
}

function deviceFor(line: LineSummary) {
  return devicesResource.data.find(device => device.imei === line.device_imei)
}

function openLineSettings(line: LineSummary): void {
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
  if (messageUnavailable.value) return
  void router.push({ name: 'messages', query: { compose: '' } })
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
    loadDevices()
  ])
}

onMounted(loadDashboard)
</script>

<template>
  <section class="workspace dashboard-workspace" :class="{ 'has-selection': hasSelection }">
    <aside class="list-pane dashboard-activity-pane">
      <header class="pane-header">
        <div>
          <h1>活动</h1>
          <span v-if="!activityLoading">{{ activities.length }}</span>
        </div>
        <button
          class="icon-button dashboard-pane-action"
          type="button"
          :disabled="Boolean(messageUnavailable)"
          :title="messageUnavailable || '新消息'"
          aria-label="新消息"
          @click="composeMessage"
        >
          <MessageSquareText :size="19" />
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
          <strong>通信概览</strong>
          <small>
            {{ onlineLines }}/{{ lines.length }} 线路在线
            <template v-if="attentionCount"> · {{ attentionCount }} 待处理</template>
          </small>
        </span>
        <ChevronRight :size="16" />
      </button>

      <div class="dashboard-list-label">最近活动</div>

      <StatePanel
        v-if="activityLoading"
        state="loading"
        title="正在载入活动"
      />
      <StatePanel
        v-else-if="activities.length === 0 && activityErrors.length > 0"
        :state="activityRetryable ? 'error' : 'forbidden'"
        title="无法载入通信活动"
        :detail="activityErrors.join('；')"
        :retryable="activityRetryable"
        @retry="retryActivities"
      />
      <StatePanel
        v-else-if="activities.length === 0"
        state="empty"
        title="还没有通信活动"
      />
      <div v-else class="item-list dashboard-activity-list">
        <div v-if="activityErrors.length > 0" class="dashboard-inline-error" role="alert">
          <AlertCircle :size="15" />
          <span>{{ activityErrors.join('；') }}</span>
          <button v-if="activityRetryable" type="button" @click="retryActivities">重试</button>
        </div>
        <button
          v-for="activity in activities"
          :key="activity.key"
          class="list-item dashboard-activity-row"
          :class="{
            'is-selected': selectionKey === activity.key,
            'is-missed': activity.kind === 'call' && activity.call.missed
          }"
          type="button"
          @click="selectActivity(activity)"
        >
          <span class="dashboard-activity-icon">
            <component :is="activityIcon(activity)" :size="18" />
          </span>
          <span class="list-item__content">
            <span class="list-item__title">
              <strong>{{ activityName(activity) }}</strong>
              <time>{{ formatRelativeDate(activity.timestamp) }}</time>
            </span>
            <span class="list-item__preview">
              <span class="dashboard-activity-meta">
                <LineTag
                  :line="activityLineTagLine(activity)"
                  :fallback="activityLineFallback(activity)"
                />
                <small>{{ activityDescription(activity) }}</small>
              </span>
              <b
                v-if="activity.kind === 'message' && activity.thread.unread_count > 0"
              >
                {{ activity.thread.unread_count }}
              </b>
            </span>
          </span>
        </button>
      </div>
    </aside>

    <article class="detail-pane dashboard-detail-pane">
      <template v-if="overviewSelected">
        <header class="detail-header dashboard-detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            title="返回首页"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <span class="dashboard-detail-symbol"><RadioTower :size="21" /></span>
          <div class="detail-header__identity">
            <h2>通信工作台</h2>
            <span>{{ onlineLines }}/{{ lines.length }} 条线路在线</span>
          </div>
          <div class="detail-header__actions dashboard-header-actions">
            <button
              class="dashboard-command-button"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '新消息'"
              @click="composeMessage"
            >
              <MessageSquareText :size="17" />
              <span>新消息</span>
            </button>
            <button
              class="dashboard-command-button"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '拨号'"
              @click="openDialer()"
            >
              <Phone :size="17" />
              <span>拨号</span>
            </button>
            <RouterLink
              class="icon-button"
              :to="{ name: 'settings', params: { section: 'devices' } }"
              title="设备设置"
            >
              <Settings :size="18" />
            </RouterLink>
          </div>
        </header>

        <div class="dashboard-detail-scroll">
          <section class="dashboard-summary-grid" aria-label="通信汇总">
            <RouterLink class="dashboard-summary-card is-message" :to="{ name: 'messages' }">
              <span class="dashboard-summary-icon">
                <MessageSquareText :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ unreadMessages }}</span>
              <span class="dashboard-summary-label">未读消息</span>
              <ChevronRight :size="17" />
            </RouterLink>
            <RouterLink class="dashboard-summary-card is-missed" :to="{ name: 'calls' }">
              <span class="dashboard-summary-icon">
                <PhoneMissed :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ missedCalls }}</span>
              <span class="dashboard-summary-label">未接来电</span>
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
                {{ onlineLines }}<small>/{{ lines.length }}</small>
              </span>
              <span class="dashboard-summary-label">在线线路</span>
              <ChevronRight :size="17" />
            </RouterLink>
            <RouterLink class="dashboard-summary-card is-contacts" :to="{ name: 'contacts' }">
              <span class="dashboard-summary-icon">
                <Users :size="20" />
              </span>
              <span class="dashboard-summary-value">{{ contactsResource.data.length }}</span>
              <span class="dashboard-summary-label">联系人</span>
              <ChevronRight :size="17" />
            </RouterLink>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-lines-title">
            <header>
              <div>
                <h3 id="dashboard-lines-title">线路状态</h3>
                <span class="dashboard-section-metrics">
                  <span><b>{{ onlineLines }}</b> 在线</span>
                  <span><b>{{ callReadyLines }}</b> 可通话</span>
                  <span><b>{{ messageReadyLines }}</b> 可短信</span>
                </span>
              </div>
              <RouterLink :to="{ name: 'settings', params: { section: 'devices' } }">
                管理设备
                <ChevronRight :size="15" />
              </RouterLink>
            </header>
            <div
              v-if="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
              class="dashboard-section-state"
            >
              <LoaderCircle class="spin" :size="17" />
              正在载入线路
            </div>
            <div
              v-else-if="bootstrapResource.status === 'error' || bootstrapResource.status === 'forbidden'"
              class="dashboard-section-state dashboard-section-state--error"
              role="alert"
            >
              <AlertCircle :size="17" />
              <span>{{ bootstrapResource.error || '无法载入线路' }}</span>
              <button
                v-if="bootstrapResource.status === 'error'"
                type="button"
                @click="loadBootstrap(true)"
              >
                重试
              </button>
            </div>
            <div v-else-if="lines.length === 0" class="dashboard-section-state">
              <Inbox :size="17" />
              尚未发现线路
            </div>
            <div v-else class="dashboard-module-grid">
              <ModuleCard
                v-for="line in lines"
                :key="lineKey(line)"
                :line="line"
                :device="deviceFor(line)"
                :default-line="line.device_imei === defaultDeviceIMEI"
                @select="openLineSettings(line)"
              />
            </div>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-contacts-title">
            <header>
              <div>
                <h3 id="dashboard-contacts-title">常用联系人</h3>
              </div>
              <RouterLink :to="{ name: 'contacts' }">
                全部联系人
                <ChevronRight :size="15" />
              </RouterLink>
            </header>
            <div
              v-if="contactsResource.status === 'loading' || contactsResource.status === 'idle'"
              class="dashboard-section-state"
            >
              <LoaderCircle class="spin" :size="17" />
              正在载入联系人
            </div>
            <div
              v-else-if="contactsResource.status === 'error' || contactsResource.status === 'forbidden'"
              class="dashboard-section-state dashboard-section-state--error"
              role="alert"
            >
              <AlertCircle :size="17" />
              <span>{{ contactsResource.error || '无法载入联系人' }}</span>
              <button
                v-if="contactsResource.status === 'error'"
                type="button"
                @click="loadContacts(true)"
              >
                重试
              </button>
            </div>
            <div v-else-if="quickContacts.length === 0" class="dashboard-section-state">
              <Inbox :size="17" />
              还没有联系人
            </div>
            <div v-else class="dashboard-detail-list">
              <div
                v-for="contact in quickContacts"
                :key="contact.id"
                class="dashboard-contact-row"
              >
                <RouterLink :to="{ name: 'contacts', params: { contactId: contact.id } }">
                  <BaseAvatar :name="contact.display_name" />
                  <span>
                    <strong>{{ contact.display_name }}</strong>
                    <small>{{ primaryPhone(contact.phones) || '没有号码' }}</small>
                  </span>
                </RouterLink>
                <span class="dashboard-contact-actions">
                  <button
                    class="icon-button"
                    type="button"
                    :disabled="!primaryPhone(contact.phones) || Boolean(dialUnavailable)"
                    :title="dialUnavailable || '拨号'"
                    :aria-label="`呼叫 ${contact.display_name}`"
                    @click="callContact(contact)"
                  >
                    <Phone :size="17" />
                  </button>
                  <button
                    class="icon-button"
                    type="button"
                    :disabled="!primaryPhone(contact.phones) || Boolean(messageUnavailable)"
                    :title="messageUnavailable || '发消息'"
                    :aria-label="`给 ${contact.display_name} 发消息`"
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

      <template v-else-if="selectedCall">
        <header class="detail-header">
          <button class="icon-button mobile-back" type="button" title="返回首页" @click="backToList">
            <ArrowLeft :size="20" />
          </button>
          <BaseAvatar :name="callName(selectedCall)" size="large" />
          <div class="detail-header__identity">
            <h2>{{ callName(selectedCall) }}</h2>
            <span>{{ selectedCall.remote_number }}</span>
          </div>
        </header>
        <div class="call-detail">
          <div class="call-detail__actions">
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '回拨'"
              @click="callNumber(selectedCall.remote_number, callName(selectedCall), selectedCall.device_id)"
            >
              <Phone :size="19" />
              <span>回拨</span>
            </button>
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '发消息'"
              @click="startMessage(selectedCall.remote_number, callName(selectedCall), selectedCall.device_id)"
            >
              <MessageSquareText :size="19" />
              <span>消息</span>
            </button>
          </div>
          <section class="detail-section detail-facts">
            <h3>通话详情</h3>
            <dl>
              <div>
                <dt>方向</dt>
                <dd>
                  {{
                    selectedCall.missed
                      ? '未接来电'
                      : selectedCall.direction === 'incoming'
                        ? '呼入'
                        : '呼出'
                  }}
                </dd>
              </div>
              <div><dt>时间</dt><dd>{{ formatDateTime(selectedCall.started_at) }}</dd></div>
              <div>
                <dt>时长</dt>
                <dd>
                  {{
                    selectedCall.missed
                      ? '未接通'
                      : formatDuration(selectedCall.duration_seconds)
                  }}
                </dd>
              </div>
              <div>
                <dt>线路</dt>
                <dd>
                  <LineTag
                    :line="lineTagLine(lineForCall(selectedCall), selectedCall.device_id)"
                    :fallback="callLineFallback(selectedCall)"
                  />
                </dd>
              </div>
              <div v-if="selectedCall.failure_reason">
                <dt>结果</dt><dd>{{ selectedCall.failure_reason }}</dd>
              </div>
            </dl>
          </section>
          <RouterLink
            class="dashboard-open-resource"
            :to="{ name: 'calls', query: { selected: selectedCall.id } }"
          >
            打开通话记录
            <ChevronRight :size="16" />
          </RouterLink>
        </div>
      </template>

      <template v-else-if="selectedThread">
        <header class="detail-header">
          <button class="icon-button mobile-back" type="button" title="返回首页" @click="backToList">
            <ArrowLeft :size="20" />
          </button>
          <BaseAvatar :name="threadName(selectedThread)" size="large" />
          <div class="detail-header__identity">
            <h2>{{ threadName(selectedThread) }}</h2>
            <span>{{ selectedThread.peer }}</span>
          </div>
        </header>
        <div class="call-detail dashboard-message-detail">
          <div class="call-detail__actions">
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '发消息'"
              @click="startMessage(selectedThread.peer, threadName(selectedThread), selectedThread.line_id || selectedThread.iccid)"
            >
              <MessageSquareText :size="19" />
              <span>消息</span>
            </button>
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '拨号'"
              @click="callNumber(selectedThread.peer, threadName(selectedThread), selectedThread.line_id || selectedThread.iccid)"
            >
              <Phone :size="19" />
              <span>拨号</span>
            </button>
          </div>
          <section class="detail-section">
            <h3>最近消息</h3>
            <LineTag
              class="dashboard-detail-line-tag"
              :line="lineTagLine(lineForThread(selectedThread), selectedThread.line_id, selectedThread.iccid)"
              :fallback="threadLineFallback(selectedThread)"
            />
            <p class="dashboard-message-preview">
              {{ selectedThread.last_content || '没有消息内容' }}
            </p>
            <small>{{ formatDateTime(selectedThread.last_timestamp) }}</small>
          </section>
          <RouterLink
            class="dashboard-open-resource"
            :to="{ name: 'messages', params: { threadKey: selectedThread.key } }"
          >
            打开对话
            <ChevronRight :size="16" />
          </RouterLink>
        </div>
      </template>

      <StatePanel
        v-else-if="activityLoading"
        state="loading"
        title="正在载入活动详情"
      />
      <StatePanel
        v-else
        state="empty"
        title="活动不存在"
        detail="返回列表选择其他通信活动"
      />
    </article>
  </section>
</template>

<style scoped>
.dashboard-workspace {
  grid-template-columns: minmax(300px, 344px) minmax(0, 1fr);
}

.dashboard-activity-pane {
  background: var(--surface);
}

.dashboard-pane-action {
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
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

.dashboard-activity-row {
  min-height: 72px;
  padding: 10px 16px;
}

.dashboard-activity-row .list-item__content strong {
  font-size: 14px;
}

.dashboard-activity-row .list-item__content small,
.dashboard-overview-row .list-item__content small,
.dashboard-activity-row time {
  font-size: 12px;
}

.dashboard-activity-meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.dashboard-activity-meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-inline-error {
  min-height: 48px;
  padding: 9px 14px;
  font-size: 12px;
}

.dashboard-detail-header {
  min-height: 76px;
}

.dashboard-detail-symbol {
  width: 42px;
  height: 42px;
  flex-basis: 42px;
  border-radius: 7px;
}

.dashboard-header-actions {
  gap: 8px;
}

.dashboard-command-button {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  padding: 0 13px;
  color: var(--accent-strong);
  font-size: 13px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.dashboard-command-button:hover:not(:disabled) {
  background: var(--surface-hover);
  border-color: var(--accent);
}

.dashboard-detail-scroll {
  padding: 20px 24px 28px;
  container-type: inline-size;
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

.dashboard-summary-card.is-contacts .dashboard-summary-icon {
  color: #7357a5;
  background: #f1ecf8;
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

.dashboard-module-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 12px;
}

.dashboard-detail-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
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

.dashboard-open-resource,
.dashboard-message-detail .detail-section > small {
  font-size: 12px;
}

.dashboard-detail-line-tag {
  margin-bottom: 10px;
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
  .dashboard-header-actions .dashboard-command-button {
    width: 40px;
    padding: 0;
  }

  .dashboard-header-actions .dashboard-command-button span {
    display: none;
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
}

@media (max-width: 390px) {
  .dashboard-detail-scroll {
    padding-inline: 12px;
  }

  .dashboard-detail-symbol {
    display: none;
  }
}
</style>
