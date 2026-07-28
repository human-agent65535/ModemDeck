<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, LoaderCircle, MessageSquarePlus, Phone, Send, X } from '@lucide/vue'
import type { Contact, LineSummary, MessageThread } from '../api/types'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import ContactSuggestInput from '../components/ContactSuggestInput.vue'
import LineSelector from '../components/LineSelector.vue'
import MessageThreadListItem from '../components/MessageThreadListItem.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  capabilityReason,
  contactForNumber,
  contactsResource,
  lineKey,
  lineSupports,
  loadBootstrap,
  loadContacts,
  loadMessages,
  loadThreads,
  markThreadRead,
  messagesFor,
  recentIncomingMessageIDs,
  recentIncomingThreadKeys,
  resolveLine,
  sendMessage,
  threadReadErrors,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate } from '../utils/format'
import { lineTagFallback, lineTagLine } from '../utils/lineIdentity'
import {
  findRecipientThread,
  messageReturnRoute,
  messageThreadUsesLine
} from './messages/messageFlow'

type MessageReadFilter = 'all' | 'unread' | 'read'

const props = withDefaults(
  defineProps<{
    embeddedCompose?: boolean
    embeddedThreadKey?: string
    initialRecipient?: string
    initialRecipientName?: string
    contextLineKey?: string
  }>(),
  {
    embeddedCompose: false,
    embeddedThreadKey: '',
    initialRecipient: '',
    initialRecipientName: '',
    contextLineKey: ''
  }
)
const emit = defineEmits<{
  close: []
  sent: [threadKey?: string]
}>()

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const search = ref('')
const messageFilter = ref<MessageReadFilter>(
  props.embeddedThreadKey || props.embeddedCompose
    ? 'all'
    : messageFilterFromRoute(route.query.filter)
)
const composingNew = ref(props.embeddedCompose)
const newRecipient = ref(props.initialRecipient)
const newRecipientName = ref(props.initialRecipientName)
const selectedLineKey = ref('')
const composeContextLineKey = ref(props.contextLineKey)
const lineFilterKey = ref('all')
const draft = ref('')
const sending = ref(false)
const sendError = ref('')
const messagesEnd = ref<HTMLElement | null>(null)

const embedded = computed(
  () => props.embeddedCompose || Boolean(props.embeddedThreadKey)
)
const selectedKey = computed(() =>
  props.embeddedThreadKey || String(route.params.threadKey || '')
)
const selectedThread = computed(() =>
  threadsResource.data.find(thread => thread.key === selectedKey.value)
)
const currentMessages = computed(() =>
  selectedKey.value ? messagesFor(selectedKey.value) : null
)
const selectedReadError = computed(() =>
  selectedThread.value ? threadReadErrors[selectedThread.value.key] || '' : ''
)
const lines = computed(() => bootstrapResource.data?.lines || [])
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const selectedLine = computed(() => lines.value.find(line => lineKey(line) === selectedLineKey.value))
const activeLine = computed(() =>
  composingNew.value ? selectedLine.value : lineForThread(selectedThread.value)
)
const existingRecipientThread = computed(() =>
  composingNew.value
    ? findRecipientThread(threadsResource.data, newRecipient.value, activeLine.value)
    : undefined
)
const replyThreadKey = computed(() => {
  const line = activeLine.value
  const thread = composingNew.value ? existingRecipientThread.value : selectedThread.value
  if (!thread || !line) return undefined
  return threadUsesLine(thread, line) ? thread.key : undefined
})
const messageUnavailable = computed(() => capabilityReason('message'))
const messageWriteUnavailable = computed(() =>
  threadsResource.status === 'forbidden'
    ? t('messages.sendForbidden')
    : messageUnavailable.value
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const messageFilters = computed<Array<{ value: MessageReadFilter; label: string }>>(() => [
  { value: 'all', label: t('common.all') },
  { value: 'unread', label: t('messages.unread') },
  { value: 'read', label: t('messages.read') }
])
const filteredThreads = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  const filteredLine =
    lineFilterKey.value === 'all'
      ? undefined
      : lines.value.find(line => lineKey(line) === lineFilterKey.value)
  return threadsResource.data.filter(thread => {
    if (filteredLine && !threadUsesLine(thread, filteredLine)) return false
    if (messageFilter.value === 'unread' && thread.unread_count <= 0) return false
    if (messageFilter.value === 'read' && thread.unread_count > 0) return false
    if (!query) return true
    return (
      (thread.contact_name || '').toLocaleLowerCase().includes(query) ||
      (digits.length > 0 && thread.peer.replace(/\D/g, '').includes(digits)) ||
      (thread.last_content || '').toLocaleLowerCase().includes(query)
    )
  })
})
const activeRecipient = computed(() =>
  composingNew.value ? newRecipient.value.trim() : selectedThread.value?.peer || ''
)
const activeContact = computed(() => contactForNumber(activeRecipient.value))
const activeLineID = computed(() => (activeLine.value ? lineKey(activeLine.value) : ''))
const sendDisabledReason = computed(() => {
  if (messageWriteUnavailable.value) return messageWriteUnavailable.value
  if (!activeRecipient.value) return t('messages.selectRecipient')
  if (!activeLineID.value) return t('messages.selectLine')
  if (lineSupports(activeLine.value, 'message') === false) {
    return t('messages.lineUnsupported')
  }
  if (!draft.value.trim()) return t('messages.enterMessage')
  return ''
})

