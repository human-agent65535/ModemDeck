<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { RouteLocationRaw } from 'vue-router'
import { History, MessageSquareText, Phone, UsersRound } from '@lucide/vue'
import {
  callsResource,
  contactsResource,
  displayPhoneNumber,
  ensureSearchData,
  threadsResource
} from '../state/workspace'
import { formatRelativeDate, primaryPhone } from '../utils/format'
import { messageThreadRoute } from '../router/messageRoute'
import SearchField from './SearchField.vue'

const router = useRouter()
const { t } = useI18n()
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

async function go(to: RouteLocationRaw): Promise<void> {
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
      :placeholder="t('shell.globalSearchPlaceholder')"
      :label="t('shell.globalSearch')"
      @focus="activate"
    />
    <div v-if="open && normalized" class="global-search__panel">
      <div v-if="loading && !hasResults" class="global-search__status">
        {{ t('shell.searching') }}
      </div>

      <template v-if="contactResults.length">
        <div class="search-group-label">
          <UsersRound :size="15" />{{ t('shell.contacts') }}
        </div>
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
        <div class="search-group-label">
          <MessageSquareText :size="15" />{{ t('shell.messages') }}
        </div>
        <button
          v-for="thread in threadResults"
          :key="thread.key"
          class="global-search__result"
          type="button"
          @click="go(messageThreadRoute(thread.key))"
        >
          <span>{{ thread.contact_name || displayPhoneNumber(thread.peer, thread.line_id) }}</span>
          <small>{{ thread.last_content || displayPhoneNumber(thread.peer, thread.line_id) }}</small>
        </button>
      </template>

      <template v-if="callResults.length">
        <div class="search-group-label">
          <History :size="15" />{{ t('shell.calls') }}
        </div>
        <button
          v-for="call in callResults"
          :key="call.id"
          class="global-search__result"
          type="button"
          @click="go({ name: 'calls', query: { selected: call.id } })"
        >
          <span>
            <Phone :size="14" />{{
              call.display_name || displayPhoneNumber(call.remote_number, call.line_id)
            }}
          </span>
          <small>{{ formatRelativeDate(call.started_at) }}</small>
        </button>
      </template>

      <div v-if="!loading && !hasResults" class="global-search__status">
        {{ t('shell.noSearchResults') }}
      </div>
    </div>
  </div>
</template>
