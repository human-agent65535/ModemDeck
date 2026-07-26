<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search } from '@lucide/vue'
import type { Contact, ContactPhone } from '../api/types'
import { primaryPhone } from '../utils/format'
import BaseAvatar from './BaseAvatar.vue'

type Suggestion = {
  contact: Contact
  phone: ContactPhone
}

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    modelValue: string
    contacts: Contact[]
    placeholder?: string
    autofocus?: boolean
  }>(),
  {
    placeholder: '',
    autofocus: false
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  select: [suggestion: Suggestion]
}>()

const input = ref<HTMLInputElement | null>(null)
const focused = ref(false)
const activeIndex = ref(0)
const inputId = `contact-suggest-${useId()}`
const listboxId = `${inputId}-listbox`
const resolvedPlaceholder = computed(() => props.placeholder || t('dialer.numberOrContact'))

const suggestions = computed<Suggestion[]>(() => {
  const query = props.modelValue.trim().toLocaleLowerCase()
  if (!query) return []
  const digits = query.replace(/\D/g, '')
  return props.contacts
    .flatMap(contact =>
      contact.phones
        .filter(phone => {
          const nameMatch = contact.display_name.toLocaleLowerCase().includes(query)
          const phoneMatch = digits.length > 0 && phone.number.replace(/\D/g, '').includes(digits)
          return nameMatch || phoneMatch
        })
        .map(phone => ({ contact, phone }))
    )
    .slice(0, 6)
})

const showSuggestions = computed(() => focused.value && suggestions.value.length > 0)
const activeDescendant = computed(() =>
  showSuggestions.value ? `${listboxId}-option-${activeIndex.value}` : undefined
)

watch(
  () => props.autofocus,
  value => {
    if (value) void nextTick(() => input.value?.focus())
  },
  { immediate: true }
)

watch(suggestions, () => {
  activeIndex.value = 0
})

function choose(suggestion: Suggestion): void {
  emit('update:modelValue', suggestion.phone.number)
  emit('select', suggestion)
  focused.value = false
}

function onKeydown(event: KeyboardEvent): void {
  if (!showSuggestions.value) return
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    activeIndex.value = (activeIndex.value + 1) % suggestions.value.length
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    activeIndex.value = (activeIndex.value - 1 + suggestions.value.length) % suggestions.value.length
  } else if (event.key === 'Enter') {
    const suggestion = suggestions.value[activeIndex.value]
    if (suggestion) {
      event.preventDefault()
      choose(suggestion)
    }
  } else if (event.key === 'Escape') {
    focused.value = false
  }
}

function onBlur(): void {
  window.setTimeout(() => {
    focused.value = false
  }, 120)
}
</script>

<template>
  <div class="suggest-input">
    <div class="suggest-input__field">
      <Search :size="18" aria-hidden="true" />
      <input
        :id="inputId"
        ref="input"
        :value="modelValue"
        type="tel"
        autocomplete="off"
        role="combobox"
        aria-autocomplete="list"
        aria-haspopup="listbox"
        :aria-expanded="showSuggestions"
        :aria-controls="listboxId"
        :aria-activedescendant="activeDescendant"
        :placeholder="resolvedPlaceholder"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
        @focus="focused = true"
        @blur="onBlur"
        @keydown="onKeydown"
      />
    </div>
    <div v-if="showSuggestions" :id="listboxId" class="suggest-menu" role="listbox">
      <button
        v-for="(suggestion, index) in suggestions"
        :id="`${listboxId}-option-${index}`"
        :key="`${suggestion.contact.id}:${suggestion.phone.id}`"
        class="suggest-menu__item"
        :class="{ 'is-active': index === activeIndex }"
        type="button"
        role="option"
        :aria-selected="index === activeIndex"
        @mousedown.prevent="choose(suggestion)"
      >
        <BaseAvatar
          :name="suggestion.contact.display_name"
          :src="suggestion.contact.avatar"
          size="small"
        />
        <span>
          <strong>{{ suggestion.contact.display_name }}</strong>
          <small>{{ suggestion.phone.label }} · {{ suggestion.phone.number }}</small>
        </span>
        <small v-if="suggestion.phone.number === primaryPhone(suggestion.contact.phones)">
          {{ t('contacts.primary') }}
        </small>
      </button>
    </div>
  </div>
</template>

<style scoped>
.suggest-input__field input::placeholder {
  font-size: 15px;
  font-weight: 500;
}
</style>
