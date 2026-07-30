<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  LoaderCircle,
  Mail,
  MailOpen,
  MessageSquarePlus,
  Phone,
  Send,
  Star,
  Trash2,
  X
} from '@lucide/vue'
import BatchActionBar from '../components/BatchActionBar.vue'
import type { Contact, LineSummary, MessageThread } from '../api/types'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import InfiniteScrollTrigger from '../components/InfiniteScrollTrigger.vue'
import FavoriteFilterButton from '../components/FavoriteFilterButton.vue'
import ContactSuggestInput from '../components/ContactSuggestInput.vue'
import LineSelector from '../components/LineSelector.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import MessageThreadListItem from '../components/MessageThreadListItem.vue'
import SearchField from '../components/SearchField.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import { useListSelection } from '../composables/useListSelection'
import { requestConfirmation } from '../state/confirmation'
import { openDialer } from '../state/ui'
import { phoneDestination } from '../utils/format'
import {
  isContactPhoneCandidate,
  isOneWayMessageSender
} from '../utils/communicationAddress'
import {
  bootstrapResource,
  capabilityReason,
  contactForNumber,
  contactsResource,
  deleteMessageThread,
  deleteMessageThreads,
  displayPhoneNumber,
  lineForKey,
  lineKey,
  lineSupports,
  loadBootstrap,
  loadContacts,
  loadMessages,
  loadMoreMessages,
  loadMoreThreads,
  loadThreads,
  markThreadRead,
  markThreadsRead,
  markThreadsUnread,
  messagesFor,
  messagePaginationFor,
  recentIncomingMessageIDs,
  recentIncomingThreadKeys,
  resolveLine,
  setThreadsFavorite,
  sendMessage,
  threadReadErrors,
  threadIsUnread,
  threadsPagination,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate } from '../utils/format'
import { lineTagFallback, lineTagLine } from '../utils/lineIdentity'
import {
  findRecipientThread,
  messageReturnRoute,
  messageThreadUsesLine
} from './messages/messageFlow'
import {
  canAcknowledgeMessageThread,
  messageViewportIsAtBottom
} from './messages/messageReadVisibility'

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
const favoriteOnly = ref(
  !props.embeddedThreadKey &&
  !props.embeddedCompose &&
  route.query.favorite === '1'
)
const composingNew = ref(props.embeddedCompose)
const newRecipient = ref(props.initialRecipient)
const newRecipientName = ref(props.initialRecipientName)
const selectedLineKey = ref('')
const selectedRecipientPreferredLineID = ref('')
const composeContextLineKey = ref(props.contextLineKey)
const lineFilterKey = ref('all')
const draft = ref('')
const sending = ref(false)
const sendError = ref('')
const threadDeleteError = ref('')
const deletingThreadKey = ref('')
const messagesViewport = ref<HTMLElement | null>(null)
const messagesEnd = ref<HTMLElement | null>(null)
const viewportAtBottom = ref(false)
const retainedUnreadThreadKeys = ref(new Set<string>())
const manuallyUnreadThreadKeys = ref(new Set<string>())
const favoritePendingKey = ref('')
const batchBusy = ref(false)
const selection = useListSelection<MessageThread>(thread => thread.key)
const selecting = selection.active
const selectionCount = selection.count

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
const currentMessagePagination = computed(() =>
  selectedThread.value ? messagePaginationFor(selectedThread.value.key) : null
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
  composingNew.value ? selectedLine.value : activeLineForThread(selectedThread.value)
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
    if (favoriteOnly.value && !thread.favorite) return false
    if (
      messageFilter.value === 'unread' &&
      !threadIsUnread(thread) &&
      !retainedUnreadThreadKeys.value.has(thread.key)
    ) return false
    if (messageFilter.value === 'read' && threadIsUnread(thread)) return false
    if (!query) return true
    return (
      (thread.contact_name || '').toLocaleLowerCase().includes(query) ||
      (digits.length > 0 && thread.peer.replace(/\D/g, '').includes(digits)) ||
      (thread.last_content || '').toLocaleLowerCase().includes(query)
    )
  })
})
const batchThreads = computed(() => selection.selected(filteredThreads.value))
const batchHasUnread = computed(() => batchThreads.value.some(threadIsUnread))
const batchAllFavorite = computed(
  () =>
    batchThreads.value.length > 0 &&
    batchThreads.value.every(thread => thread.favorite)
)
const activeRecipient = computed(() =>
  composingNew.value ? newRecipient.value.trim() : selectedThread.value?.peer || ''
)
const activeRecipientIsContactable = computed(() =>
  isContactPhoneCandidate(activeRecipient.value)
)
const selectedThreadIsOneWay = computed(() =>
  Boolean(selectedThread.value && isOneWayMessageSender(selectedThread.value.peer))
)
const activeContact = computed(() =>
  activeRecipientIsContactable.value ? contactForNumber(activeRecipient.value) : undefined
)
const activeLineID = computed(() => (activeLine.value ? lineKey(activeLine.value) : ''))
const sendDisabledReason = computed(() => {
  if (messageWriteUnavailable.value) return messageWriteUnavailable.value
  if (selectedThreadIsOneWay.value) return t('messages.senderDoesNotAcceptReplies')
  if (!activeRecipient.value) return t('messages.selectRecipient')
  if (!activeLineID.value) return t('messages.selectLine')
  if (lineSupports(activeLine.value, 'message') === false) {
    return t('messages.lineUnsupported')
  }
  if (!draft.value.trim()) return t('messages.enterMessage')
  return ''
})

