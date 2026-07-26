<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
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
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactEditor from '../components/ContactEditor.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import { requestConfirmation } from '../state/confirmation'
import { openDialer } from '../state/ui'
import {
  capabilityReason,
  bootstrapResource,
  contactsResource,
  contactEditingAvailable,
  deleteContact,
  lineLabel,
  loadBootstrap,
  loadContacts,
  saveContact
} from '../state/workspace'
import { primaryPhone } from '../utils/format'

const route = useRoute()
const router = useRouter()
const search = ref('')
const editorOpen = ref(false)
const editing = ref<Contact | undefined>()
const saving = ref(false)
const editorError = ref('')
const deleting = ref(false)
const favoritePending = ref(false)
const favoriteError = ref('')

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
  () => bootstrapResource.data?.lines.filter(line => Boolean(line.device_imei)) || []
)

watch(
  () => contactsResource.status,
  status => {
    if (status === 'ready' && selectedId.value && !selected.value) {
      void router.replace({ name: 'contacts' })
    }
  }
)

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
    editorError.value = error instanceof Error ? error.message : '联系人保存失败'
  } finally {
    saving.value = false
  }
}

async function remove(contact: Contact): Promise<void> {
  const confirmed = await requestConfirmation({
    title: '删除联系人？',
    message: `“${contact.display_name}”将被永久删除。`,
    confirmLabel: '删除',
    tone: 'danger'
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await deleteContact(contact)
    await router.replace({ name: 'contacts' })
  } catch (error) {
    editorError.value = error instanceof Error ? error.message : '联系人删除失败'
  } finally {
    deleting.value = false
  }
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
        preferred_device_imei: contact.preferred_device_imei,
        revision: contact.revision,
        phones: contact.phones.map(phone => ({
          id: phone.id,
          label: phone.label,
          number: phone.number,
          primary: phone.primary
        }))
      },
      contact.id
    )
  } catch (error) {
    favoriteError.value = error instanceof Error ? error.message : '收藏状态保存失败'
  } finally {
    favoritePending.value = false
  }
}

function call(contact: Contact, number: string): void {
  if (dialUnavailable.value) return
  openDialer(number, contact.display_name, contact.preferred_device_imei || '')
}

function message(contact: Contact, number: string): void {
  if (messageUnavailable.value) return
  void router.push({
    name: 'messages',
    query: {
      compose: number,
      name: contact.display_name,
      ...(contact.preferred_device_imei ? { line: contact.preferred_device_imei } : {})
    }
  })
}

function openSavedContact(contact: Contact): void {
  search.value = ''
  selectContact(contact)
}

function preferredLineName(contact: Contact): string {
  const line = contactLines.value.find(item => item.device_imei === contact.preferred_device_imei)
  return line ? lineLabel(line) : contact.preferred_device_imei || ''
}

onMounted(() => {
  void loadBootstrap()
  void loadContacts()
})
</script>

<template>
  <section class="workspace" :class="{ 'has-selection': selected }">
    <aside class="list-pane">
      <header class="pane-header">
        <div>
          <h1>联系人</h1>
          <span v-if="contactsResource.status === 'ready'">{{ contactsResource.data.length }}</span>
        </div>
        <button v-if="contactEditingAvailable" class="icon-button" type="button" title="新建联系人" @click="openNew">
          <UserPlus :size="19" />
        </button>
      </header>
      <div class="pane-search">
        <SearchField v-model="search" placeholder="搜索姓名或号码" />
      </div>

      <StatePanel
        v-if="contactsResource.status === 'loading'"
        state="loading"
        title="正在载入联系人"
      />
      <StatePanel
        v-else-if="contactsResource.status === 'forbidden'"
        state="forbidden"
        title="无权查看联系人"
        :detail="contactsResource.error"
      />
      <StatePanel
        v-else-if="contactsResource.status === 'error'"
        state="error"
        title="无法载入联系人"
        :detail="contactsResource.error"
        retryable
        @retry="loadContacts(true)"
      />
      <div v-else-if="filteredContacts.length === 0" class="contact-empty-state">
        <StatePanel
          state="empty"
          :title="search ? '没有匹配的联系人' : '还没有联系人'"
        />
        <ContactNumberActions
          v-if="searchedPhone"
          :number="searchedPhone"
          @saved="openSavedContact"
        />
      </div>
      <div v-else class="item-list" role="list">
        <button
          v-for="contact in filteredContacts"
          :key="contact.id"
          class="list-item"
          :class="{ 'is-selected': contact.id === selectedId }"
          type="button"
          @click="selectContact(contact)"
        >
          <BaseAvatar :name="contact.display_name" :src="contact.avatar" />
          <span class="list-item__content">
            <strong>{{ contact.display_name }}</strong>
            <small>{{ primaryPhone(contact.phones) || '没有号码' }}</small>
          </span>
          <Star
            v-if="contact.favorite"
            class="contact-favorite-mark"
            :size="15"
            fill="currentColor"
            aria-label="已收藏"
          />
        </button>
      </div>
    </aside>

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button class="icon-button mobile-back" type="button" title="返回联系人" @click="backToList">
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
              :title="selected.favorite ? '取消收藏' : '收藏联系人'"
              :aria-pressed="selected.favorite"
              :disabled="favoritePending"
              @click="toggleFavorite(selected)"
            >
              <Star :size="18" :fill="selected.favorite ? 'currentColor' : 'none'" />
            </button>
            <button class="icon-button" type="button" title="编辑联系人" @click="openEdit(selected)">
              <Pencil :size="18" />
            </button>
            <button
              class="icon-button icon-button--danger"
              type="button"
              title="删除联系人"
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
            <h3>电话号码</h3>
            <div v-for="phone in selected.phones" :key="phone.id" class="phone-detail-row">
              <span>
                <small>{{ phone.label }}{{ phone.primary ? ' · 主要' : '' }}</small>
                <strong>{{ phone.number }}</strong>
              </span>
              <span class="row-actions">
                <button
                  class="icon-button"
                  type="button"
                  :disabled="Boolean(dialUnavailable)"
                  :title="dialUnavailable || '拨打'"
                  @click="call(selected, phone.number)"
                >
                  <Phone :size="18" />
                </button>
                <button
                  class="icon-button"
                  type="button"
                  :disabled="Boolean(messageUnavailable)"
                  :title="messageUnavailable || '发送消息'"
                  @click="message(selected, phone.number)"
                >
                  <MessageSquareText :size="18" />
                </button>
              </span>
            </div>
            <p v-if="selected.preferred_device_imei" class="contact-preferred-line">
              首选线路：{{ preferredLineName(selected) }}
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
        title="选择一个联系人"
        detail="联系人详情会显示在这里"
      />
    </article>

    <ContactEditor
      :open="editorOpen"
      :contact="editing"
      :lines="contactLines"
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
</style>
