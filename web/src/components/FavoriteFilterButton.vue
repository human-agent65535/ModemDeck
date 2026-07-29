<script setup lang="ts">
import { Star } from '@lucide/vue'

defineProps<{
  active: boolean
  label: string
}>()

const emit = defineEmits<{
  toggle: []
}>()
</script>

<template>
  <button
    class="favorite-filter-button"
    :class="{ 'is-active': active }"
    type="button"
    :aria-pressed="active"
    :title="label"
    :aria-label="label"
    @click="emit('toggle')"
  >
    <Star :size="16" :fill="active ? 'currentColor' : 'none'" aria-hidden="true" />
    <span>{{ label }}</span>
  </button>
</template>

<style scoped>
.favorite-filter-button {
  display: inline-grid;
  width: 38px;
  height: 34px;
  flex: 0 0 38px;
  place-items: center;
  color: var(--muted);
  background: transparent;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.favorite-filter-button:hover:not(:disabled),
.favorite-filter-button.is-active {
  color: #a86400;
  background: transparent;
  border-color: #d6ae55;
}

.favorite-filter-button span {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
