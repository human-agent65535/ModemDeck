<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Star, Trash2, X } from '@lucide/vue'
import type { Contact, ContactInput, LineSummary } from '../api/types'
import ContactAvatarPicker from './ContactAvatarPicker.vue'
import CountryRegionSelector from './CountryRegionSelector.vue'
import LineSelector from './LineSelector.vue'
import OverlayDialog from './OverlayDialog.vue'

type PhoneDraft = {
  id?: string
  label: string
  number: string
  region?: string
  primary: boolean
}

const { t } = useI18n()
const props = defineProps<{
  open: boolean
  contact?: Contact
  lines?: LineSummary[]
  defaultLineId?: string
  initialPhone?: string
  saving?: boolean
  error?: string
}>()

const emit = defineEmits<{
  close: []
  save: [input: ContactInput]
}>()

const draft = reactive<{
  name: string
  avatar: string
  notes: string
  favorite: boolean
  preferredLineID: string
  phones: PhoneDraft[]
}>({
  name: '',
  avatar: '',
  notes: '',
  favorite: false,
  preferredLineID: '',
  phones: []
})
const avatarBusy = ref(false)
const preferredPhoneRegions = computed(() => {
  const regions = new Set<string>()
  for (const line of props.lines || []) {
    const region = line.home_country_iso.trim().toUpperCase()
    if (/^[A-Z]{2}$/.test(region)) regions.add(region)
  }
  for (const phone of draft.phones) {
    const region = phone.region?.trim().toUpperCase() || ''
    if (/^[A-Z]{2}$/.test(region)) regions.add(region)
  }
  return Array.from(regions)
})
const defaultPhoneRegion = computed(() => {
  const preferredDefault = props.lines?.find(line => line.id === props.defaultLineId)
  const candidates = [preferredDefault, ...(props.lines || [])]
  for (const line of candidates) {
    const region = line?.home_country_iso.trim().toUpperCase() || ''
    if (/^[A-Z]{2}$/.test(region)) return region
  }
  return ''
})
const valid = computed(
  () =>
    !avatarBusy.value &&
    Boolean(draft.name.trim()) &&
    draft.phones.length > 0 &&
    draft.phones.every(phone => Boolean(phone.number.trim())) &&
    draft.phones.filter(phone => phone.primary).length === 1
)

watch(
  () => [props.open, props.contact] as const,
  ([open, contact]) => {
    if (!open) return
    draft.name = contact?.display_name || ''
    draft.avatar = contact?.avatar || ''
    draft.notes = contact?.notes || ''
    draft.favorite = contact?.favorite || false
    draft.preferredLineID = contact?.preferred_line_id || ''
    const initialNumber = props.initialPhone?.trim() || ''
    draft.phones = contact?.phones.length
      ? contact.phones.map(phone => ({
          id: phone.id,
          label: phone.label,
          number: phone.number,
          region: phone.region,
          primary: phone.primary
        }))
      : [{
          label: t('contacts.mobile'),
          number: initialNumber,
          region: initialNumber.startsWith('+') ? '' : defaultPhoneRegion.value,
          primary: true
        }]
  },
  { immediate: true }
)

function addPhone(): void {
  draft.phones.push({
    label: draft.phones.length === 0 ? t('contacts.mobile') : t('contacts.other'),
    number: '',
    region: defaultPhoneRegion.value,
    primary: draft.phones.length === 0
  })
}

function removePhone(index: number): void {
  const wasPrimary = draft.phones[index]?.primary
  draft.phones.splice(index, 1)
  if (wasPrimary && draft.phones[0]) draft.phones[0].primary = true
}

function setPrimary(index: number): void {
  draft.phones.forEach((phone, phoneIndex) => {
    phone.primary = phoneIndex === index
  })
}

function submit(): void {
  if (!valid.value || props.saving) return
  emit('save', {
    display_name: draft.name.trim(),
    avatar: draft.avatar || undefined,
    favorite: draft.favorite,
    notes: draft.notes.trim() || undefined,
    preferred_line_id: draft.preferredLineID || undefined,
    revision: props.contact?.revision,
    phones: draft.phones.map(phone => ({
      id: phone.id,
      label: phone.label.trim() || t('contacts.phone'),
      number: phone.number.trim(),
      region: phone.region?.trim().toUpperCase() || undefined,
      primary: phone.primary
    }))
  })
}
</script>

