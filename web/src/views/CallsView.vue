<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  CassetteTape,
  MessageSquareText,
  Phone,
  PhoneIncoming,
  LoaderCircle,
  PhoneMissed,
  PhoneOutgoing
} from '@lucide/vue'
import type { CallFilter, CallRecord } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import LineTag from '../components/LineTag.vue'
import RecordingList from '../components/RecordingList.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { callState } from '../state/call'
import {
  loadRecordingEntries,
  recordingCatalogState
} from '../state/recording'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  callsResource,
  capabilityReason,
  contactForNumber,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadContacts
} from '../state/workspace'
import { formatDateTime, formatDuration, formatRelativeDate } from '../utils/format'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine
} from '../utils/lineIdentity'

const route = useRoute()
const router = useRouter()
const search = ref('')
const filter = ref<CallFilter>('all')

const filters: Array<{ value: CallFilter; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'missed', label: '未接' },
  { value: 'incoming', label: '呼入' },
  { value: 'outgoing', label: '呼出' }
]

const filteredCalls = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  return callsResource.data
    .filter(call => {
      const matchesFilter =
        filter.value === 'all' ||
        (filter.value === 'missed' && call.missed) ||
        (filter.value === 'incoming' && call.direction === 'incoming') ||
        (filter.value === 'outgoing' && call.direction === 'outgoing')
      const matchesSearch =
        !query ||
        (call.display_name || '').toLocaleLowerCase().includes(query) ||
        (digits.length > 0 && call.remote_number.replace(/\D/g, '').includes(digits))
      return matchesFilter && matchesSearch
    })
    .slice()
    .sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))
})
const selectedId = computed(() => (typeof route.query.selected === 'string' ? route.query.selected : ''))
const selected = computed(() => callsResource.data.find(call => call.id === selectedId.value))
const selectedContact = computed(() =>
  selected.value ? contactForNumber(selected.value.remote_number) : undefined
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
const lines = computed(() => bootstrapResource.data?.lines || [])
const lineLookup = computed(() => createLineLookup(lines.value))
const defaultDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
)

function displayName(call: CallRecord): string {
  return call.display_name || contactForNumber(call.remote_number)?.display_name || call.remote_number
}

function iconFor(call: CallRecord) {
  if (call.missed) return PhoneMissed
  return call.direction === 'incoming' ? PhoneIncoming : PhoneOutgoing
}

function directionLabel(call: CallRecord): string {
  if (call.missed) return '未接来电'
  return call.direction === 'incoming' ? '呼入' : '呼出'
}

function hasPlayableRecording(call: CallRecord): boolean {
  return playableRecordingCallIDs.value.has(call.id)
}

function lineForCall(call: CallRecord) {
  if (call.local_phone) {
    return findLine(lineLookup.value, call.local_phone)
  }
  return findLine(
    lineLookup.value,
    call.line_iccid,
    call.line_imsi
  )
}

function callLineFallback(call: CallRecord): string {
  const line = lineForCall(call)
  if (!line && call.local_phone?.trim()) return call.local_phone.trim()
  return lineTagFallback(
    line,
    lines.value,
    defaultDeviceIMEI.value,
    call.line_iccid,
    call.line_imsi
  )
}

function actionLineKey(call: CallRecord): string {
  const line = lineForCall(call)
  return line ? lineKey(line) : ''
}

function selectCall(call: CallRecord): void {
  void router.push({ name: 'calls', query: { selected: call.id } })
}

function backToList(): void {
  void router.push({ name: 'calls' })
}

function callBack(call: CallRecord): void {
  if (dialUnavailable.value) return
  openDialer(call.remote_number, displayName(call), actionLineKey(call))
}

function sendMessage(call: CallRecord): void {
  if (messageUnavailable.value) return
  const selectedLineKey = actionLineKey(call)
  void router.push({
    name: 'messages',
    query: {
      compose: call.remote_number,
      name: displayName(call),
      ...(selectedLineKey ? { line: selectedLineKey } : {})
    }
  })
}

