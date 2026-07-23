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
  Settings
} from '@lucide/vue'
import type { CallRecord, Contact, LineSummary, MessageThread } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import StatePanel from '../components/StatePanel.vue'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  callsResource,
  capabilityReason,
  contactForNumber,
  contactsResource,
  lineKey,
  lineLabel,
  loadBootstrap,
  loadCalls,
  loadContacts,
  loadThreads,
  threadsResource
} from '../state/workspace'
import {
  formatDateTime,
  formatDuration,
  formatRelativeDate,
  primaryPhone
} from '../utils/format'

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

function lineCapabilities(line: LineSummary): string {
  const available: string[] = []
  if (line.capabilities?.dial === true) available.push('通话')
  if (line.capabilities?.message === true) available.push('消息')
  return available.length > 0 ? available.join(' · ') : '能力未报告'
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

function callNumber(number: string, label = ''): void {
  if (dialUnavailable.value) return
  openDialer(number, label)
}

function startMessage(number: string, name = ''): void {
  if (messageUnavailable.value) return
  void router.push({
    name: 'messages',
    query: { compose: number, ...(name ? { name } : {}) }
  })
}

function callContact(contact: Contact): void {
  const number = primaryPhone(contact.phones)
  if (number) callNumber(number, contact.display_name)
}

function messageContact(contact: Contact): void {
  const number = primaryPhone(contact.phones)
  if (number) startMessage(number, contact.display_name)
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
    loadContacts()
  ])
}

onMounted(loadDashboard)
</script>

<template>
  <section class="workspace dashboard-workspace" :class="{ 'has-selection': hasSelection }">
    <aside class="list-pane dashboard-activity-pane">
      <header class="pane-header">
        <div>
          <h1>首页</h1>
          <span v-if="!activityLoading">{{ activities.length }}</span>
        </div>
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
          <small>{{ lines.length }} 条线路 · {{ unreadMessages }} 条未读</small>
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
              <small>{{ activityDescription(activity) }}</small>
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
          <span class="dashboard-detail-symbol"><House :size="21" /></span>
          <div class="detail-header__identity">
            <h2>通信概览</h2>
            <span>线路、未读消息与未接来电</span>
          </div>
          <div class="detail-header__actions">
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
          <section class="dashboard-summary-strip" aria-label="通信汇总">
            <div><strong>{{ lines.length }}</strong><span>线路</span></div>
            <div><strong>{{ unreadMessages }}</strong><span>未读消息</span></div>
            <div><strong>{{ missedCalls }}</strong><span>未接来电</span></div>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-lines-title">
            <header>
              <h3 id="dashboard-lines-title">线路状态</h3>
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
            <div v-else class="dashboard-detail-list">
              <div v-for="line in lines" :key="lineKey(line)" class="dashboard-line-row">
                <span class="dashboard-activity-icon"><Phone :size="17" /></span>
                <span>
                  <strong>{{ lineLabel(line) }}</strong>
                  <small>{{ line.phone_number || line.operator || lineKey(line) }}</small>
                </span>
                <span>
                  <strong>{{ line.state || '状态未报告' }}</strong>
                  <small>{{ lineCapabilities(line) }}</small>
                </span>
              </div>
            </div>
          </section>

          <section class="dashboard-detail-section" aria-labelledby="dashboard-contacts-title">
            <header>
              <h3 id="dashboard-contacts-title">快捷联系人</h3>
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
              @click="callNumber(selectedCall.remote_number, callName(selectedCall))"
            >
              <Phone :size="19" />
              <span>回拨</span>
            </button>
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '发消息'"
              @click="startMessage(selectedCall.remote_number, callName(selectedCall))"
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
              @click="startMessage(selectedThread.peer, threadName(selectedThread))"
            >
              <MessageSquareText :size="19" />
              <span>消息</span>
            </button>
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '拨号'"
              @click="callNumber(selectedThread.peer, threadName(selectedThread))"
            >
              <Phone :size="19" />
              <span>拨号</span>
            </button>
          </div>
          <section class="detail-section">
            <h3>最近消息</h3>
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