let lineSelectionOverridden = false
let composeReturnThreadKey = ''

function messageFilterFromRoute(value: unknown): MessageReadFilter {
  return value === 'unread' || value === 'read' ? value : 'all'
}

function messageFilterQuery(
  value = messageFilter.value
): { filter?: MessageReadFilter; favorite?: '1' } {
  return {
    ...(value === 'all' ? {} : { filter: value }),
    ...(favoriteOnly.value ? { favorite: '1' as const } : {})
  }
}

function setMessageFilter(value: MessageReadFilter): void {
  messageFilter.value = value
  if (embedded.value) return
  const query = { ...route.query }
  if (value === 'all') delete query.filter
  else query.filter = value
  void router.replace({ name: 'messages', params: route.params, query })
}

function setFavoriteFilter(value: boolean): void {
  favoriteOnly.value = value
  if (embedded.value) return
  const query = { ...route.query }
  if (value) query.favorite = '1'
  else delete query.favorite
  void router.replace({ name: 'messages', params: route.params, query })
}

function threadUsesLine(thread: MessageThread, line: LineSummary): boolean {
  return messageThreadUsesLine(thread, line)
}

function lineForThread(thread?: MessageThread): LineSummary | undefined {
  if (!thread) return undefined
  return lineForKey(thread.line_id)
}

function activeLineForThread(thread?: MessageThread): LineSummary | undefined {
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
  return (
    contactForNumber(thread.peer)?.display_name ||
    thread.contact_name ||
    threadDisplayNumber(thread)
  )
}

function threadDisplayNumber(thread: MessageThread): string {
  return displayPhoneNumber(thread.peer, thread.line_id)
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
    preferredLineID: selectedRecipientPreferredLineID.value,
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
      selectedRecipientPreferredLineID.value = ''
      composeContextLineKey.value =
        typeof route.query.line === 'string' ? route.query.line : ''
      lineSelectionOverridden = false
      syncComposeLine(true)
      sendError.value = ''
      return
    }
    composingNew.value = false
    selectedRecipientPreferredLineID.value = ''
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
  () => route.query.favorite,
  value => {
    if (!embedded.value) favoriteOnly.value = value === '1'
  }
)

watch([messageFilter, favoriteOnly], () => {
  retainedUnreadThreadKeys.value.clear()
  selection.clear()
})

watch([search, lineFilterKey], () => selection.clear())

watch(filteredThreads, threads => selection.reconcile(threads))

watch(
  () =>
    [
      selectedKey.value,
      selectedThread.value?.key || '',
      composingNew.value,
      threadsPagination.hasMore,
      threadsPagination.loadingMore
    ] as const,
  () => {
    if (composingNew.value || !selectedKey.value) return
    const thread = selectedThread.value
    if (thread) {
      void openThread(thread)
    } else if (threadsPagination.hasMore && !threadsPagination.loadingMore) {
      void loadMoreThreads()
    }
  },
  { immediate: true }
)

watch(
  () => currentMessages.value?.data,
  (messages, previousMessages) => {
    if (!messages || messages === previousMessages) return
    const followLatest =
      viewportAtBottom.value && messageDocumentIsReadable()
    void reconcileRenderedMessages(followLatest)
  }
)

