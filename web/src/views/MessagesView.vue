<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, LoaderCircle, MessageSquarePlus, Phone, Send } from '@lucide/vue'
import type { Contact } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactSuggestInput from '../components/ContactSuggestInput.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  capabilityReason,
  contactsResource,
  lineKey,
  lineLabel,
  lineSupports,
  loadBootstrap,
  loadContacts,
  loadMessages,
  loadThreads,
  messagesFor,
  sendMessage,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate } from '../utils/format'

const route = useRoute()
const router = useRouter()
const search = ref('')
const composingNew = ref(false)
const newRecipient = ref('')
const newRecipientName = ref('')
const selectedLineKey = ref('')
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
const lines = computed(() => bootstrapResource.data?.lines || [])
const selectedLine = computed(() => lines.value.find(line => lineKey(line) === selectedLineKey.value))
const activeLine = computed(() =>
  composingNew.value
    ? selectedLine.value
    : lines.value.find(line => line.iccid && line.iccid === selectedThread.value?.iccid)
)
const messageUnavailable = computed(() => capabilityReason('message'))
const messageWriteUnavailable = computed(() =>
  threadsResource.status === 'forbidden' ? '当前账户无权发送消息' : messageUnavailable.value
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const filteredThreads = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  return threadsResource.data.filter(thread => {
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
const activeICCID = computed(() => {
  if (!composingNew.value) return selectedThread.value?.iccid || ''
  return selectedLine.value?.iccid || ''
})
const activeLineID = computed(() => (activeLine.value ? lineKey(activeLine.value) : ''))
const sendDisabledReason = computed(() => {
  if (messageWriteUnavailable.value) return messageWriteUnavailable.value
  if (!activeRecipient.value) return '请选择联系人或输入号码'
  if (!activeLineID.value && !activeICCID.value) return '请选择线路'
  if (lineSupports(activeLine.value, 'message') === false) return '所选线路不支持发送消息'
  if (!draft.value.trim()) return '请输入消息'
  return ''
})

watch(
  lines,
  value => {
    if (!value.some(line => lineKey(line) === selectedLineKey.value)) {
      selectedLineKey.value = ''
    }
  },
  { immediate: true }
)

watch(
  () => [route.params.threadKey, route.query.compose] as const,
  ([routeThreadKey, compose]) => {
    if (typeof compose === 'string') {
      composingNew.value = true
      newRecipient.value = compose
      newRecipientName.value = typeof route.query.name === 'string' ? route.query.name : ''
      sendError.value = ''
      return
    }
    composingNew.value = false
    if (typeof routeThreadKey === 'string' && routeThreadKey) {
      const thread = threadsResource.data.find(item => item.key === routeThreadKey)
      if (thread) void loadMessages(thread).then(() => scrollToEnd())
    }
  },
  { immediate: true }
)

watch(
  () => threadsResource.status,
  status => {
    if (status !== 'ready' || !selectedKey.value) return
    const thread = selectedThread.value
    if (thread) void loadMessages(thread).then(() => scrollToEnd())
  }
)

watch(
  () => currentMessages.value?.data.length,
  () => scrollToEnd()
)

function scrollToEnd(): void {
  void nextTick(() => messagesEnd.value?.scrollIntoView({ block: 'end' }))
}

function chooseThread(key: string): void {
  composingNew.value = false
  draft.value = ''
  sendError.value = ''
  void router.push({ name: 'messages', params: { threadKey: key } })
}

function startMessage(): void {
  if (messageWriteUnavailable.value) return
  composingNew.value = true
  newRecipient.value = ''
  newRecipientName.value = ''
  draft.value = ''
  sendError.value = ''
  void router.push({ name: 'messages', query: { compose: '' } })
}

function chooseRecipient(suggestion: { contact: Contact; phone: { number: string } }): void {
  newRecipient.value = suggestion.phone.number
  newRecipientName.value = suggestion.contact.display_name
}

function backToList(): void {
  composingNew.value = false
  void router.push({ name: 'messages' })
}

async function submit(): Promise<void> {
  if (sendDisabledReason.value || sending.value) return
  sending.value = true
  sendError.value = ''
  try {
    const sent = await sendMessage({
      thread_key: selectedThread.value?.key,
      line_id: activeLineID.value || undefined,
      iccid: activeICCID.value || undefined,
      to: activeRecipient.value,
      content: draft.value.trim()
    })
    draft.value = ''
    const key = `${sent.iccid}|${sent.peer}`
    if (composingNew.value) {
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
    composingNew.value ? newRecipientName.value : selectedThread.value?.contact_name || ''
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
          class="icon-button"
          type="button"
          :disabled="Boolean(messageWriteUnavailable)"
          :title="messageWriteUnavailable || '新消息'"
          @click="startMessage"
        >
          <MessageSquarePlus :size="19" />
        </button>
      </header>
      <div class="pane-search">
        <SearchField v-model="search" placeholder="搜索对话" />
      </div>

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
      <StatePanel
        v-else-if="filteredThreads.length === 0"
        state="empty"
        :title="search ? '没有匹配的对话' : '还没有消息'"
      />
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
              <small>{{ thread.last_content || thread.peer }}</small>
              <b v-if="thread.unread_count">{{ thread.unread_count }}</b>
            </span>
          </span>
        </button>
      </div>
    </aside>

    <article class="detail-pane conversation-pane">
      <template v-if="selectedThread || composingNew">
        <header class="conversation-header">
          <button class="icon-button mobile-back" type="button" title="返回消息" @click="backToList">
            <ArrowLeft :size="20" />
          </button>
          <template v-if="composingNew">
            <div class="conversation-recipient">
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
            @retry="selectedThread && loadMessages(selectedThread, true)"
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
          </div>
          <div ref="messagesEnd" />
        </div>

        <footer class="message-composer">
          <label v-if="composingNew && lines.length > 0" class="compact-select">
            <span>线路</span>
            <select v-model="selectedLineKey" aria-label="消息线路">
              <option value="" disabled>选择线路</option>
              <option v-for="line in lines" :key="lineKey(line)" :value="lineKey(line)">
                {{ lineLabel(line) }}{{
                  lineSupports(line, 'message') === false ? ' · 不支持消息' : ''
                }}
              </option>
            </select>
          </label>
          <p v-else-if="composingNew" class="unavailable-note">没有可用线路</p>
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
