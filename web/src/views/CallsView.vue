<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  LoaderCircle,
  MessageSquareText,
  Phone,
  Trash2
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
import SwipeActionRow from '../components/SwipeActionRow.vue'
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
  capabilityReason,
  contactForNumber,
  deleteCall,
  displayPhoneNumber,
  lineForKey,
  lineKey,
  loadBootstrap,
  loadCalls,
  loadContacts,
  markMissedCallRead,
  markMissedCallsRead
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
const missedReadError = ref('')
const callMutationError = ref('')
const deletingCallID = ref('')
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
const unreadMissedCallIDs = computed(() =>
  callsResource.data
    .filter(call => call.missed && !call.read)
    .map(call => call.id)
    .sort()
    .join('\u0000')
)

function callFilterFromRoute(value: unknown): CallFilter {
  return value === 'missed' || value === 'incoming' || value === 'outgoing'
    ? value
    : 'all'
}

function callFilterQuery(value = filter.value): { filter?: CallFilter } {
  return value === 'all' ? {} : { filter: value }
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
  void router.push({
    name: 'calls',
    query: { selected: call.id, ...callFilterQuery() }
  })
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

function backToList(): void {
  if (embedded.value) {
    emit('close')
    return
  }
  void router.push({ name: 'calls', query: callFilterQuery() })
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
  () => route.query.filter,
  value => {
    if (!embedded.value) filter.value = callFilterFromRoute(value)
  }
)

async function acknowledgeMissedCalls(): Promise<void> {
  missedReadError.value = ''
  try {
    await markMissedCallsRead()
  } catch (error) {
    missedReadError.value = t('calls.markReadFailed', {
      error: error instanceof Error ? error.message : String(error)
    })
  }
}

function retryMissedCallsRead(): void {
  void acknowledgeMissedCalls()
}

watch(
  [filter, () => callsResource.status, unreadMissedCallIDs],
  ([activeFilter, status, unreadIDs]) => {
    if (!embedded.value && activeFilter === 'missed' && status === 'ready' && unreadIDs) {
      void acknowledgeMissedCalls()
    }
  },
  { immediate: true }
)

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
        <div class="pane-search-row">
          <SearchField
            v-model="search"
            :placeholder="t('contacts.searchNameOrNumber')"
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
        </div>
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
      <div
        v-if="missedReadError"
        class="call-sync-status call-sync-status--error"
        role="alert"
      >
        <span>{{ missedReadError }}</span>
        <button type="button" @click="retryMissedCallsRead">
          {{ t('common.retry') }}
        </button>
      </div>
      <div
        v-if="callMutationError"
        class="call-sync-status call-sync-status--error"
        role="alert"
      >
        {{ callMutationError }}
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
        <SwipeActionRow
          v-for="call in filteredCalls"
          :key="call.id"
          :can-read="call.missed && !call.read"
          :read-label="t('common.markRead')"
          :delete-label="t('common.delete')"
          :disabled="Boolean(deletingCallID)"
          @read="acknowledgeMissedCall(call)"
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
            :number="callDisplayNumber(selected)"
            :avatar="selectedContact?.avatar"
            :line="lineTagLine(lineForCall(selected), selected.line_id)"
            :line-fallback="callLineFallback(selected)"
          />
          <div class="detail-header__actions call-detail__header-actions">
            <ContactNumberActions
              :number="selected.remote_number"
              :contact="selectedContact"
              compact
            />
            <button
              v-if="selectedIsContactable"
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
              v-if="selectedIsContactable"
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
            <button
              class="icon-button icon-button--danger desktop-delete-action"
              type="button"
              :disabled="Boolean(deletingCallID)"
              :title="t('calls.delete')"
              @click="removeCall(selected)"
            >
              <Trash2 :size="18" />
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

.calls-workspace .detail-header {
  container-type: inline-size;
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

@container (max-width: 760px) {
  .desktop-delete-action {
    display: none;
  }

  .call-detail__header-actions {
    gap: 6px;
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

  .call-detail__header-actions
    :deep(.contact-number-actions.is-compact .secondary-button) {
    width: 36px;
    min-width: 36px;
    height: 36px;
    min-height: 36px;
    padding: 0;
    border-radius: 50%;
  }

  .call-detail__header-actions
    :deep(.contact-number-actions.is-compact .contact-number-action__label) {
    display: none;
  }
}
</style>
