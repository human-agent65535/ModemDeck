<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ContactRound, UserPlus, X } from '@lucide/vue'
import type { Contact } from '../api/types'
import BaseAvatar from './BaseAvatar.vue'
import SearchField from './SearchField.vue'
import {
  contactEditingAvailable,
  contactsResource,
  loadContacts,
  saveContact
} from '../state/workspace'

const props = defineProps<{
  number: string
  contact?: Contact
}>()

const emit = defineEmits<{
  saved: [contact: Contact]
}>()

const mode = ref<'create' | 'add' | null>(null)
const createName = ref('')
const phoneLabel = ref('手机')
const search = ref('')
const selectedContactID = ref('')
const saving = ref(false)
const error = ref('')

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

const selectedContact = computed(() =>
  contactsResource.data.find(contact => contact.id === selectedContactID.value)
)

watch(
  () => props.contact,
  contact => {
    if (contact) closeDialog()
  }
)

function closeDialog(): void {
  if (saving.value) return
  mode.value = null
  createName.value = ''
  phoneLabel.value = '手机'
  search.value = ''
  selectedContactID.value = ''
  error.value = ''
}

function openCreate(): void {
  createName.value = ''
  phoneLabel.value = '手机'
  error.value = ''
  mode.value = 'create'
}

async function openAdd(): Promise<void> {
  search.value = ''
  selectedContactID.value = ''
  error.value = ''
  mode.value = 'add'
  await loadContacts()
}

async function createContact(): Promise<void> {
  const name = createName.value.trim()
  if (!name || !props.number.trim() || saving.value) return
  saving.value = true
  error.value = ''
  try {
    const saved = await saveContact({
      display_name: name,
      favorite: false,
      phones: [
        {
          label: phoneLabel.value.trim() || '手机',
          number: props.number.trim(),
          primary: true
        }
      ]
    })
    mode.value = null
    emit('saved', saved)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '联系人保存失败'
  } finally {
    saving.value = false
  }
}