let lineSelectionOverridden = false
let openedThreadKey = ''
let attemptedReadKey = ''
let composeReturnThreadKey = ''

function messageFilterFromRoute(value: unknown): MessageReadFilter {
  return value === 'unread' || value === 'read' ? value : 'all'
}

function messageFilterQuery(value = messageFilter.value): { filter?: MessageReadFilter } {
  return value === 'all' ? {} : { filter: value }
}

function setMessageFilter(value: MessageReadFilter): void {
  messageFilter.value = value
  if (embedded.value) return
  const query = { ...route.query }
  if (value === 'all') delete query.filter
  else query.filter = value
  void router.replace({ name: 'messages', params: route.params, query })
}

function threadUsesLine(thread: MessageThread, line: LineSummary): boolean {
  return messageThreadUsesLine(thread, line)
}

function lineForThread(thread?: MessageThread): LineSummary | undefined {
  if (!thread) return undefined
  return lines.value.find(line => lineKey(line) === thread.line_id)
}

function threadLineFallback(thread: MessageThread): string {
  const line = lineForThread(thread)
  return lineTagFallback(
    line,
    lines.value,
    defaultLineID.value,
    thread.line_id
  )
}

function avatarForNumber(number: string): string {
  return contactForNumber(number)?.avatar || ''
}

function displayNameForThread(thread: MessageThread): string {
  return contactForNumber(thread.peer)?.display_name || thread.contact_name || thread.peer
}

async function contactSaved(contact: Contact): Promise<void> {
  if (composingNew.value) newRecipientName.value = contact.display_name
  await loadThreads(true)
}

function syncComposeLine(force = false): void {
  if (!composingNew.value) return
  const selectedStillExists = lines.value.some(line => lineKey(line) === selectedLineKey.value)
  if (!selectedStillExists) lineSelectionOverridden = false
  if (!force && lineSelectionOverridden) return
  const resolved = resolveLine('message', {
    contextKey: composeContextLineKey.value,
    number: newRecipient.value
  })
  selectedLineKey.value = resolved ? lineKey(resolved) : ''
}

watch(
  lines,
  value => {
    if (
      lineFilterKey.value !== 'all' &&
      !value.some(line => lineKey(line) === lineFilterKey.value)
    ) lineFilterKey.value = 'all'
    syncComposeLine()
  },
  { immediate: true }
)

