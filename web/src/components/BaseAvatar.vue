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

const hue = computed(() => {
  let hash = 0
  for (const char of props.name) hash = (hash * 31 + char.codePointAt(0)!) % 360
  return `hsl(${hash} 42% 92%)`
})
</script>

<template>
  <span class="avatar" :class="`avatar--${size}`" :style="{ backgroundColor: hue }" aria-hidden="true">
    {{ initials(name) }}
  </span>
</template>