async function addToContact(): Promise<void> {
  const contact = selectedContact.value
  if (!contact || !props.number.trim() || saving.value) return
  const digits = props.number.replace(/\D/g, '')
  if (contact.phones.some(phone => phone.number.replace(/\D/g, '') === digits)) {
    error.value = '该联系人已经包含此号码'
    return
  }

  saving.value = true
  error.value = ''
  try {
    const saved = await saveContact(
      {
        display_name: contact.display_name,
        avatar: contact.avatar,
        favorite: contact.favorite,
        notes: contact.notes,
        preferred_device_imei: contact.preferred_device_imei,
        revision: contact.revision,
        phones: [
          ...contact.phones.map(phone => ({
            id: phone.id,
            label: phone.label,
            number: phone.number,
            primary: phone.primary
          })),
          {
            label: phoneLabel.value.trim() || '其他',
            number: props.number.trim(),
            primary: !contact.phones.some(phone => phone.primary)
          }
        ]
      },
      contact.id
    )
    mode.value = null
    emit('saved', saved)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '联系人保存失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="contact-number-actions">
    <RouterLink
      v-if="contact"
      class="secondary-button"
      :to="{ name: 'contacts', params: { contactId: contact.id } }"
    >
      <ContactRound :size="17" />
      查看联系人
    </RouterLink>
    <template v-else-if="contactEditingAvailable">
      <button class="secondary-button" type="button" @click="openCreate">
        <UserPlus :size="17" />
        新建联系人
      </button>
      <button class="secondary-button" type="button" @click="openAdd">
        <ContactRound :size="17" />
        加入已有联系人
      </button>
    </template>
  </div>

  <Teleport to="body">
    <Transition name="fade">
      <div v-if="mode" class="modal-backdrop" @mousedown.self="closeDialog">
        <section
          class="editor-dialog quick-contact-dialog"
          role="dialog"
          aria-modal="true"
          :aria-label="mode === 'create' ? '新建联系人' : '加入已有联系人'"
          @keydown.esc="closeDialog"
        >
          <header class="tool-header">
            <h2>{{ mode === 'create' ? '新建联系人' : '加入已有联系人' }}</h2>
            <button
              class="icon-button"
              type="button"
              title="关闭"
              :disabled="saving"
              @click="closeDialog"
            >
              <X :size="19" />
            </button>
          </header>

          <form
            v-if="mode === 'create'"
            class="quick-contact-form"
            @submit.prevent="createContact"
          >
            <label class="field">
              <span>姓名</span>
              <input v-model="createName" autocomplete="name" autofocus required />
            </label>
            <div class="quick-contact-phone">
              <label class="field">
                <span>类型</span>
                <input v-model="phoneLabel" aria-label="号码类型" />
              </label>
              <label class="field quick-contact-phone__number">
                <span>电话号码</span>
                <input :value="number" type="tel" readonly />
              </label>
            </div>
            <p v-if="error" class="field-error" role="alert">{{ error }}</p>
            <footer class="dialog-actions">
              <button class="secondary-button" type="button" :disabled="saving" @click="closeDialog">
                取消
              </button>
              <button
                class="primary-button"
                type="submit"
                :disabled="!createName.trim() || saving"
              >
                {{ saving ? '正在保存…' : '创建' }}
              </button>
            </footer>
          </form>

          <form v-else class="quick-contact-form" @submit.prevent="addToContact">
            <div class="quick-contact-target">
              <span>电话号码</span>
              <strong>{{ number }}</strong>
            </div>
            <label class="field">
              <span>号码类型</span>
              <input v-model="phoneLabel" aria-label="号码类型" />
            </label>
            <SearchField v-model="search" placeholder="搜索联系人" />
            <p
              v-if="contactsResource.status === 'loading' || contactsResource.status === 'idle'"
              class="quick-contact-state"
            >
              正在载入联系人
            </p>
            <p
              v-else-if="contactsResource.status === 'error' || contactsResource.status === 'forbidden'"
              class="field-error"
              role="alert"
            >
              {{ contactsResource.error || '无法载入联系人' }}
            </p>
            <div v-else-if="filteredContacts.length" class="quick-contact-list" role="listbox">
              <button
                v-for="candidate in filteredContacts"
                :key="candidate.id"
                class="quick-contact-option"
                :class="{ 'is-selected': candidate.id === selectedContactID }"
                type="button"
                role="option"
                :aria-selected="candidate.id === selectedContactID"
                @click="selectedContactID = candidate.id"
              >
                <BaseAvatar :name="candidate.display_name" :src="candidate.avatar" />
                <span>
                  <strong>{{ candidate.display_name }}</strong>
                  <small>{{ candidate.phones[0]?.number || '没有号码' }}</small>
                </span>
              </button>
            </div>
            <p v-else class="quick-contact-state">没有匹配的联系人</p>
            <p v-if="error" class="field-error" role="alert">{{ error }}</p>
            <footer class="dialog-actions">
              <button class="secondary-button" type="button" :disabled="saving" @click="closeDialog">
                取消
              </button>
              <button
                class="primary-button"
                type="submit"
                :disabled="!selectedContact || saving"
              >
                {{ saving ? '正在保存…' : '添加号码' }}
              </button>
            </footer>
          </form>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.contact-number-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.quick-contact-dialog {
  width: min(520px, calc(100vw - 32px));
}

.quick-contact-form {
  display: flex;
  max-height: calc(100dvh - 94px);
  flex-direction: column;
  gap: 16px;
  padding: 18px;
  overflow-y: auto;
}

.quick-contact-phone {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr);
  gap: 10px;
}

.quick-contact-phone__number {
  min-width: 0;
}

.quick-contact-target {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 12px;
  background: var(--surface-subtle);
  border-radius: 8px;
}

.quick-contact-target span,
.quick-contact-state {
  color: var(--muted);
  font-size: 13px;
}

.quick-contact-target strong {
  overflow-wrap: anywhere;
  font-size: 14px;
}

.quick-contact-list {
  max-height: min(360px, 44dvh);
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.quick-contact-option {
  display: flex;
  width: 100%;
  min-height: 62px;
  align-items: center;
  gap: 11px;
  padding: 8px 12px;
  color: var(--text);
  text-align: left;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.quick-contact-option:last-child {
  border-bottom: 0;
}

.quick-contact-option:hover,
.quick-contact-option.is-selected {
  background: var(--accent-soft);
}

.quick-contact-option.is-selected {
  box-shadow: inset 3px 0 var(--accent);
}

.quick-contact-option > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.quick-contact-option strong,
.quick-contact-option small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quick-contact-option strong {
  font-size: 14px;
}

.quick-contact-option small {
  color: var(--muted);
  font-size: 12px;
}

.quick-contact-state {
  padding: 20px 12px;
  text-align: center;
}

@media (max-width: 560px) {
  .contact-number-actions,
  .contact-number-actions .secondary-button {
    width: 100%;
  }

  .quick-contact-phone {
    grid-template-columns: 1fr;
  }
}
</style>
