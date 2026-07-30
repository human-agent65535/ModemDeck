<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  MessageSquareText,
  Pencil,
  Phone,
  Star,
  Trash2,
  UserPlus
} from '@lucide/vue'
import type { Contact, ContactInput } from '../api/types'
import BatchActionBar from '../components/BatchActionBar.vue'
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactEditor from '../components/ContactEditor.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import InfiniteScrollTrigger from '../components/InfiniteScrollTrigger.vue'
import ListItemAvatarStatus from '../components/ListItemAvatarStatus.vue'
import ListItemStatusRail from '../components/ListItemStatusRail.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import SearchField from '../components/SearchField.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import { useListSelection } from '../composables/useListSelection'
import { requestConfirmation } from '../state/confirmation'
import { openDialer } from '../state/ui'
import {
  capabilityReason,
  bootstrapResource,
  contactsResource,
  contactsPagination,
  contactEditingAvailable,
  deleteContact,
  deleteContacts,
  lineKey,
  lineLabel,
  loadBootstrap,
  loadContacts,
  loadMoreContacts,
  saveContact
} from '../state/workspace'
import { phoneDestination, primaryPhone } from '../utils/format'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const search = ref('')
const editorOpen = ref(false)
const editing = ref<Contact | undefined>()
const saving = ref(false)
const editorError = ref('')
const deleting = ref(false)
const deleteError = ref('')
const favoritePending = ref(false)
const favoriteError = ref('')
const batchBusy = ref(false)
const selection = useListSelection<Contact>(contact => contact.id)
const selecting = selection.active
const selectionCount = selection.count

const filteredContacts = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const digits = query.replace(/\D/g, '')
  return contactsResource.data
    .filter(contact => {
      if (!query) return true
      return (
        contact.display_name.toLocaleLowerCase().includes(query) ||
        (digits.length > 0 &&
          contact.phones.some(phone => phone.number.replace(/\D/g, '').includes(digits)))
      )
    })
    .slice()
    .sort((a, b) => a.display_name.localeCompare(b.display_name))
})

const selectedId = computed(() => String(route.params.contactId || ''))
const selected = computed(() => contactsResource.data.find(contact => contact.id === selectedId.value))
const messageUnavailable = computed(() => capabilityReason('message'))
const dialUnavailable = computed(() => capabilityReason('dial'))
const searchedPhone = computed(() => {
  const value = search.value.trim()
  const digits = value.replace(/\D/g, '')
  return digits.length >= 3 && /^[+\d\s().-]+$/.test(value) ? value : ''
})
const contactLines = computed(
  () => bootstrapResource.data?.lines.filter(line => Boolean(lineKey(line))) || []
)
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const batchContacts = computed(() => selection.selected(filteredContacts.value))

watch(
  [
    () => contactsResource.status,
    selectedId,
    selected,
    () => contactsPagination.hasMore,
    () => contactsPagination.loadingMore
  ],
  ([status, id, contact, hasMore, loadingMore]) => {
    if (status !== 'ready' || !id || contact) return
    if (hasMore) {
      if (!loadingMore) void loadMoreContacts()
    } else {
      void router.replace({ name: 'contacts' })
    }
  }
)

watch(search, () => selection.clear())
watch(filteredContacts, contacts => selection.reconcile(contacts))

function selectContact(contact: Contact): void {
  void router.push({ name: 'contacts', params: { contactId: contact.id } })
}

function backToList(): void {
  void router.push({ name: 'contacts' })
}

function openNew(): void {
  editing.value = undefined
  editorError.value = ''
  editorOpen.value = true
}

function openEdit(contact: Contact): void {
  editing.value = contact
  editorError.value = ''
  editorOpen.value = true
}

async function save(input: ContactInput): Promise<void> {
  saving.value = true
  editorError.value = ''
  try {
    const contact = await saveContact(input, editing.value?.id)
    editorOpen.value = false
    await router.push({ name: 'contacts', params: { contactId: contact.id } })
  } catch (error) {
    editorError.value = error instanceof Error ? error.message : t('contacts.saveFailed')
  } finally {
    saving.value = false
  }
}