watch(
  [lines, defaultLineID, () => contactsResource.data, newRecipient, composingNew],
  () => syncComposeLine()
)

watch(
  () => [route.params.threadKey, route.query.compose] as const,
  ([, compose]) => {
    if (embedded.value) return
    if (typeof compose === 'string') {
      composingNew.value = true
      newRecipient.value = compose
      newRecipientName.value = typeof route.query.name === 'string' ? route.query.name : ''
      composeContextLineKey.value =
        typeof route.query.line === 'string' ? route.query.line : ''
      lineSelectionOverridden = false
      syncComposeLine(true)
      sendError.value = ''
      return
    }
    composingNew.value = false
    composeContextLineKey.value = ''
  },
  { immediate: true }
)

watch(
  () => route.query.filter,
  value => {
    if (!embedded.value) messageFilter.value = messageFilterFromRoute(value)
  }
)

watch(
  () =>
    [
      selectedKey.value,
      selectedThread.value?.last_timestamp || '',
      selectedThread.value?.unread_count || 0,
      composingNew.value
    ] as const,
  () => {
    if (composingNew.value || !selectedKey.value) {
      openedThreadKey = ''
      attemptedReadKey = ''
      return
    }
    const thread = selectedThread.value
    if (thread) void openThread(thread)
  },
  { immediate: true }
)

watch(
  () => currentMessages.value?.data.length,
  () => scrollToEnd()
)

function scrollToEnd(): void {
  void nextTick(() => messagesEnd.value?.scrollIntoView({ block: 'end' }))
}

async function openThread(thread: MessageThread, force = false): Promise<void> {
  if (openedThreadKey !== thread.key) {
    openedThreadKey = thread.key
    attemptedReadKey = ''
  }
  const messages = await loadMessages(thread, force)
  if (!messages || composingNew.value || selectedKey.value !== thread.key) return

  const current = selectedThread.value
  if (current?.unread_count) {
    const readKey = `${current.key}\u0000${current.last_timestamp}\u0000${current.unread_count}`
    if (readKey !== attemptedReadKey) {
      attemptedReadKey = readKey
      await markThreadRead(current)
    }
  }
  scrollToEnd()
}

function retryThreadRead(): void {
  attemptedReadKey = ''
  const thread = selectedThread.value
  if (thread) void openThread(thread)
}

function chooseThread(key: string): void {
  composingNew.value = false
  composeReturnThreadKey = ''
  draft.value = ''
  sendError.value = ''
  void router.push({
    name: 'messages',
    params: { threadKey: key },
    query: messageFilterQuery()
  })
}

function startMessage(): void {
  if (messageWriteUnavailable.value) return
  composeReturnThreadKey = selectedThread.value?.key || ''
  composingNew.value = true
  newRecipient.value = ''
  newRecipientName.value = ''
  composeContextLineKey.value = ''
  lineSelectionOverridden = false
  syncComposeLine(true)
  draft.value = ''
  sendError.value = ''
  void router.push({
    name: 'messages',
    query: { compose: '', ...messageFilterQuery() }
  })
}

function chooseRecipient(suggestion: { contact: Contact; phone: { number: string } }): void {
  newRecipient.value = suggestion.phone.number
  newRecipientName.value = suggestion.contact.display_name
  syncComposeLine()
}

function backToList(): void {
  if (embedded.value) {
    draft.value = ''
    sendError.value = ''
    emit('close')
    return
  }
  composingNew.value = false
  const destination = messageReturnRoute(composeReturnThreadKey)
  composeReturnThreadKey = ''
  void router.push({ ...destination, query: messageFilterQuery() })
}

function viewExistingRecipientThread(): void {
  const thread = existingRecipientThread.value
  if (thread) chooseThread(thread.key)
}

function changeSendingLine(): void {
  if (composingNew.value) lineSelectionOverridden = true
  sendError.value = ''
}