<template>
  <OverlayDialog
    :open="open"
    size="large"
    surface-class="editor-dialog"
    :label="contact ? t('contacts.edit') : t('contacts.new')"
    initial-focus='input[autocomplete="name"]'
    @close="emit('close')"
  >
    <header class="tool-header">
      <h2>{{ contact ? t('contacts.edit') : t('contacts.new') }}</h2>
      <button
        class="icon-button"
        type="button"
        :title="t('common.close')"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="19" />
      </button>
    </header>

    <form class="editor-form" @submit.prevent="submit">
      <ContactAvatarPicker
        v-model="draft.avatar"
        :name="draft.name"
        :disabled="saving"
        @processing="avatarBusy = $event"
      />

      <label class="field">
        <span>{{ t('contacts.name') }}</span>
        <input v-model="draft.name" autocomplete="name" required />
      </label>

      <label class="contact-favorite-toggle">
        <span>
          <Star :size="18" :fill="draft.favorite ? 'currentColor' : 'none'" />
          <strong>{{ t('contacts.favorite') }}</strong>
        </span>
        <input v-model="draft.favorite" type="checkbox" role="switch" />
      </label>

      <fieldset class="phone-fields">
        <legend>{{ t('contacts.phoneNumbers') }}</legend>
        <div v-for="(phone, index) in draft.phones" :key="phone.id || index" class="phone-row">
          <input
            v-model="phone.label"
            class="phone-row__label"
            :aria-label="t('contacts.phoneType')"
          />
          <div class="phone-row__number">
            <CountryRegionSelector
              v-model="phone.region"
              :regions="preferredPhoneRegions"
            />
            <input
              v-model="phone.number"
              type="tel"
              autocomplete="tel"
              :aria-label="t('contacts.phoneNumber')"
              required
            />
          </div>
          <label
            class="primary-radio"
            :title="
              phone.primary
                ? t('contacts.primaryNumber')
                : t('contacts.makePrimary')
            "
          >
            <input
              type="radio"
              name="primary-phone"
              :checked="phone.primary"
              @change="setPrimary(index)"
            />
            <span>{{ t('contacts.primary') }}</span>
          </label>
          <button
            class="icon-button icon-button--quiet"
            type="button"
            :title="t('contacts.removeNumber')"
            :aria-label="t('contacts.removeNumber')"
            :disabled="draft.phones.length === 1"
            @click="removePhone(index)"
          >
            <Trash2 :size="17" />
          </button>
        </div>
        <button class="text-button" type="button" @click="addPhone">
          <Plus :size="16" />{{ t('contacts.addNumber') }}
        </button>
      </fieldset>

      <LineSelector
        v-if="lines?.length"
        v-model="draft.preferredLineID"
        :lines="lines"
        :label="t('contacts.preferredLine')"
        include-all
        all-value=""
        :all-label="t('contacts.followDefaultLine')"
        :all-description="t('contacts.followDefaultLineDescription')"
      />

      <label class="field">
        <span>{{ t('contacts.notes') }}</span>
        <textarea v-model="draft.notes" rows="3" />
      </label>

      <p v-if="error" class="field-error" role="alert">{{ error }}</p>
      <footer class="dialog-actions">
        <button class="secondary-button" type="button" @click="emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button class="primary-button" type="submit" :disabled="!valid || saving">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </footer>
    </form>
  </OverlayDialog>
</template>

<style scoped>
.contact-favorite-toggle {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid var(--border);
}

.contact-favorite-toggle > span {
  display: flex;
  align-items: center;
  gap: 9px;
}

.contact-favorite-toggle input {
  position: relative;
  width: 42px;
  height: 24px;
  flex: 0 0 auto;
  appearance: none;
  background: #d8dde2;
  border-radius: 12px;
  cursor: pointer;
}

.contact-favorite-toggle input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.contact-favorite-toggle input:checked {
  color: var(--accent-strong);
  background: var(--accent);
}

.contact-favorite-toggle input:checked::before {
  transform: translateX(18px);
}

.contact-favorite-toggle input:focus-visible {
  outline: 3px solid rgb(17 120 100 / 18%);
  outline-offset: 2px;
}
</style>
