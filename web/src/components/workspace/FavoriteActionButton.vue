<script setup lang="ts">
import { Star } from '@lucide/vue'

withDefaults(
  defineProps<{
    active: boolean
    disabled?: boolean
    activateLabel: string
    deactivateLabel: string
  }>(),
  {
    disabled: false
  }
)

defineEmits<{
  toggle: []
}>()
</script>

<template>
  <button
    class="icon-button workspace-favorite-action"
    :class="{ 'is-active': active }"
    type="button"
    :disabled="disabled"
    :title="active ? deactivateLabel : activateLabel"
    :aria-label="active ? deactivateLabel : activateLabel"
    :aria-pressed="active"
    @click="$emit('toggle')"
  >
    <Star :size="18" :fill="active ? 'currentColor' : 'none'" />
    <span class="workspace-favorite-action__label">
      {{ active ? deactivateLabel : activateLabel }}
    </span>
  </button>
</template>

<style scoped>
.workspace-favorite-action:hover:not(:disabled),
.workspace-favorite-action.is-active {
  color: var(--favorite);
  background: transparent;
}

.workspace-favorite-action__label {
  display: none;
}
</style>