async function submit(): Promise<void> {
  if (sendDisabledReason.value || sending.value) return
  sending.value = true
  sendError.value = ''
  try {
    const replyKey = replyThreadKey.value
    const result = await sendMessage({
      thread_key: replyKey,
      line_id: activeLineID.value,
      to: activeRecipient.value,
      content: draft.value.trim()
    })
    draft.value = ''
    const sentThread = result.thread
    if (props.embeddedCompose) {
      emit('sent', sentThread?.key)
      return
    }
    if (sentThread && (composingNew.value || sentThread.key !== replyKey)) {
      composingNew.value = false
      await router.replace({
        name: 'messages',
        params: { threadKey: sentThread.key },
        query: messageFilterQuery()
      })
      await loadMessages(sentThread)
    }
    scrollToEnd()
  } catch (error) {
    sendError.value = error instanceof Error ? error.message : t('messages.sendFailed')
  } finally {
    sending.value = false
  }
}

function callCurrent(): void {
  if (dialUnavailable.value || !activeRecipient.value) return
  openDialer(
    activeRecipient.value,
    composingNew.value ? newRecipientName.value : selectedThread.value?.contact_name || '',
    activeLineID.value
  )
}

function statusLabel(status: number): string {
  if (status === 2) return t('messages.sent')
  if (status === 3) return t('messages.sendFailed')
  return ''
}

onMounted(() => {
  void Promise.all([loadBootstrap(), loadContacts(), loadThreads()])
})
</script>

