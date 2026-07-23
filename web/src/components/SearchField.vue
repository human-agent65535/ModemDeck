<script setup lang="ts">
import { Search, X } from '@lucide/vue'

withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    label?: string
  }>(),
  {
    placeholder: '搜索',
    label: '搜索'
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  focus: []
}>()
</script>

<template>
  <label class="search-field">
    <Search :size="18" aria-hidden="true" />
    <span class="sr-only">{{ label }}</span>
    <input
      :value="modelValue"
      type="search"
      :placeholder="placeholder"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      @focus="emit('focus')"
    />
    <button
      v-if="modelValue"
      class="icon-button icon-button--quiet"
      type="button"
      title="清除"
      aria-label="清除搜索"
      @click="emit('update:modelValue', '')"
    >
      <X :size="16" />
    </button>
  </label>
</template>
