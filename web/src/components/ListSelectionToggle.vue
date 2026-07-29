<script setup lang="ts">
import { ListChecks } from '@lucide/vue'

defineProps<{
  active: boolean
  label: string
  doneLabel: string
  disabled?: boolean
}>()

const emit = defineEmits<{
  toggle: []
}>()
</script>

<template>
  <button
    class="pane-selection-toggle"
    :class="{ 'is-active': active }"
    type="button"
    :disabled="disabled"
    :aria-pressed="active"
    :title="active ? doneLabel : label"
    @click="emit('toggle')"
  >
    <ListChecks :size="18" aria-hidden="true" />
    <span>{{ active ? doneLabel : label }}</span>
  </button>
</template>

<style scoped>
.pane-selection-toggle {
  display: inline-flex;
  width: 38px;
  min-height: 34px;
  flex: 0 0 38px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-left: auto;
  padding: 0;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.pane-selection-toggle:hover:not(:disabled),
.pane-selection-toggle.is-active {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: var(--accent);
}

.pane-selection-toggle span {
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

.pane-search-row .pane-selection-toggle {
  margin-left: 0;
}
</style>