<template>
  <section
    class="workspace messages-workspace"
    :class="{
      'has-selection': selectedThread || composingNew,
      'is-embedded': embedded
    }"
  >
    <aside v-if="!embedded" class="list-pane">
      <header class="pane-header">
        <div>
          <h1>{{ t('shell.messages') }}</h1>
          <span v-if="threadsResource.status === 'ready'">{{ threadsResource.data.length }}</span>
        </div>
        <button
          class="pane-create-button mobile-list-fab"
          type="button"
          :disabled="Boolean(messageWriteUnavailable)"
          :title="messageWriteUnavailable || t('dashboard.newMessage')"
          @click="startMessage"
        >
          <MessageSquarePlus :size="19" />
          <span>{{ t('dashboard.newMessage') }}</span>
        </button>
      </header>
      <div class="pane-search">
        <div class="pane-search-row">
          <SearchField v-model="search" :placeholder="t('messages.search')" />
          <LineSelector
            v-if="lines.length > 1"
            v-model="lineFilterKey"
            class="message-line-filter"
            :lines="lines"
            :default-line-id="defaultLineID"
            :label="t('messages.lineFilter')"
            include-all
            filter-mode
            :all-label="t('messages.allLines')"
            :all-description="t('messages.allLinesDescription')"
          />
        </div>
        <div class="segmented-control" :aria-label="t('messages.filter')">
          <button
            v-for="item in messageFilters"
            :key="item.value"
            type="button"
            :class="{ 'is-active': messageFilter === item.value }"
            @click="setMessageFilter(item.value)"
          >
            {{ item.label }}
          </button>
        </div>
      </div>
      <p
        v-if="threadsResource.status === 'ready' && threadsResource.error"
        class="field-error pane-error"
        role="alert"
      >
        {{ threadsResource.error }}
      </p>

      <StatePanel
        v-if="threadsResource.status === 'loading'"
        state="loading"
        :title="t('messages.loading')"
      />
      <StatePanel
        v-else-if="threadsResource.status === 'forbidden'"
        state="forbidden"
        :title="t('messages.forbidden')"
        :detail="threadsResource.error"
      />
      <StatePanel
        v-else-if="threadsResource.status === 'error'"
        state="error"
        :title="t('messages.loadFailed')"
        :detail="threadsResource.error"
        retryable
        @retry="loadThreads(true)"
      />
      <div v-else-if="filteredThreads.length === 0" class="message-list-empty">
        <StatePanel
          state="empty"
          :title="
            search || messageFilter !== 'all' || lineFilterKey !== 'all'
              ? t('messages.noMatches')
              : t('messages.empty')
          "
        />
        <button
          v-if="!search && !messageWriteUnavailable"
          class="secondary-button"
          type="button"
          @click="startMessage"
        >
          <MessageSquarePlus :size="17" />
          {{ t('dashboard.newMessage') }}
        </button>
      </div>
      <div v-else class="item-list">
        <MessageThreadListItem
          v-for="thread in filteredThreads"
          :key="thread.key"
          :thread="thread"
          :name="displayNameForThread(thread)"
          :avatar="avatarForNumber(thread.peer)"
          :line="lineTagLine(lineForThread(thread), thread.line_id)"
          :line-fallback="threadLineFallback(thread)"
          :selected="thread.key === selectedKey && !composingNew"
          :arriving="recentIncomingThreadKeys[thread.key]"
          @select="chooseThread"
        />
      </div>
    </aside>

    <article class="detail-pane conversation-pane">
      <template v-if="selectedThread || composingNew">
        <header class="conversation-header">
          <button
            class="icon-button"
            :class="{ 'mobile-back': !composingNew }"
            type="button"
            :title="
              composingNew
                ? t('messages.cancelNew')
                : t('messages.back')
            "
            @click="backToList"
          >
            <X v-if="composingNew" :size="20" />
            <ArrowLeft v-else :size="20" />
          </button>
          <template v-if="composingNew">
            <div class="conversation-recipient">
              <ContactSuggestInput
                v-model="newRecipient"
                :contacts="contactsResource.data"
                autofocus
                @select="chooseRecipient"
              />
              <LineSelector
                v-if="lines.length > 0"
                v-model="selectedLineKey"
                class="compose-line-select"
                :lines="lines"
                :default-line-id="defaultLineID"
                :label="t('messages.sendingLine')"
                capability="message"
                compact
                :unavailable-label="t('messages.unsupported')"
                @change="changeSendingLine"
              />
              <p v-else class="unavailable-note">
                {{ t('messages.noAvailableLines') }}
              </p>
              <small v-if="newRecipientName">{{ newRecipientName }}</small>
            </div>
          </template>
          <template v-else-if="selectedThread">
            <ContactHeaderIdentity
              :name="displayNameForThread(selectedThread)"
              :number="selectedThread.peer"
              :avatar="avatarForNumber(selectedThread.peer)"
              :line="lineTagLine(lineForThread(selectedThread), selectedThread.line_id)"
              :line-fallback="threadLineFallback(selectedThread)"
            />
          </template>
          <div
            v-if="selectedThread && !composingNew"
            class="conversation-header__contact-actions"
          >
            <ContactNumberActions
              :number="selectedThread.peer"
              :contact="activeContact"
              compact
              @saved="contactSaved"
            />
          </div>
          <button
            v-if="selectedThread && !composingNew"
            class="icon-button"
            type="button"
            :disabled="Boolean(dialUnavailable) || !activeRecipient"
            :title="dialUnavailable || t('calls.dial')"
            @click="callCurrent"
          >
            <Phone :size="19" />
          </button>
        </header>

        <div class="messages-scroll">
          <p v-if="selectedReadError" class="message-read-error" role="alert">
            <span>{{ selectedReadError }}</span>
            <button type="button" @click="retryThreadRead">
              {{ t('common.retry') }}
            </button>
          </p>
          <StatePanel
            v-if="!composingNew && currentMessages?.status === 'loading'"
            state="loading"
            :title="t('messages.loadingConversation')"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'forbidden'"
            state="forbidden"
            :title="t('messages.conversationForbidden')"
            :detail="currentMessages.error"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'error'"
            state="error"
            :title="t('messages.conversationLoadFailed')"
            :detail="currentMessages.error"
            retryable
            @retry="selectedThread && openThread(selectedThread, true)"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'ready' && currentMessages.data.length === 0"
            state="empty"
            :title="t('messages.conversationEmpty')"
          />
          <div v-else-if="!composingNew" class="message-stack">
            <div
              v-for="message in currentMessages?.data || []"
              :key="message.id"
              class="message-row"
              :class="[
                `message-row--${message.direction}`,
                { 'is-arriving': recentIncomingMessageIDs[message.id] }
              ]"
            >
              <div class="message-bubble">
                <p>{{ message.content }}</p>
                <span>
                  {{ formatRelativeDate(message.timestamp) }}
                  <template v-if="message.direction === 'outgoing' && statusLabel(message.status)">
                    · {{ statusLabel(message.status) }}
                  </template>
                </span>
              </div>
            </div>
          </div>
          <div v-else class="new-message-empty">
            <MessageSquarePlus :size="30" />
            <strong>{{ t('dashboard.newMessage') }}</strong>
            <button
              v-if="existingRecipientThread"
              class="existing-thread-button"
              type="button"
              @click="viewExistingRecipientThread"
            >
              {{ t('messages.viewExistingConversation') }}
            </button>
          </div>
          <div ref="messagesEnd" />
        </div>

        <footer class="message-composer">
          <div class="composer-row">
            <textarea
              v-model="draft"
              rows="1"
              :placeholder="t('messages.enterMessage')"
              :disabled="Boolean(messageWriteUnavailable)"
              @keydown.enter.exact.prevent="submit"
            />
            <button
              class="send-button"
              type="button"
              :disabled="Boolean(sendDisabledReason) || sending"
              :title="sendDisabledReason || t('messages.send')"
              @click="submit"
            >
              <LoaderCircle v-if="sending" class="spin" :size="19" />
              <Send v-else :size="19" />
            </button>
          </div>
          <p v-if="messageWriteUnavailable" class="unavailable-note">
            {{ messageWriteUnavailable }}
          </p>
          <p v-else-if="sendError" class="field-error">{{ sendError }}</p>
        </footer>
      </template>

      <StatePanel
        v-else
        state="empty"
        :title="t('messages.selectConversation')"
        :detail="t('messages.detailPlaceholder')"
      />
    </article>
  </section>
