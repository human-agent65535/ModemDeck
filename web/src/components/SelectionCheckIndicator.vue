<script setup lang="ts">
import { Check } from '@lucide/vue'

defineProps<{
  checked: boolean
}>()
</script>

<template>
  <span
    class="selection-check-indicator"
    :class="{ 'is-checked': checked }"
  >
    <Check v-if="checked" :size="15" stroke-width="3" />
  </span>
</template>

<style scoped>
.selection-check-indicator {
  display: grid;
  width: 22px;
  height: 22px;
  flex: 0 0 22px;
  place-items: center;
  color: var(--on-accent);
  background: var(--surface);
  border: 2px solid var(--faint);
  border-radius: 5px;
}

.selection-check-indicator.is-checked {
  background: var(--accent);
  border-color: var(--accent);
}
</style>
