<script setup lang="ts">
import { computed } from 'vue'
import { CircleHelp, UserRound } from '@lucide/vue'
import { initials } from '../utils/format'

const props = withDefaults(
  defineProps<{
    name: string
    src?: string
    size?: 'small' | 'medium' | 'large'
    fallback?: 'initials' | 'person' | 'unknown'
    paletteKey?: string
  }>(),
  {
    src: '',
    size: 'medium',
    fallback: 'initials',
    paletteKey: ''
  }
)

const palette = computed(() => {
  let hash = 0
  const source = props.paletteKey.trim() || props.name
  for (const char of source) hash = (hash * 31 + char.codePointAt(0)!) % 8
  return `avatar--palette-${hash}`
})
const fallbackIcon = computed(() => {
  if (props.fallback === 'person') return UserRound
  if (props.fallback === 'unknown') return CircleHelp
  return undefined
})
const iconSize = computed(() => {
  if (props.size === 'large') return 30
  if (props.size === 'small') return 16
  return 20
})
const fallbackClass = computed(() =>
  props.src ? '' : `avatar--fallback-${props.fallback}`
)
</script>

<template>
  <span
    class="avatar"
    :class="[`avatar--${size}`, palette, fallbackClass]"
    aria-hidden="true"
  >
    <img v-if="src" :src="src" alt="" />
    <component
      :is="fallbackIcon"
      v-else-if="fallbackIcon"
      :size="iconSize"
      :stroke-width="1.8"
    />
    <template v-else>{{ initials(name) }}</template>
  </span>
</template>

<style scoped>
.avatar--fallback-unknown {
  color: var(--muted);
  background: var(--surface-hover);
  border-color: var(--border);
}
</style>
