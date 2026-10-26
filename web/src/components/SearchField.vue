<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search, X } from '@lucide/vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    label?: string
    /** Keyboard shortcut shown while empty and announced through aria-keyshortcuts. */
    shortcut?: string
    keyshortcuts?: string
  }>(),
  {
    placeholder: '',
    label: '',
    shortcut: '',
    keyshortcuts: ''
  }
)
const resolvedPlaceholder = computed(() => props.placeholder || t('common.search'))
const resolvedLabel = computed(() => props.label || t('common.search'))

const emit = defineEmits<{
  'update:modelValue': [value: string]
  focus: []
}>()
</script>

<template>
  <label class="search-field">
    <Search :size="18" aria-hidden="true" />
    <span class="sr-only">{{ resolvedLabel }}</span>
    <input
      :value="modelValue"
      type="search"
      :placeholder="resolvedPlaceholder"
      :aria-keyshortcuts="keyshortcuts || undefined"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @focus="emit('focus')"
    />
    <kbd v-if="shortcut && !modelValue" class="search-field__shortcut" aria-hidden="true">
      {{ shortcut }}
    </kbd>
    <button
      v-if="modelValue"
      class="icon-button icon-button--quiet"
      type="button"
      :title="t('common.clear')"
      :aria-label="t('common.clearSearch')"
      @click="emit('update:modelValue', '')"
    >
      <X :size="16" />
    </button>
  </label>
</template>
