<script setup lang="ts">
import { computed } from 'vue'
import { initials } from '../utils/format'

const props = withDefaults(
  defineProps<{
    name: string
    size?: 'small' | 'medium' | 'large'
  }>(),
  { size: 'medium' }
)

const palette = computed(() => {
  let hash = 0
  for (const char of props.name) hash = (hash * 31 + char.codePointAt(0)!) % 8
  return `avatar--palette-${hash}`
})
</script>

<template>
  <span class="avatar" :class="[`avatar--${size}`, palette]" aria-hidden="true">
    {{ initials(name) }}
  </span>
</template>