function scrollToEnd(): void {
  void nextTick(() => {
    scrollMessageViewportToEnd()
    updateMessageViewportPosition()
  })
}

function scrollMessageViewportToEnd(): void {
  const viewport = messagesViewport.value
  if (viewport) {
    viewport.scrollTop = viewport.scrollHeight
    return
  }
  messagesEnd.value?.scrollIntoView({ block: 'end' })
}

function updateMessageViewportPosition(): void {
  const viewport = messagesViewport.value
  viewportAtBottom.value = Boolean(
    viewport && messageViewportIsAtBottom(viewport)
  )
}

function messageDocumentIsReadable(): boolean {
  return document.visibilityState === 'visible' && document.hasFocus()
}

async function reconcileRenderedMessages(followLatest: boolean): Promise<void> {
  await nextTick()
  if (followLatest && messageDocumentIsReadable()) {
    scrollMessageViewportToEnd()
  }
  updateMessageViewportPosition()
  await acknowledgeSelectedThreadRead()
}

async function openThread(thread: MessageThread, force = false): Promise<void> {
  const messages = await loadMessages(thread, force)
  if (!messages || composingNew.value || selectedKey.value !== thread.key) return

  await nextTick()
  scrollMessageViewportToEnd()
  updateMessageViewportPosition()
  await acknowledgeSelectedThreadRead(true)
}

async function acknowledgeSelectedThreadRead(retry = false): Promise<void> {
  const current = selectedThread.value
  if (
    !current ||
    (!retry && Boolean(threadReadErrors[current.key])) ||
    !canAcknowledgeMessageThread({
      selected: selectedKey.value === current.key,
      messagesReady: currentMessages.value?.status === 'ready',
      unread: threadIsUnread(current),
      composing: composingNew.value,
      manuallyUnread: manuallyUnreadThreadKeys.value.has(current.key),
      documentVisible: document.visibilityState === 'visible',
      windowFocused: document.hasFocus(),
      atBottom: viewportAtBottom.value
    })
  ) return
  await markThreadReadInView(current)
}

function onMessagesScroll(): void {
  updateMessageViewportPosition()
  if (viewportAtBottom.value) void acknowledgeSelectedThreadRead()
}

async function loadOlderMessages(): Promise<void> {
  const thread = selectedThread.value
  const viewport = messagesViewport.value
  if (!thread || !viewport) return
  const previousHeight = viewport.scrollHeight
  const previousTop = viewport.scrollTop
  await loadMoreMessages(thread)
  if (selectedThread.value?.key !== thread.key) return
  await nextTick()
  const currentViewport = messagesViewport.value
  if (!currentViewport) return
  currentViewport.scrollTop =
    previousTop + currentViewport.scrollHeight - previousHeight
  updateMessageViewportPosition()
}

function onMessageDocumentVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    void reconcileRenderedMessages(false)
  }
}

function onMessageWindowFocus(): void {
  void reconcileRenderedMessages(false)
}

function markThreadReadInView(thread: MessageThread): Promise<boolean> {
  if (messageFilter.value === 'unread') {
    retainedUnreadThreadKeys.value.add(thread.key)
  }
  return markThreadRead(thread)
}

async function markThreadUnreadInView(thread: MessageThread): Promise<void> {
  const alreadyManual = manuallyUnreadThreadKeys.value.has(thread.key)
  manuallyUnreadThreadKeys.value.add(thread.key)
  try {
    await markThreadsUnread([thread])
  } catch (error) {
    if (!alreadyManual) manuallyUnreadThreadKeys.value.delete(thread.key)
    throw error
  }
}

function toggleThreadRead(thread: MessageThread): Promise<boolean | void> {
  return threadIsUnread(thread)
    ? markThreadReadInView(thread)
    : markThreadUnreadInView(thread)
}

function retryThreadRead(): void {
  void acknowledgeSelectedThreadRead(true)
}

