<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, LoaderCircle, MessageSquarePlus, Phone, Send, X } from '@lucide/vue'
import type { Contact, LineSummary, MessageThread } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactSuggestInput from '../components/ContactSuggestInput.vue'
import LineSelector from '../components/LineSelector.vue'
import LineTag from '../components/LineTag.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  capabilityReason,
  contactsResource,
  lineKey,
  lineSupports,
  loadBootstrap,
  loadContacts,
  loadMessages,
  loadThreads,
  markThreadRead,
  messagesFor,
  resolveLine,
  sendMessage,
  threadReadErrors,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate } from '../utils/format'
import {
  createLineLookup,
  findLine,
  lineTagFallback,
  lineTagLine
} from '../utils/lineIdentity'
import {
  findRecipientThread,
  messageReturnRoute,
  messageThreadUsesLine
} from './messages/messageFlow'

const route = useRoute()
const router = useRouter()
const search = ref('')
const composingNew = ref(false)
const newRecipient = ref('')
const newRecipientName = ref('')
const selectedLineKey = ref('')
const composeContextLineKey = ref('')
const lineFilterKey = ref('all')
const draft = ref('')
const sending = ref(false)
const sendError = ref('')
const messagesEnd = ref<HTMLElement | null>(null)

const selectedKey = computed(() => String(route.params.threadKey || ''))
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
const defaultLineDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
)
const selectedLine = computed(() => lines.value.find(line => lineKey(line) === selectedLineKey.value))
const activeLine = computed(() => selectedLine.value)
const lineLookup = computed(() => createLineLookup(lines.value))
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
  threadsResource.status === 'forbidden' ? '当前账户无权发送消息' : messageUnavailable.value
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const filteredThreads = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  const filteredLine =
    lineFilterKey.value === 'all'
      ? undefined
      : lines.value.find(line => lineKey(line) === lineFilterKey.value)
  return threadsResource.data.filter(thread => {
    if (filteredLine && !threadUsesLine(thread, filteredLine)) return false
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
const activeICCID = computed(() => activeLine.value?.iccid || '')
const activeLineID = computed(() => (activeLine.value ? lineKey(activeLine.value) : ''))
const sendDisabledReason = computed(() => {
  if (messageWriteUnavailable.value) return messageWriteUnavailable.value
  if (!activeRecipient.value) return '请选择联系人或输入号码'
  if (!activeLineID.value && !activeICCID.value) return '请选择线路'
  if (lineSupports(activeLine.value, 'message') === false) return '所选线路不支持发送消息'
  if (!draft.value.trim()) return '请输入消息'
  return ''
})

let lineSelectionOverridden = false
let replyLineThreadKey = ''
let openedThreadKey = ''
let attemptedReadKey = ''
let composeReturnThreadKey = ''

function threadUsesLine(thread: MessageThread, line: LineSummary): boolean {
  return messageThreadUsesLine(thread, line)
}

function lineForThread(thread?: MessageThread): LineSummary | undefined {
  if (!thread) return undefined
  return findLine(lineLookup.value, thread.line_id, thread.iccid)
}

function threadLineFallback(thread: MessageThread): string {
  return lineTagFallback(
    lineForThread(thread),
    lines.value,
    defaultLineDeviceIMEI.value,
    thread.line_id,
    thread.iccid
  )
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
  [lines, defaultLineDeviceIMEI, () => contactsResource.data, newRecipient, composingNew],
  () => syncComposeLine()
)

watch(
  [selectedThread, lines, composingNew],
  () => {
    if (composingNew.value) {
      replyLineThreadKey = ''
      return
    }
    const thread = selectedThread.value
    if (!thread) {
      replyLineThreadKey = ''
      selectedLineKey.value = ''
      return
    }
    const selectedStillExists = lines.value.some(line => lineKey(line) === selectedLineKey.value)
    if (replyLineThreadKey === thread.key && selectedStillExists) return

    replyLineThreadKey = thread.key
    const threadLine = lineForThread(thread)
    selectedLineKey.value = threadLine ? lineKey(threadLine) : ''
  },
  { immediate: true }
)

watch(
  () => [route.params.threadKey, route.query.compose] as const,
  ([, compose]) => {
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
  void router.push({ name: 'messages', params: { threadKey: key } })
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
  void router.push({ name: 'messages', query: { compose: '' } })
}

function chooseRecipient(suggestion: { contact: Contact; phone: { number: string } }): void {
  newRecipient.value = suggestion.phone.number
  newRecipientName.value = suggestion.contact.display_name
  syncComposeLine()
}

function backToList(): void {
  composingNew.value = false
  const destination = messageReturnRoute(composeReturnThreadKey)
  composeReturnThreadKey = ''
  void router.push(destination)
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
    const threadKey = replyThreadKey.value
    const sent = await sendMessage({
      thread_key: threadKey,
      line_id: activeLineID.value || undefined,
      iccid: activeICCID.value || undefined,
      to: activeRecipient.value,
      content: draft.value.trim()
    })
    draft.value = ''
    const key = `${sent.iccid}|${sent.peer}`
    if (composingNew.value || key !== threadKey) {
      composingNew.value = false
      await router.replace({ name: 'messages', params: { threadKey: key } })
      const thread = threadsResource.data.find(item => item.key === key)
      if (thread) await loadMessages(thread)
    }
    scrollToEnd()
  } catch (error) {
    sendError.value = error instanceof Error ? error.message : '消息发送失败'
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
  if (status === 2) return '已发送'
  if (status === 3) return '发送失败'
  return ''
}

onMounted(() => {
  void Promise.all([loadBootstrap(), loadContacts(), loadThreads()])
})
</script>

<template>
  <section class="workspace" :class="{ 'has-selection': selectedThread || composingNew }">
    <aside class="list-pane">
      <header class="pane-header">
        <div>
          <h1>消息</h1>
          <span v-if="threadsResource.status === 'ready'">{{ threadsResource.data.length }}</span>
        </div>
        <button
          class="new-message-button"
          type="button"
          :disabled="Boolean(messageWriteUnavailable)"
          :title="messageWriteUnavailable || '新消息'"
          @click="startMessage"
        >
          <MessageSquarePlus :size="19" />
          <span>新消息</span>
        </button>
      </header>
      <div class="pane-search">
        <SearchField v-model="search" placeholder="搜索对话" />
        <LineSelector
          v-if="lines.length > 1"
          v-model="lineFilterKey"
          class="message-line-filter"
          :lines="lines"
          :default-device-imei="defaultLineDeviceIMEI"
          label="消息线路"
          include-all
          all-label="全部线路"
          all-description="显示所有模组的对话"
        />
      </div>
      <p
        v-if="threadsResource.status === 'ready' && threadsResource.error"
        class="field-error pane-error"
        role="alert"
      >
        {{ threadsResource.error }}
      </p>

      <StatePanel v-if="threadsResource.status === 'loading'" state="loading" title="正在载入消息" />
      <StatePanel
        v-else-if="threadsResource.status === 'forbidden'"
        state="forbidden"
        title="无权查看消息"
        :detail="threadsResource.error"
      />
      <StatePanel
        v-else-if="threadsResource.status === 'error'"
        state="error"
        title="无法载入消息"
        :detail="threadsResource.error"
        retryable
        @retry="loadThreads(true)"
      />
      <div v-else-if="filteredThreads.length === 0" class="message-list-empty">
        <StatePanel
          state="empty"
          :title="search ? '没有匹配的对话' : '还没有消息'"
        />
        <button
          v-if="!search && !messageWriteUnavailable"
          class="secondary-button"
          type="button"
          @click="startMessage"
        >
          <MessageSquarePlus :size="17" />
          新消息
        </button>
      </div>
      <div v-else class="item-list">
        <button
          v-for="thread in filteredThreads"
          :key="thread.key"
          class="list-item list-item--thread"
          :class="{ 'is-selected': thread.key === selectedKey && !composingNew }"
          type="button"
          @click="chooseThread(thread.key)"
        >
          <BaseAvatar :name="thread.contact_name || thread.peer" />
          <span class="list-item__content">
            <span class="list-item__title">
              <strong>{{ thread.contact_name || thread.peer }}</strong>
              <time>{{ formatRelativeDate(thread.last_timestamp) }}</time>
            </span>
            <span class="list-item__preview">
              <span class="message-thread-meta">
                <LineTag
                  :line="lineTagLine(lineForThread(thread), thread.line_id, thread.iccid)"
                  :fallback="threadLineFallback(thread)"
                />
                <small>{{ thread.last_content || thread.peer }}</small>
              </span>
              <b v-if="thread.unread_count">{{ thread.unread_count }}</b>
            </span>
          </span>
        </button>
      </div>
    </aside>

    <article class="detail-pane conversation-pane">
      <template v-if="selectedThread || composingNew">
        <header class="conversation-header">
          <button
            class="icon-button"
            :class="{ 'mobile-back': !composingNew }"
            type="button"
            :title="composingNew ? '取消新消息' : '返回消息'"
            @click="backToList"
          >
            <X v-if="composingNew" :size="20" />
            <ArrowLeft v-else :size="20" />
          </button>
          <template v-if="composingNew">
            <div class="conversation-recipient">
              <span class="conversation-recipient__label">收件人</span>
              <ContactSuggestInput
                v-model="newRecipient"
                :contacts="contactsResource.data"
                autofocus
                @select="chooseRecipient"
              />
              <small v-if="newRecipientName">{{ newRecipientName }}</small>
            </div>
          </template>
          <template v-else-if="selectedThread">
            <BaseAvatar :name="selectedThread.contact_name || selectedThread.peer" size="small" />
            <div class="conversation-title">
              <h2>{{ selectedThread.contact_name || selectedThread.peer }}</h2>
              <span v-if="selectedThread.contact_name">{{ selectedThread.peer }}</span>
              <LineTag
                class="conversation-line-tag"
                :line="lineTagLine(lineForThread(selectedThread), selectedThread.line_id, selectedThread.iccid)"
                :fallback="threadLineFallback(selectedThread)"
              />
            </div>
          </template>
          <button
            class="icon-button"
            type="button"
            :disabled="Boolean(dialUnavailable) || !activeRecipient"
            :title="dialUnavailable || '拨打'"
            @click="callCurrent"
          >
            <Phone :size="19" />
          </button>
        </header>

        <div class="messages-scroll">
          <p v-if="selectedReadError" class="message-read-error" role="alert">
            <span>{{ selectedReadError }}</span>
            <button type="button" @click="retryThreadRead">重试</button>
          </p>
          <StatePanel
            v-if="!composingNew && currentMessages?.status === 'loading'"
            state="loading"
            title="正在载入对话"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'forbidden'"
            state="forbidden"
            title="无权查看这段对话"
            :detail="currentMessages.error"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'error'"
            state="error"
            title="无法载入对话"
            :detail="currentMessages.error"
            retryable
            @retry="selectedThread && openThread(selectedThread, true)"
          />
          <StatePanel
            v-else-if="!composingNew && currentMessages?.status === 'ready' && currentMessages.data.length === 0"
            state="empty"
            title="这段对话还没有消息"
          />
          <div v-else-if="!composingNew" class="message-stack">
            <div
              v-for="message in currentMessages?.data || []"
              :key="message.id"
              class="message-row"
              :class="`message-row--${message.direction}`"
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
            <strong>新消息</strong>
            <button
              v-if="existingRecipientThread"
              class="existing-thread-button"
              type="button"
              @click="viewExistingRecipientThread"
            >
              查看该线路上的已有对话
            </button>
          </div>
          <div ref="messagesEnd" />
        </div>

        <footer class="message-composer">
          <LineSelector
            v-if="lines.length > 0"
            v-model="selectedLineKey"
            class="message-line-select"
            :lines="lines"
            :default-device-imei="defaultLineDeviceIMEI"
            label="发送线路"
            placement="up"
            capability="message"
            unavailable-label="不支持消息"
            @change="changeSendingLine"
          />
          <p v-else class="unavailable-note">没有可用线路</p>
          <div class="composer-row">
            <textarea
              v-model="draft"
              rows="1"
              placeholder="输入消息"
              :disabled="Boolean(messageWriteUnavailable)"
              @keydown.enter.exact.prevent="submit"
            />
            <button
              class="send-button"
              type="button"
              :disabled="Boolean(sendDisabledReason) || sending"
              :title="sendDisabledReason || '发送'"
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

      <StatePanel v-else state="empty" title="选择一段对话" detail="消息会显示在这里" />
    </article>
  </section>
</template>

<style scoped>
.pane-search {
  display: grid;
  gap: 8px;
}

.new-message-button,
.existing-thread-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  font-weight: 700;
}

.new-message-button {
  min-height: 36px;
  padding: 0 11px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 6px;
}

.new-message-button:disabled {
  color: var(--faint);
  background: var(--surface-subtle);
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

.message-line-select {
  margin-bottom: 10px;
}

.message-thread-meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.message-thread-meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conversation-line-tag {
  margin-top: 3px;
}

.conversation-recipient {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
}

.conversation-recipient__label {
  color: var(--muted);
  font-size: 13px;
  font-weight: 700;
}

.conversation-recipient > small {
  grid-column: 2;
  margin-top: -2px;
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