watch(
  () => callState.session?.phase,
  phase => {
    if (phase === 'ended' || phase === 'failed') void loadCalls(true)
  }
)

onMounted(() => {
  void Promise.all([
    loadBootstrap(),
    loadCalls(),
    loadContacts(),
    loadRecordingEntries()
  ])
})
</script>

<template>
  <section class="workspace" :class="{ 'has-selection': selected }">
    <aside class="list-pane">
      <header class="pane-header">
        <div>
          <h1>通话</h1>
          <span v-if="callsResource.status === 'ready'">{{ callsResource.data.length }}</span>
        </div>
      </header>
      <div class="pane-search pane-search--calls">
        <SearchField v-model="search" placeholder="搜索姓名或号码" />
        <div class="segmented-control" aria-label="通话筛选">
          <button
            v-for="item in filters"
            :key="item.value"
            type="button"
            :class="{ 'is-active': filter === item.value }"
            @click="filter = item.value"
          >
            {{ item.label }}
          </button>
        </div>
      </div>
      <div
        v-if="callState.syncStatus === 'loading'"
        class="call-sync-status"
        role="status"
      >
        <LoaderCircle class="spin" :size="14" />
        正在连接通话服务
      </div>
      <div
        v-else-if="callState.syncStatus === 'error' || callState.syncStatus === 'forbidden'"
        class="call-sync-status call-sync-status--error"
        role="alert"
      >
        {{ callState.syncError }}
      </div>

      <StatePanel
        v-if="callsResource.status === 'loading'"
        state="loading"
        title="正在载入通话记录"
      />
      <StatePanel
        v-else-if="callsResource.status === 'forbidden'"
        state="forbidden"
        title="无权查看通话记录"
        :detail="callsResource.error"
      />
      <StatePanel
        v-else-if="callsResource.status === 'error'"
        state="error"
        title="无法载入通话记录"
        :detail="callsResource.error"
        retryable
        @retry="loadCalls(true)"
      />
      <StatePanel
        v-else-if="filteredCalls.length === 0"
        state="empty"
        :title="search || filter !== 'all' ? '没有匹配的通话' : '还没有通话记录'"
      />
      <div v-else class="item-list">
        <div
          v-for="call in filteredCalls"
          :key="call.id"
          class="list-item call-list-item"
          :class="{ 'is-selected': call.id === selectedId, 'is-missed': call.missed }"
        >
          <button
            class="call-list-item__select"
            type="button"
            :aria-label="`查看 ${displayName(call)} 的通话详情`"
            @click="selectCall(call)"
          >
            <span class="call-direction-icon"><component :is="iconFor(call)" :size="18" /></span>
            <span class="list-item__content">
              <span class="list-item__title">
                <strong>{{ displayName(call) }}</strong>
                <time>{{ formatRelativeDate(call.started_at) }}</time>
              </span>
              <span class="call-list-item__meta">
                <LineTag
                  :line="lineTagLine(lineForCall(call), call.local_phone, call.line_iccid, call.line_imsi)"
                  :fallback="callLineFallback(call)"
                />
                <small>{{ directionLabel(call) }} · {{ call.remote_number }}</small>
                <span
                  v-if="hasPlayableRecording(call)"
                  class="call-list-item__recording"
                  role="img"
                  aria-label="有通话录音"
                  title="有通话录音"
                >
                  <CassetteTape :size="15" aria-hidden="true" />
                </span>
              </span>
            </span>
          </button>
          <button
            class="icon-button icon-button--quiet call-list-item__call"
            type="button"
            :disabled="Boolean(dialUnavailable)"
            :title="dialUnavailable || '回拨'"
            :aria-label="`回拨 ${displayName(call)}`"
            @click="callBack(call)"
            @keydown.enter.prevent="callBack(call)"
          >
            <Phone :size="17" />
          </button>
        </div>
      </div>
    </aside>

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button class="icon-button mobile-back" type="button" title="返回通话" @click="backToList">
            <ArrowLeft :size="20" />
          </button>
          <BaseAvatar
            :name="displayName(selected)"
            :src="selectedContact?.avatar"
            size="large"
          />
          <div class="detail-header__identity">
            <h2>{{ displayName(selected) }}</h2>
            <span>{{ selected.remote_number }}</span>
          </div>
          <div class="detail-header__actions call-detail__header-actions">
            <button
              class="call-detail__command call-detail__command--primary"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '回拨'"
              :aria-label="`回拨 ${displayName(selected)}`"
              @click="callBack(selected)"
            >
              <Phone :size="17" />
              <span>回拨</span>
            </button>
            <button
              class="call-detail__command"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '发送消息'"
              :aria-label="`给 ${displayName(selected)} 发送消息`"
              @click="sendMessage(selected)"
            >
              <MessageSquareText :size="17" />
              <span>消息</span>
            </button>
          </div>
        </header>

        <div class="call-detail">
          <div class="call-detail__contact-actions">
            <ContactNumberActions
              :number="selected.remote_number"
              :contact="selectedContact"
            />
          </div>

          <section class="detail-section detail-facts">
            <h3>通话详情</h3>
            <dl>
              <div><dt>方向</dt><dd>{{ directionLabel(selected) }}</dd></div>
              <div><dt>时间</dt><dd>{{ formatDateTime(selected.started_at) }}</dd></div>
              <div><dt>时长</dt><dd>{{ selected.missed ? '未接通' : formatDuration(selected.duration_seconds) }}</dd></div>
              <div>
                <dt>线路</dt>
                <dd>
                  <LineTag
                    :line="lineTagLine(lineForCall(selected), selected.local_phone, selected.line_iccid, selected.line_imsi)"
                    :fallback="callLineFallback(selected)"
                  />
                </dd>
              </div>
              <div v-if="selected.failure_reason"><dt>结果</dt><dd>{{ selected.failure_reason }}</dd></div>
            </dl>
          </section>

          <RecordingList :call-id="selected.id" />

          <p v-if="dialUnavailable || messageUnavailable" class="unavailable-note">
            {{ dialUnavailable || messageUnavailable }}
          </p>
        </div>
      </template>

      <StatePanel
        v-else
        state="empty"
        title="选择一条通话记录"
        detail="通话详情会显示在这里"
      />
    </article>
  </section>