function chooseThread(key: string): void {
  manuallyUnreadThreadKeys.value.delete(key)
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

async function removeThread(thread: MessageThread): Promise<void> {
  const confirmed = await requestConfirmation({
    title: t('messages.deleteConfirmTitle'),
    message: t('messages.deleteConfirmMessage', {
      name: displayNameForThread(thread)
    }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  deletingThreadKey.value = thread.key
  threadDeleteError.value = ''
  try {
    await deleteMessageThread(thread)
    if (selectedKey.value === thread.key) {
      if (embedded.value) emit('close')
      else await router.replace({ name: 'messages', query: messageFilterQuery() })
    }
  } catch (error) {
    threadDeleteError.value =
      error instanceof Error ? error.message : t('messages.deleteFailed')
  } finally {
    deletingThreadKey.value = ''
  }
}

async function toggleFavorite(thread: MessageThread): Promise<void> {
  if (favoritePendingKey.value) return
  favoritePendingKey.value = thread.key
  threadDeleteError.value = ''
  try {
    await setThreadsFavorite([thread], !thread.favorite)
  } catch (error) {
    threadDeleteError.value =
      error instanceof Error ? error.message : t('messages.favoriteFailed')
  } finally {
    favoritePendingKey.value = ''
  }
}

async function batchSetRead(read: boolean): Promise<void> {
  if (batchBusy.value || batchThreads.value.length === 0) return
  batchBusy.value = true
  threadDeleteError.value = ''
  try {
    if (read) await markThreadsRead(batchThreads.value)
    else {
      const insertedManualKeys: string[] = []
      for (const thread of batchThreads.value) {
        if (!manuallyUnreadThreadKeys.value.has(thread.key)) {
          insertedManualKeys.push(thread.key)
        }
        manuallyUnreadThreadKeys.value.add(thread.key)
      }
      try {
        await markThreadsUnread(batchThreads.value)
      } catch (error) {
        for (const key of insertedManualKeys) {
          manuallyUnreadThreadKeys.value.delete(key)
        }
        throw error
      }
    }
  } catch (error) {
    threadDeleteError.value =
      error instanceof Error ? error.message : t('runtime.requestFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchSetFavorite(favorite: boolean): Promise<void> {
  if (batchBusy.value || batchThreads.value.length === 0) return
  batchBusy.value = true
  threadDeleteError.value = ''
  try {
    await setThreadsFavorite(batchThreads.value, favorite)
  } catch (error) {
    threadDeleteError.value =
      error instanceof Error ? error.message : t('messages.favoriteFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchDelete(): Promise<void> {
  const threads = batchThreads.value
  if (batchBusy.value || threads.length === 0) return
  const confirmed = await requestConfirmation({
    title: t('messages.deleteSelectedTitle'),
    message: t('messages.deleteSelectedMessage', { count: threads.length }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  batchBusy.value = true
  threadDeleteError.value = ''
  try {
    const deletedKeys = new Set(threads.map(thread => thread.key))
    await deleteMessageThreads(threads)
    if (deletedKeys.has(selectedKey.value)) {
      await router.replace({ name: 'messages', query: messageFilterQuery() })
    }
    selection.exit()
  } catch (error) {
    threadDeleteError.value =
      error instanceof Error ? error.message : t('messages.deleteFailed')
  } finally {
    batchBusy.value = false
  }
}

function toggleSelectionMode(): void {
  selection.toggleMode()
}

function onSelectionKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selection.active.value) selection.exit()
}

function startMessage(): void {
  if (messageWriteUnavailable.value) return
  composeReturnThreadKey = selectedThread.value?.key || ''
  composingNew.value = true
  newRecipient.value = ''
  newRecipientName.value = ''
  selectedRecipientPreferredLineID.value = ''
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

function chooseRecipient(suggestion: {
  contact: Contact
  phone: { number: string; normalized_number?: string }
}): void {
  newRecipient.value = phoneDestination(suggestion.phone)
  newRecipientName.value = suggestion.contact.display_name
  selectedRecipientPreferredLineID.value = suggestion.contact.preferred_line_id || ''
  syncComposeLine()
}

function editRecipient(value: string): void {
  newRecipient.value = value
  newRecipientName.value = ''
  selectedRecipientPreferredLineID.value = ''
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
  if (
    dialUnavailable.value ||
    !activeRecipient.value ||
    !activeRecipientIsContactable.value
  ) return
  openDialer(
    activeRecipient.value,
    composingNew.value ? newRecipientName.value : selectedThread.value?.contact_name || '',
    activeLineID.value
  )
}

function statusLabel(status: 'submitted' | 'delivered' | 'failed' | ''): string {
  if (status === 'submitted') return t('messages.submitted')
  if (status === 'delivered') return t('messages.delivered')
  if (status === 'failed') return t('messages.sendFailed')
  return ''
}

onMounted(() => {
  window.addEventListener('keydown', onSelectionKeydown)
  window.addEventListener('focus', onMessageWindowFocus)
  document.addEventListener(
    'visibilitychange',
    onMessageDocumentVisibilityChange
  )
  void Promise.all([loadBootstrap(), loadContacts(), loadThreads()])
  void reconcileRenderedMessages(false)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
  window.removeEventListener('focus', onMessageWindowFocus)
  document.removeEventListener(
    'visibilitychange',
    onMessageDocumentVisibilityChange
  )
})
</script>

<template>
  <section
    class="workspace messages-workspace"
    :class="{
      'has-selection': selectedThread || composingNew,
      'is-embedded': embedded,
      'is-batch-selecting': selecting
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
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="threadsResource.status !== 'ready' || threadsResource.data.length === 0"
            @toggle="toggleSelectionMode"
          />
          <SearchField v-model="search" :placeholder="t('common.search')" />
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
        <div class="message-filter-row">
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
          <FavoriteFilterButton
            :active="favoriteOnly"
            :label="t('messages.favoriteOnly')"
            @toggle="setFavoriteFilter(!favoriteOnly)"
          />
        </div>
      </div>
      <p
        v-if="
          threadsResource.status === 'ready' &&
          (threadsResource.error || threadDeleteError)
        "
        class="field-error pane-error"
        role="alert"
      >
        {{ threadDeleteError || threadsResource.error }}
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
      <div v-else class="item-list">
        <div
          v-if="
            filteredThreads.length === 0 &&
            !threadsPagination.hasMore &&
            !threadsPagination.loadingMore
          "
          class="message-list-empty"
        >
          <StatePanel
            state="empty"
            :title="
              search || messageFilter !== 'all' || favoriteOnly || lineFilterKey !== 'all'
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
        <template v-else>
          <SelectableListRow
            v-for="thread in filteredThreads"
            :key="thread.key"
            :active="selecting"
            :selected="selection.has(thread)"
            :label="t('common.selectItem', { name: displayNameForThread(thread) })"
            @toggle="selection.toggle(thread)"
          >
            <SwipeActionRow
              can-read
              :read-mode="threadIsUnread(thread) ? 'read' : 'unread'"
              :read-label="
                threadIsUnread(thread)
                  ? t('common.markRead')
                  : t('common.markUnread')
              "
              :delete-label="t('common.delete')"
              :disabled="
                selecting ||
                Boolean(deletingThreadKey) ||
                favoritePendingKey === thread.key
              "
              @read="toggleThreadRead(thread)"
              @delete="removeThread(thread)"
            >
              <MessageThreadListItem
                :thread="thread"
                :name="displayNameForThread(thread)"
                :peer="threadDisplayNumber(thread)"
                :avatar="avatarForNumber(thread.peer)"
                :line="lineTagLine(lineForThread(thread), thread.line_id)"
                :line-fallback="threadLineFallback(thread)"
                :selected="thread.key === selectedKey && !composingNew"
                :arriving="recentIncomingThreadKeys[thread.key]"
                :favorite-interactive="false"
                @select="chooseThread"
              />
            </SwipeActionRow>
          </SelectableListRow>
        </template>
        <InfiniteScrollTrigger
          :has-more="threadsPagination.hasMore"
          :loading="threadsPagination.loadingMore"
          :error="threadsPagination.error"
          :loading-label="t('messages.loading')"
          :retry-label="t('common.retry')"
          @load="loadMoreThreads"
        />
      </div>
      <BatchActionBar
        v-if="selecting"
        :selected="selectionCount"
        :total="filteredThreads.length"
        :selected-label="t('common.selectedCount', { count: selectionCount })"
        :select-all-label="t('common.selectAll')"
        :clear-all-label="t('common.clearAll')"
        :done-label="t('common.done')"
        :busy="batchBusy"
        @select-all="selection.selectAll(filteredThreads)"
        @done="selection.exit"
      >
        <button
          v-if="batchThreads.length > 0"
          type="button"
          :disabled="batchBusy"
          :title="batchHasUnread ? t('common.markRead') : t('common.markUnread')"
          @click="batchSetRead(batchHasUnread)"
        >
          <MailOpen v-if="batchHasUnread" :size="17" />
          <Mail v-else :size="17" />
          <span>
            {{ batchHasUnread ? t('common.markRead') : t('common.markUnread') }}
          </span>
        </button>
        <button
          v-if="batchThreads.length > 0"
          type="button"
          :disabled="batchBusy"
          :title="
            batchAllFavorite
              ? t('messages.unfavorite')
              : t('messages.favorite')
          "
          @click="batchSetFavorite(!batchAllFavorite)"
        >
          <Star
            :size="17"
            :fill="batchAllFavorite ? 'currentColor' : 'none'"
          />
          <span>
            {{
              batchAllFavorite
                ? t('messages.unfavorite')
                : t('messages.favorite')
            }}
          </span>
        </button>
        <button
          v-if="batchThreads.length > 0"
          class="is-danger"
          type="button"
          :disabled="batchBusy"
          :title="t('common.delete')"
          @click="batchDelete"
        >
          <Trash2 :size="17" />
          <span>{{ t('common.delete') }}</span>
        </button>
      </BatchActionBar>
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
                :model-value="newRecipient"
                :contacts="contactsResource.data"
                autofocus
                @update:model-value="editRecipient"
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
              channel="message"
              :name="displayNameForThread(selectedThread)"
              :number="threadDisplayNumber(selectedThread)"
              :avatar="avatarForNumber(selectedThread.peer)"
              :line="lineTagLine(lineForThread(selectedThread), selectedThread.line_id)"
              :line-fallback="threadLineFallback(selectedThread)"
            />
          </template>
          <div
            v-if="selectedThread && !composingNew && activeRecipientIsContactable"
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
            class="icon-button conversation-favorite-button"
            :class="{ 'is-active': selectedThread.favorite }"
            type="button"
            :disabled="Boolean(favoritePendingKey)"
            :title="
              selectedThread.favorite
                ? t('messages.unfavorite')
                : t('messages.favorite')
            "
            :aria-pressed="selectedThread.favorite"
            @click="toggleFavorite(selectedThread)"
          >
            <Star
              :size="18"
              :fill="selectedThread.favorite ? 'currentColor' : 'none'"
            />
          </button>
          <button
            v-if="selectedThread && !composingNew && activeRecipientIsContactable"
            class="icon-button"
            type="button"
            :disabled="Boolean(dialUnavailable) || !activeRecipient"
            :title="dialUnavailable || t('calls.dial')"
            @click="callCurrent"
          >
            <Phone :size="19" />
          </button>
          <button
            v-if="selectedThread && !composingNew"
            class="icon-button icon-button--danger desktop-delete-action"
            type="button"
            :disabled="Boolean(deletingThreadKey)"
            :title="t('messages.delete')"
            @click="removeThread(selectedThread)"
          >
            <Trash2 :size="18" />
          </button>
        </header>

        <div
          ref="messagesViewport"
          class="messages-scroll"
          @scroll="onMessagesScroll"
        >
          <p v-if="selectedReadError" class="message-read-error" role="alert">
            <span>{{ selectedReadError }}</span>
            <button type="button" @click="retryThreadRead">
              {{ t('common.retry') }}
            </button>
          </p>
          <InfiniteScrollTrigger
            v-if="
              !composingNew &&
              currentMessages?.status === 'ready' &&
              currentMessagePagination
            "
            :has-more="currentMessagePagination.hasMore"
            :loading="currentMessagePagination.loadingMore"
            :error="currentMessagePagination.error"
            :loading-label="t('messages.loadingConversation')"
            :retry-label="t('common.retry')"
            @load="loadOlderMessages"
          />
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
                  <template
                    v-if="
                      message.direction === 'outgoing' &&
                      statusLabel(message.delivery_status)
                    "
                  >
                    · {{ statusLabel(message.delivery_status) }}
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

        <footer v-if="!selectedThreadIsOneWay" class="message-composer">
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
        <footer v-else class="message-composer message-composer--readonly">
          <p class="unavailable-note">{{ t('messages.senderDoesNotAcceptReplies') }}</p>
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

.message-filter-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.message-filter-row .segmented-control {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.conversation-favorite-button:hover:not(:disabled),
.conversation-favorite-button.is-active {
  color: #a86400;
  background: transparent;
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

.message-composer--readonly .unavailable-note {
  margin: 0;
  text-align: center;
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

@media (max-width: 1100px) {
  .desktop-delete-action {
    display: none;
  }
}

</style>
