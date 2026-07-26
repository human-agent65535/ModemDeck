<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  LoaderCircle,
  MessageSquareText,
  Phone
} from '@lucide/vue'
import type { CallFilter, CallRecord } from '../api/types'
import CallHistoryListItem from '../components/CallHistoryListItem.vue'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import LineSelector from '../components/LineSelector.vue'
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
import { formatDateTime, formatDuration } from '../utils/format'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine
} from '../utils/lineIdentity'

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
const filter = ref<CallFilter>('all')
const lineFilterKey = ref('all')
const lines = computed(() => bootstrapResource.data?.lines || [])
const lineLookup = computed(() => createLineLookup(lines.value))
const defaultDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
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
      return matchesFilter && matchesLine && matchesSearch
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

function displayName(call: CallRecord): string {
  return call.display_name || contactForNumber(call.remote_number)?.display_name || call.remote_number
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
  if (embedded.value) {
    emit('close')
    return
  }
  void router.push({ name: 'calls' })
}

function callBack(call: CallRecord): void {
  if (dialUnavailable.value) return
  openDialer(call.remote_number, displayName(call), actionLineKey(call))
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
  if (messageUnavailable.value) return
  const selectedLineKey = actionLineKey(call)
  if (embedded.value) {
    emit('message', {
      number: call.remote_number,
      name: displayName(call),
      contextLineKey: selectedLineKey
    })
    return
  }
  void router.push({
    name: 'messages',
    query: {
      compose: call.remote_number,
      name: displayName(call),
      ...(selectedLineKey ? { line: selectedLineKey } : {})
    }
  })
}

watch(lines, availableLines => {
  if (
    lineFilterKey.value !== 'all' &&
    !availableLines.some(line => lineKey(line) === lineFilterKey.value)
  ) {
    lineFilterKey.value = 'all'
  }
})

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
  <section
    class="workspace calls-workspace"
    :class="{ 'has-selection': selected, 'is-embedded': embedded }"
  >
    <aside v-if="!embedded" class="list-pane">
      <header class="pane-header">
        <div>
          <h1>{{ t('shell.calls') }}</h1>
          <span v-if="callsResource.status === 'ready'">{{ callsResource.data.length }}</span>
        </div>
      </header>
      <div class="pane-search pane-search--calls">
        <SearchField v-model="search" :placeholder="t('contacts.searchNameOrNumber')" />
        <LineSelector
          v-if="lines.length > 1"
          v-model="lineFilterKey"
          class="call-line-filter"
          :lines="lines"
          :default-device-imei="defaultDeviceIMEI"
          :label="t('calls.lineFilter')"
          include-all
          :all-label="t('calls.allLines')"
          :all-description="t('calls.allLinesDescription')"
        />
        <div class="segmented-control" :aria-label="t('calls.filter')">
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
        {{ t('calls.connecting') }}
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
        :title="t('calls.loading')"
      />
      <StatePanel
        v-else-if="callsResource.status === 'forbidden'"
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
      <StatePanel
        v-else-if="filteredCalls.length === 0"
        state="empty"
        :title="
          search || filter !== 'all' || lineFilterKey !== 'all'
            ? t('calls.noMatches')
            : t('calls.empty')
        "
      />
      <div v-else class="item-list">
        <CallHistoryListItem
          v-for="call in filteredCalls"
          :key="call.id"
          :call="call"
          :name="displayName(call)"
          :avatar="avatarForCall(call)"
          :line="lineTagLine(lineForCall(call), call.local_phone, call.line_iccid, call.line_imsi)"
          :line-fallback="callLineFallback(call)"
          :selected="call.id === selectedId"
          :has-recording="hasPlayableRecording(call)"
          @select="selectCall"
        />
      </div>
    </aside>

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            :title="t('calls.back')"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <ContactHeaderIdentity
            :name="displayName(selected)"
            :number="selected.remote_number"
            :avatar="selectedContact?.avatar"
            :line="lineTagLine(lineForCall(selected), selected.local_phone, selected.line_iccid, selected.line_imsi)"
            :line-fallback="callLineFallback(selected)"
          />
          <div class="detail-header__actions call-detail__header-actions">
            <ContactNumberActions
              :number="selected.remote_number"
              :contact="selectedContact"
              compact
            />
            <button
              class="call-detail__command call-detail__command--primary"
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
              class="call-detail__command"
              type="button"
              :disabled="Boolean(messageUnavailable)"
              :title="messageUnavailable || t('messages.sendMessage')"
              :aria-label="t('calls.messageName', { name: displayName(selected) })"
              @click="sendMessage(selected)"
            >
              <MessageSquareText :size="17" />
              <span>{{ t('shell.messages') }}</span>
            </button>
          </div>
        </header>

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
                    :line="lineTagLine(lineForCall(selected), selected.local_phone, selected.line_iccid, selected.line_imsi)"
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

      <StatePanel
        v-else
        state="empty"
        :title="t('calls.select')"
        :detail="t('calls.detailPlaceholder')"
      />
    </article>
  </section>
</template>

<style scoped>
.calls-workspace.is-embedded {
  grid-template-columns: minmax(0, 1fr);
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