</template>

<style scoped>
.call-list-item {
  gap: 0;
  padding: 0;
  cursor: default;
}

.call-list-item__select {
  display: flex;
  min-width: 0;
  min-height: 76px;
  flex: 1;
  align-items: center;
  gap: 12px;
  padding: 10px 6px 10px 14px;
  color: inherit;
  text-align: left;
  background: transparent;
}

.call-list-item__select:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.call-list-item__call {
  margin-right: 10px;
}

.call-list-item__meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.call-list-item__meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-list-item__recording {
  display: inline-grid;
  width: 22px;
  height: 22px;
  flex: 0 0 22px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.call-detail__header-actions {
  gap: 8px;
}

.call-detail__command {
  display: inline-flex;
  min-width: 72px;
  min-height: 36px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 11px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
}

.call-detail__command:hover:not(:disabled) {
  background: var(--surface-hover);
}

.call-detail__command--primary {
  color: #ffffff;
  background: var(--accent);
  border-color: var(--accent);
}

.call-detail__command--primary:hover:not(:disabled) {
  background: var(--accent-strong);
  border-color: var(--accent-strong);
}

.call-detail__contact-actions {
  max-width: 760px;
  margin-bottom: 20px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--border);
}

.call-detail__contact-actions :deep(.secondary-button) {
  min-height: 32px;
  padding: 0 4px;
  color: var(--accent-strong);
  background: transparent;
  border: 0;
}

.call-detail__contact-actions :deep(.secondary-button:hover:not(:disabled)) {
  background: var(--accent-soft);
}

@media (max-width: 720px) {
  .call-detail__header-actions {
    gap: 4px;
  }

  .call-detail__command {
    width: 36px;
    min-width: 36px;
    height: 36px;
    min-height: 36px;
    padding: 0;
    border-radius: 50%;
  }

  .call-detail__command span {
    display: none;
  }
}
</style>
