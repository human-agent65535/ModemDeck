<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { History, MessageSquareText, Phone, UsersRound } from '@lucide/vue'
import {
  callsResource,
  contactsResource,
  ensureSearchData,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate, primaryPhone } from '../utils/format'
import SearchField from './SearchField.vue'

const router = useRouter()
const root = ref<HTMLElement | null>(null)
const query = ref('')
const open = ref(false)

const normalized = computed(() => query.value.trim().toLocaleLowerCase())
const digits = computed(() => normalized.value.replace(/\D/g, ''))

const contactResults = computed(() =>
  contactsResource.data
    .filter(contact => {
      if (!normalized.value) return false
      return (
        contact.display_name.toLocaleLowerCase().includes(normalized.value) ||
        (digits.value.length > 0 &&
          contact.phones.some(phone => phone.number.replace(/\D/g, '').includes(digits.value)))
      )
    })
    .slice(0, 4)
)

const threadResults = computed(() =>
  threadsResource.data
    .filter(thread => {
      if (!normalized.value) return false
      return (
        (thread.contact_name || '').toLocaleLowerCase().includes(normalized.value) ||
        (digits.value.length > 0 && thread.peer.replace(/\D/g, '').includes(digits.value)) ||
        (thread.last_content || '').toLocaleLowerCase().includes(normalized.value)
      )
    })
    .slice(0, 4)
)

const callResults = computed(() =>
  callsResource.data
    .filter(call => {
      if (!normalized.value) return false
      return (
        (call.display_name || '').toLocaleLowerCase().includes(normalized.value) ||
        (digits.value.length > 0 && call.remote_number.replace(/\D/g, '').includes(digits.value))
      )
    })
    .slice(0, 4)
)

const loading = computed(
  () =>
    contactsResource.status === 'loading' ||
    threadsResource.status === 'loading' ||
    callsResource.status === 'loading'
)
const hasResults = computed(
  () => contactResults.value.length + threadResults.value.length + callResults.value.length > 0
)

function activate(): void {
  open.value = true
  void ensureSearchData()
}

function close(): void {
  open.value = false
}

async function go(to: string | { name: string; params?: Record<string, string>; query?: Record<string, string> }): Promise<void> {
  query.value = ''
  close()
  await router.push(to)
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) close()
}

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))
</script>

<template>
  <div ref="root" class="global-search" @keydown.esc="close">
    <SearchField
      v-model="query"
      placeholder="搜索姓名、号码、消息或通话"
      label="全局搜索"
      @focus="activate"
    />
    <div v-if="open && normalized" class="global-search__panel">
      <div v-if="loading && !hasResults" class="global-search__status">正在搜索…</div>

      <template v-if="contactResults.length">
        <div class="search-group-label"><UsersRound :size="15" />联系人</div>
        <button
          v-for="contact in contactResults"
          :key="contact.id"
          class="global-search__result"
          type="button"
          @click="go({ name: 'contacts', params: { contactId: contact.id } })"
        >
          <span>{{ contact.display_name }}</span>
          <small>{{ primaryPhone(contact.phones) }}</small>
        </button>
      </template>

      <template v-if="threadResults.length">
        <div class="search-group-label"><MessageSquareText :size="15" />消息</div>
        <button
          v-for="thread in threadResults"
          :key="thread.key"
          class="global-search__result"
          type="button"
          @click="go({ name: 'messages', params: { threadKey: thread.key } })"
        >
          <span>{{ thread.contact_name || thread.peer }}</span>
          <small>{{ thread.last_content || thread.peer }}</small>
        </button>
      </template>

      <template v-if="callResults.length">
        <div class="search-group-label"><History :size="15" />通话</div>
        <button
          v-for="call in callResults"
          :key="call.id"
          class="global-search__result"
          type="button"
          @click="go({ name: 'calls', query: { selected: call.id } })"
        >
          <span><Phone :size="14" />{{ call.display_name || call.remote_number }}</span>
          <small>{{ formatRelativeDate(call.started_at) }}</small>
        </button>
      </template>

      <div v-if="!loading && !hasResults" class="global-search__status">没有匹配结果</div>
    </div>
  </div>
</template>