async function remove(contact: Contact): Promise<void> {
  const confirmed = await requestConfirmation({
    title: t('contacts.deleteConfirmTitle'),
    message: t('contacts.deleteConfirmMessage', { name: contact.display_name }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  deleting.value = true
  deleteError.value = ''
  try {
    await deleteContact(contact)
    if (selectedId.value === contact.id) {
      await router.replace({ name: 'contacts' })
    }
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('contacts.deleteFailed')
  } finally {
    deleting.value = false
  }
}

async function batchDelete(): Promise<void> {
  const contacts = batchContacts.value
  if (batchBusy.value || contacts.length === 0) return
  const confirmed = await requestConfirmation({
    title: t('contacts.deleteSelectedTitle'),
    message: t('contacts.deleteSelectedMessage', { count: contacts.length }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  batchBusy.value = true
  deleteError.value = ''
  try {
    const deleted = new Set(contacts.map(contact => contact.id))
    await deleteContacts(contacts)
    if (deleted.has(selectedId.value)) {
      await router.replace({ name: 'contacts' })
    }
    selection.exit()
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('contacts.deleteFailed')
  } finally {
    batchBusy.value = false
  }
}

function onSelectionKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selection.active.value) selection.exit()
}

async function toggleFavorite(contact: Contact): Promise<void> {
  if (favoritePending.value) return
  favoritePending.value = true
  favoriteError.value = ''
  try {
    await saveContact(
      {
        display_name: contact.display_name,
        avatar: contact.avatar,
        favorite: !contact.favorite,
        notes: contact.notes,
        preferred_line_id: contact.preferred_line_id,
        revision: contact.revision,
        phones: contact.phones.map(phone => ({
          id: phone.id,
          label: phone.label,
          number: phone.number,
          region: phone.region,
          primary: phone.primary
        }))
      },
      contact.id
    )
  } catch (error) {
    favoriteError.value =
      error instanceof Error ? error.message : t('contacts.favoriteSaveFailed')
  } finally {
    favoritePending.value = false
  }
}

function call(contact: Contact, number: string): void {
  if (dialUnavailable.value) return
  openDialer(number, contact.display_name, contact.preferred_line_id || '')
}

function message(contact: Contact, number: string): void {
  if (messageUnavailable.value) return
  void router.push({
    name: 'messages',
    query: {
      compose: number,
      name: contact.display_name,
      ...(contact.preferred_line_id ? { line: contact.preferred_line_id } : {})
    }
  })
}

function openSavedContact(contact: Contact): void {
  search.value = ''
  selectContact(contact)
}

function preferredLineName(contact: Contact): string {
  const line = contactLines.value.find(item => lineKey(item) === contact.preferred_line_id)
  return line ? lineLabel(line) : contact.preferred_line_id || ''
}

onMounted(() => {
  window.addEventListener('keydown', onSelectionKeydown)
  void loadBootstrap()
  void loadContacts()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
})
</script>

<template>
  <section
    class="workspace"
    :class="{
      'has-selection': selected,
      'is-batch-selecting': selecting
    }"
  >
    <aside class="list-pane">
      <header class="pane-header">
        <div>
          <h1>{{ t('shell.contacts') }}</h1>
          <span v-if="contactsResource.status === 'ready'">{{ contactsResource.data.length }}</span>
        </div>
        <button
          v-if="contactEditingAvailable"
          class="pane-create-button mobile-list-fab"
          type="button"
          :title="t('contacts.new')"
          :aria-label="t('contacts.new')"
          @click="openNew"
        >
          <UserPlus :size="19" />
          <span>{{ t('contacts.new') }}</span>
        </button>
      </header>
      <div class="pane-search">
        <div class="pane-search-row">
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="contactsResource.status !== 'ready' || contactsResource.data.length === 0"
            @toggle="selection.toggleMode"
          />
          <SearchField v-model="search" :placeholder="t('common.search')" />
        </div>
      </div>
      <p v-if="deleteError" class="field-error contact-delete-error" role="alert">
        {{ deleteError }}
      </p>

      <StatePanel
        v-if="contactsResource.status === 'loading'"
        state="loading"
        :title="t('contacts.loading')"
      />
      <StatePanel
        v-else-if="contactsResource.status === 'forbidden'"
        state="forbidden"
        :title="t('contacts.forbidden')"
        :detail="contactsResource.error"
      />
      <StatePanel
        v-else-if="contactsResource.status === 'error'"
        state="error"
        :title="t('contacts.loadFailed')"
        :detail="contactsResource.error"
        retryable
        @retry="loadContacts(true)"
      />
      <div v-else class="item-list" role="list">
        <div
          v-if="
            filteredContacts.length === 0 &&
            !contactsPagination.hasMore &&
            !contactsPagination.loadingMore
          "
          class="contact-empty-state"
        >
          <StatePanel
            state="empty"
            :title="search ? t('contacts.noMatches') : t('contacts.empty')"
          />
          <ContactNumberActions
            v-if="searchedPhone"
            :number="searchedPhone"
            @saved="openSavedContact"
          />
        </div>
        <template v-else>
          <SelectableListRow
            v-for="contact in filteredContacts"
            :key="contact.id"
            :active="selecting"
            :selected="selection.has(contact)"
            :label="t('common.selectItem', { name: contact.display_name })"
            @toggle="selection.toggle(contact)"
          >
            <SwipeActionRow
              :delete-label="t('common.delete')"
              :disabled="selecting || deleting"
              @delete="remove(contact)"
            >
              <button
                class="list-item"
                :class="{ 'is-selected': contact.id === selectedId }"
                type="button"
                @click="selectContact(contact)"
              >
                <ListItemAvatarStatus>
                  <BaseAvatar :name="contact.display_name" :src="contact.avatar" />
                </ListItemAvatarStatus>
                <span class="list-item__content">
                  <strong>{{ contact.display_name }}</strong>
                  <small>{{ primaryPhone(contact.phones) || t('contacts.noNumber') }}</small>
                </span>
                <ListItemStatusRail>
                  <Star
                    v-if="contact.favorite"
                    class="contact-favorite-mark"
                    :size="15"
                    fill="currentColor"
                    :aria-label="t('contacts.favorited')"
                  />
                </ListItemStatusRail>
              </button>
            </SwipeActionRow>
          </SelectableListRow>
        </template>
        <InfiniteScrollTrigger
          :has-more="contactsPagination.hasMore"
          :loading="contactsPagination.loadingMore"
          :error="contactsPagination.error"
          :loading-label="t('contacts.loading')"
          :retry-label="t('common.retry')"
          @load="loadMoreContacts"
        />
      </div>
      <BatchActionBar
        v-if="selecting"
        :selected="selectionCount"
        :total="filteredContacts.length"
        :selected-label="t('common.selectedCount', { count: selectionCount })"
        :select-all-label="t('common.selectAll')"
        :clear-all-label="t('common.clearAll')"
        :done-label="t('common.done')"
        :busy="batchBusy"
        @select-all="selection.selectAll(filteredContacts)"
        @done="selection.exit"
      >
        <button
          v-if="batchContacts.length > 0"
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

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            :title="t('contacts.back')"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <BaseAvatar :name="selected.display_name" :src="selected.avatar" size="large" />
          <div class="detail-header__identity">
            <h2>{{ selected.display_name }}</h2>
            <span v-if="selected.notes">{{ selected.notes }}</span>
          </div>
          <div v-if="contactEditingAvailable" class="detail-header__actions">
            <button
              class="icon-button contact-favorite-button"
              :class="{ 'is-active': selected.favorite }"
              type="button"
              :title="
                selected.favorite
                  ? t('contacts.unfavorite')
                  : t('contacts.favorite')
              "
              :aria-pressed="selected.favorite"
              :disabled="favoritePending"
              @click="toggleFavorite(selected)"
            >
              <Star :size="18" :fill="selected.favorite ? 'currentColor' : 'none'" />
            </button>
            <button
              class="icon-button"
              type="button"
              :title="t('contacts.edit')"
              @click="openEdit(selected)"
            >
              <Pencil :size="18" />
            </button>
            <button
              class="icon-button icon-button--danger desktop-delete-action"
              type="button"
              :title="t('contacts.delete')"
              :disabled="deleting"
              @click="remove(selected)"
            >
              <Trash2 :size="18" />
            </button>
          </div>
        </header>

        <div class="contact-detail">
          <p v-if="favoriteError" class="field-error" role="alert">{{ favoriteError }}</p>
          <section class="detail-section">
            <h3>{{ t('contacts.phoneNumbers') }}</h3>
            <div v-for="phone in selected.phones" :key="phone.id" class="phone-detail-row">
              <span>
                <small>
                  {{ phone.label
                  }}{{ phone.primary ? ` · ${t('contacts.primary')}` : '' }}
                </small>
                <strong>{{ phone.number }}</strong>
              </span>
              <span class="row-actions">
                <button
                  class="icon-button"
                  type="button"
                  :disabled="Boolean(dialUnavailable)"
                  :title="dialUnavailable || t('calls.dial')"
                  @click="call(selected, phoneDestination(phone))"
                >
                  <Phone :size="18" />
                </button>
                <button
                  class="icon-button"
                  type="button"
                  :disabled="Boolean(messageUnavailable)"
                  :title="messageUnavailable || t('messages.sendMessage')"
                  @click="message(selected, phoneDestination(phone))"
                >
                  <MessageSquareText :size="18" />
                </button>
              </span>
            </div>
            <p v-if="selected.preferred_line_id" class="contact-preferred-line">
              {{ t('contacts.preferredLine') }}：{{ preferredLineName(selected) }}
            </p>
          </section>
          <p v-if="dialUnavailable || messageUnavailable" class="unavailable-note">
            {{ dialUnavailable || messageUnavailable }}
          </p>
        </div>
      </template>

      <StatePanel
        v-else
        state="empty"
        :title="t('contacts.select')"
        :detail="t('contacts.detailPlaceholder')"
      />
    </article>

    <ContactEditor
      :open="editorOpen"
      :contact="editing"
      :lines="contactLines"
      :default-line-id="defaultLineID"
      :saving="saving"
      :error="editorError"
      @close="editorOpen = false"
      @save="save"
    />
  </section>
</template>

<style scoped>
.contact-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 0 20px 24px;
}

.contact-favorite-mark,
.contact-favorite-button.is-active {
  color: #a86400;
}

.contact-favorite-mark {
  flex: 0 0 auto;
}

.contact-delete-error {
  margin: 0 16px 8px;
}

@media (max-width: 1100px) {
  .desktop-delete-action {
    display: none;
  }
}
</style>
