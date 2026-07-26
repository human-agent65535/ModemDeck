<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ContactRound, UserPlus, X } from '@lucide/vue'
import type { Contact, ContactInput } from '../api/types'
import BaseAvatar from './BaseAvatar.vue'
import ContactEditor from './ContactEditor.vue'
import SearchField from './SearchField.vue'
import {
  bootstrapResource,
  contactEditingAvailable,
  contactsResource,
  loadContacts,
  saveContact
} from '../state/workspace'

const props = withDefaults(
  defineProps<{
    number: string
    contact?: Contact
    compact?: boolean
  }>(),
  { compact: false }
)
const { t } = useI18n()

const emit = defineEmits<{
  saved: [contact: Contact]
}>()

const createOpen = ref(false)
const addOpen = ref(false)
const phoneLabel = ref(t('contacts.mobile'))
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
const contactLines = computed(
  () => bootstrapResource.data?.lines.filter(line => Boolean(line.device_imei)) || []
)

const selectedContact = computed(() =>
  contactsResource.data.find(contact => contact.id === selectedContactID.value)
)

watch(
  () => props.contact,
  contact => {
    if (contact) {
      closeCreate()
      closeAdd()
    }
  }
)

function closeCreate(): void {
  if (saving.value) return
  createOpen.value = false
  error.value = ''
}

function closeAdd(): void {
  if (saving.value) return
  addOpen.value = false
  search.value = ''
  selectedContactID.value = ''
  error.value = ''
}

function openCreate(): void {
  error.value = ''
  createOpen.value = true
}

async function openAdd(): Promise<void> {
  phoneLabel.value = t('contacts.mobile')
  search.value = ''
  selectedContactID.value = ''
  error.value = ''
  addOpen.value = true
  await loadContacts()
}

async function createContact(input: ContactInput): Promise<void> {
  if (saving.value) return
  saving.value = true
  error.value = ''
  try {
    const saved = await saveContact(input)
    createOpen.value = false
    emit('saved', saved)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('contacts.saveFailed')
  } finally {
    saving.value = false
  }
}

async function addToContact(): Promise<void> {
  const contact = selectedContact.value
  if (!contact || !props.number.trim() || saving.value) return
  const digits = props.number.replace(/\D/g, '')
  if (contact.phones.some(phone => phone.number.replace(/\D/g, '') === digits)) {
    error.value = t('contacts.duplicateNumber')
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
            label: phoneLabel.value.trim() || t('contacts.other'),
            number: props.number.trim(),
            primary: !contact.phones.some(phone => phone.primary)
          }
        ]
      },
      contact.id
    )
    addOpen.value = false
    emit('saved', saved)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('contacts.saveFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="contact-number-actions" :class="{ 'is-compact': compact }">
    <RouterLink
      v-if="contact"
      class="secondary-button"
      :to="{ name: 'contacts', params: { contactId: contact.id } }"
      :aria-label="t('contacts.view')"
      :title="t('contacts.view')"
    >
      <ContactRound :size="17" />
      <span class="contact-number-action__label">{{ t('contacts.view') }}</span>
    </RouterLink>
    <template v-else-if="contactEditingAvailable">
      <button
        class="secondary-button"
        type="button"
        :aria-label="t('contacts.new')"
        :title="t('contacts.new')"
        @click="openCreate"
      >
        <UserPlus :size="17" />
        <span class="contact-number-action__label">{{ t('contacts.new') }}</span>
      </button>
      <button
        class="secondary-button"
        type="button"
        :aria-label="t('contacts.addExisting')"
        :title="t('contacts.addExisting')"
        @click="openAdd"
      >
        <ContactRound :size="17" />
        <span class="contact-number-action__label">{{ t('contacts.addExisting') }}</span>
      </button>
    </template>
  </div>

  <ContactEditor
    :open="createOpen"
    :initial-phone="number"
    :lines="contactLines"
    :saving="saving"
    :error="error"
    @close="closeCreate"
    @save="createContact"
  />

  <Teleport to="body">
    <Transition name="fade">
      <div v-if="addOpen" class="modal-backdrop" @mousedown.self="closeAdd">
        <section
          class="editor-dialog quick-contact-dialog"
          role="dialog"
          aria-modal="true"
          :aria-label="t('contacts.addExisting')"
          @keydown.esc="closeAdd"
        >
          <header class="tool-header">
            <h2>{{ t('contacts.addExisting') }}</h2>
            <button
              class="icon-button"
              type="button"
              :title="t('common.close')"
              :disabled="saving"
              @click="closeAdd"
            >
              <X :size="19" />
            </button>
          </header>

          <form class="quick-contact-form" @submit.prevent="addToContact">
            <div class="quick-contact-target">
              <span>{{ t('contacts.phoneNumber') }}</span>
              <strong>{{ number }}</strong>
            </div>
            <label class="field">
              <span>{{ t('contacts.phoneType') }}</span>
              <input v-model="phoneLabel" :aria-label="t('contacts.phoneType')" />
            </label>
            <SearchField v-model="search" :placeholder="t('contacts.search')" />
            <p
              v-if="contactsResource.status === 'loading' || contactsResource.status === 'idle'"
              class="quick-contact-state"
            >
              {{ t('contacts.loading') }}
            </p>
            <p
              v-else-if="contactsResource.status === 'error' || contactsResource.status === 'forbidden'"
              class="field-error"
              role="alert"
            >
              {{ contactsResource.error || t('contacts.loadFailed') }}
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
                  <small>{{ candidate.phones[0]?.number || t('contacts.noNumber') }}</small>
                </span>
              </button>
            </div>
            <p v-else class="quick-contact-state">{{ t('contacts.noMatches') }}</p>
            <p v-if="error" class="field-error" role="alert">{{ error }}</p>
            <footer class="dialog-actions">
              <button class="secondary-button" type="button" :disabled="saving" @click="closeAdd">
                {{ t('common.cancel') }}
              </button>
              <button
                class="primary-button"
                type="submit"
                :disabled="!selectedContact || saving"
              >
                {{ saving ? t('common.saving') : t('contacts.addNumber') }}
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

.contact-number-actions.is-compact {
  flex-wrap: nowrap;
}

.contact-number-actions.is-compact .secondary-button {
  min-height: 34px;
  padding: 0 10px;
  white-space: nowrap;
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

@media (max-width: 760px) {
  .contact-number-actions.is-compact .secondary-button {
    width: 36px;
    min-width: 36px;
    padding: 0;
  }

  .contact-number-actions.is-compact .contact-number-action__label {
    display: none;
  }
}

@media (max-width: 560px) {
  .contact-number-actions:not(.is-compact),
  .contact-number-actions:not(.is-compact) .secondary-button {
    width: 100%;
  }

}
</style>