</template>

<style scoped>
.messages-workspace.is-embedded {
  grid-template-columns: minmax(0, 1fr);
}

.pane-search {
  display: grid;
  gap: 8px;
}

.existing-thread-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  font-weight: 700;
}

.message-list-empty {
  display: flex;
  min-height: 220px;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
}

.message-list-empty :deep(.state-panel) {
  min-height: auto;
  flex: 0 0 auto;
}

.message-row.is-arriving {
  animation: incoming-message 520ms ease-out;
}

@keyframes incoming-message {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .message-row.is-arriving {
    animation: none;
  }
}

.conversation-header__contact-actions {
  display: flex;
  align-items: center;
}

.conversation-recipient {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
}

.conversation-recipient :deep(.suggest-input) {
  min-width: 0;
}

.conversation-recipient > small {
  grid-column: 1 / -1;
  margin-top: -2px;
}

.conversation-recipient > .unavailable-note {
  grid-column: 1 / -1;
}

.existing-thread-button {
  min-height: 34px;
  padding: 0 12px;
  color: var(--accent-strong);
  background: var(--surface);
  border: 1px solid var(--accent);
  border-radius: 6px;
}

.message-read-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0 0 14px;
  padding: 10px 12px;
  color: var(--danger);
  font-size: 13px;
  background: #fff4f4;
  border: 1px solid #f2caca;
  border-radius: 7px;
}

.message-read-error button {
  flex: 0 0 auto;
  color: var(--danger);
  font-weight: 700;
}

.pane-error {
  margin: 0 16px 8px;
}

</style>
