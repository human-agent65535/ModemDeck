<script setup lang="ts">
import { CircleOff } from '@lucide/vue'

defineProps<{
  disabled: boolean
  label: string
}>()
</script>

<template>
  <span
    v-if="disabled"
    class="settings-resource-status"
    role="img"
    :title="label"
    :aria-label="label"
  >
    <CircleOff :size="16" aria-hidden="true" />
  </span>
</template>

<style scoped>
.settings-resource-status {
  display: inline-grid;
  width: 20px;
  height: 20px;
  flex: 0 0 20px;
  place-items: center;
  color: var(--muted);
}
</style>
