<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  MessageSquareText,
  Phone,
  PhoneIncoming,
  PhoneMissed,
  PhoneOutgoing
} from '@lucide/vue'
import type { CallFilter, CallRecord } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { openDialer } from '../state/ui'
import {
  callsResource,
  capabilityReason,
  contactForNumber,
  deviceName,
  loadCalls,
  loadContacts,
  loadDevices
} from '../state/workspace'
import { formatDateTime, formatDuration, formatRelativeDate } from '../utils/format'

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
const dialUnavailable = computed(() => capabilityReason('dial'))
const messageUnavailable = computed(() => capabilityReason('message'))

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

function selectCall(call: CallRecord): void {
  void router.push({ name: 'calls', query: { selected: call.id } })
}

function backToList(): void {
  void router.push({ name: 'calls' })
}

function callBack(call: CallRecord): void {
  if (dialUnavailable.value) return
  openDialer(call.remote_number, displayName(call))
}

function sendMessage(call: CallRecord): void {
  if (messageUnavailable.value) return
  void router.push({
    name: 'messages',
    query: { compose: call.remote_number, name: displayName(call) }
  })
}

onMounted(() => {
  void Promise.all([loadCalls(), loadContacts(), loadDevices()])
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

      <StatePanel
        v-if="callsResource.status === 'loading'"
        state="loading"
        title="正在载入通话记录"
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
          role="button"
          tabindex="0"
          @click="selectCall(call)"
          @keydown.enter="selectCall(call)"
          @keydown.space.prevent="selectCall(call)"
        >
          <span class="call-direction-icon"><component :is="iconFor(call)" :size="18" /></span>
          <span class="list-item__content">
            <span class="list-item__title">
              <strong>{{ displayName(call) }}</strong>
              <time>{{ formatRelativeDate(call.started_at) }}</time>
            </span>
            <small>{{ directionLabel(call) }} · {{ call.remote_number }}</small>
          </span>
          <button
            class="icon-button icon-button--quiet call-list-item__call"
            type="button"
            :disabled="Boolean(dialUnavailable)"
            :title="dialUnavailable || '回拨'"
            @click.stop="callBack(call)"
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
          <BaseAvatar :name="displayName(selected)" size="large" />
          <div class="detail-header__identity">
            <h2>{{ displayName(selected) }}</h2>
            <span>{{ selected.remote_number }}</span>
          </div>
        </header>

        <div class="call-detail">
          <div class="call-detail__actions">
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(dialUnavailable)"
              :title="dialUnavailable || '回拨'"
              @click="callBack(selected)"
            >
              <Phone :size="19" />
              <span>回拨</span>
            </button>
            <button
              class="action-button"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || '发送消息'"
              @click="sendMessage(selected)"
            >
              <MessageSquareText :size="19" />
              <span>消息</span>
            </button>
          </div>

          <section class="detail-section detail-facts">
            <h3>通话详情</h3>
            <dl>
              <div><dt>方向</dt><dd>{{ directionLabel(selected) }}</dd></div>
              <div><dt>时间</dt><dd>{{ formatDateTime(selected.started_at) }}</dd></div>
              <div><dt>时长</dt><dd>{{ selected.missed ? '未接通' : formatDuration(selected.duration_seconds) }}</dd></div>
              <div><dt>设备</dt><dd>{{ deviceName(selected.device_id) }}</dd></div>
              <div v-if="selected.failure_reason"><dt>结果</dt><dd>{{ selected.failure_reason }}</dd></div>
            </dl>
          </section>

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
