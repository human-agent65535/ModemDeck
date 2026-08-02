<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
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
import ListSkeleton from '../components/ListSkeleton.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import SearchField from '../components/SearchField.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import LoadingSkeletonBoundary from '../components/skeletons/LoadingSkeletonBoundary.vue'
import WorkspaceDetailSkeleton from '../components/skeletons/WorkspaceDetailSkeleton.vue'
import CommunicationListToolbar from '../components/workspace/CommunicationListToolbar.vue'
import FavoriteActionButton from '../components/workspace/FavoriteActionButton.vue'
import WorkspaceDetailActions from '../components/workspace/WorkspaceDetailActions.vue'
import WorkspaceDetailHeader from '../components/workspace/WorkspaceDetailHeader.vue'
import WorkspaceDetailPane from '../components/workspace/WorkspaceDetailPane.vue'
import WorkspaceListHeader from '../components/workspace/WorkspaceListHeader.vue'
import WorkspaceMasterDetail from '../components/workspace/WorkspaceMasterDetail.vue'
import { useInitialLoadBarrier } from '../composables/useInitialLoadBarrier'
import { useDurablePageRefresh } from '../composables/useDurablePageRefresh'
import { useListArrivals } from '../composables/useListArrivals'
import { useListSelection } from '../composables/useListSelection'
import { skeletonPreviewEnabled } from '../composables/useSkeletonPreview'
import { messageComposeRoute } from '../router/messageRoute'
import { requestConfirmation } from '../state/confirmation'
import { showSuccess } from '../state/feedback'
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
  refreshContacts,
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
const { loading: initialLoading, waitFor: waitForInitialLoad } = useInitialLoadBarrier()
useDurablePageRefresh(() => refreshContacts(), {
  enabled: () => route.name === 'contacts'
})
const contactArrivals = useListArrivals(
  () => ({
    items: contactsResource.data,
    ready: contactsResource.status === 'ready',
    animate: !contactsPagination.loadingMore
  }),
  contact => contact.id
)

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
    showSuccess(t('common.saved'))
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
  void router.push(
    messageComposeRoute({
      recipient: number,
      name: contact.display_name,
      lineKey: contact.preferred_line_id || ''
    })
  )
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
  void waitForInitialLoad([() => loadBootstrap(), () => refreshContacts()])
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
})
</script>

<template>
  <WorkspaceMasterDetail
    :has-selection="Boolean(selected)"
    :batch-selecting="selecting"
  >
    <template #list>
      <WorkspaceListHeader
        :title="t('shell.contacts')"
        compact-mode="floating-action"
        :count="
          !initialLoading && contactsResource.status === 'ready'
            ? contactsResource.data.length
            : undefined
        "
      >
        <template #actions>
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
        </template>
      </WorkspaceListHeader>
      <CommunicationListToolbar>
        <template #primary>
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="contactsResource.status !== 'ready' || contactsResource.data.length === 0"
            @toggle="selection.toggleMode"
          />
          <SearchField v-model="search" :placeholder="t('common.search')" />
        </template>
      </CommunicationListToolbar>
      <p v-if="deleteError" class="field-error contact-delete-error" role="alert">
        {{ deleteError }}
      </p>

      <LoadingSkeletonBoundary
        :loading="initialLoading || contactsResource.status === 'loading'"
      >
        <template #skeleton>
          <ListSkeleton :label="t('contacts.loading')" />
        </template>
        <StatePanel
          v-if="contactsResource.status === 'forbidden'"
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
            :arriving="contactArrivals.isArriving(contact.id)"
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
                  <template #favorite>
                    <Star
                      v-if="contact.favorite"
                      class="contact-favorite-mark"
                      :size="15"
                      fill="currentColor"
                      :aria-label="t('contacts.favorited')"
                    />
                  </template>
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
      </LoadingSkeletonBoundary>
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
    </template>

    <template #detail>
      <WorkspaceDetailPane
        :content-key="initialLoading || skeletonPreviewEnabled ? null : selected?.id"
      >
        <template v-if="selected">
          <WorkspaceDetailHeader>
          <template #identity>
            <BaseAvatar
              :name="selected.display_name"
              :src="selected.avatar"
              size="medium"
            />
            <div class="workspace-detail-identity">
              <h2>{{ selected.display_name }}</h2>
              <span v-if="selected.notes">{{ selected.notes }}</span>
            </div>
          </template>
          <template v-if="contactEditingAvailable" #actions>
            <WorkspaceDetailActions>
              <template #primary>
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
                <FavoriteActionButton
                  :active="selected.favorite"
                  :disabled="favoritePending"
                  :activate-label="t('contacts.favorite')"
                  :deactivate-label="t('contacts.unfavorite')"
                  @toggle="toggleFavorite(selected)"
                />
              </template>
            </WorkspaceDetailActions>
          </template>
        </WorkspaceDetailHeader>

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
        <template #empty>
          <LoadingSkeletonBoundary :loading="initialLoading">
            <template #skeleton>
              <WorkspaceDetailSkeleton
                :label="t('contacts.loading')"
                shape="contact"
              />
            </template>
            <StatePanel
              state="empty"
              :title="t('contacts.select')"
              :detail="t('contacts.detailPlaceholder')"
            />
          </LoadingSkeletonBoundary>
        </template>
      </WorkspaceDetailPane>
    </template>
  </WorkspaceMasterDetail>

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
</template>

<style scoped>
.contact-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 0 20px 24px;
}

.contact-favorite-mark {
  color: var(--favorite);
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
