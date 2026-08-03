<script setup lang="ts">
import { X } from '@lucide/vue'
import SelectionCheckIndicator from './SelectionCheckIndicator.vue'

defineProps<{
  selected: number
  total: number
  selectedLabel: string
  selectAllLabel: string
  clearAllLabel: string
  doneLabel: string
  busy?: boolean
}>()

const emit = defineEmits<{
  selectAll: []
  done: []
}>()
</script>

<template>
  <footer class="batch-action-bar" aria-live="polite">
    <button
      class="batch-action-bar__select"
      type="button"
      :disabled="busy || total === 0"
      :title="selected === total && total > 0 ? clearAllLabel : selectAllLabel"
      :aria-label="selected === total && total > 0 ? clearAllLabel : selectAllLabel"
      @click="emit('selectAll')"
    >
      <SelectionCheckIndicator
        :checked="selected === total && total > 0"
        aria-hidden="true"
      />
      <span class="batch-action-bar__select-label">
        {{ selected === total && total > 0 ? clearAllLabel : selectAllLabel }}
      </span>
    </button>
    <strong :title="selectedLabel" :aria-label="selectedLabel">
      {{ selectedLabel }}
    </strong>
    <div class="batch-action-bar__actions">
      <slot />
    </div>
    <button
      class="batch-action-bar__done"
      type="button"
      :disabled="busy"
      :title="doneLabel"
      :aria-label="doneLabel"
      @click="emit('done')"
    >
      <X :size="18" aria-hidden="true" />
    </button>
  </footer>
</template>

<style scoped>
.batch-action-bar {
  --batch-action-bar-height: 52px;

  position: relative;
  z-index: 8;
  display: flex;
  height: var(--batch-action-bar-height);
  min-height: var(--batch-action-bar-height);
  max-height: var(--batch-action-bar-height);
  flex: 0 0 var(--batch-action-bar-height);
  align-items: center;
  gap: 4px;
  padding: 7px 10px;
  overflow: hidden;
  background: rgb(255 255 255 / 97%);
  border-top: 1px solid var(--border);
  box-shadow: 0 -6px 18px rgb(16 24 40 / 7%);
}

.batch-action-bar > strong {
  min-width: 0;
  max-width: 92px;
  flex: 0 1 auto;
  margin-left: -2px;
  overflow: hidden;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  font-size: 12px;
}

.batch-action-bar button {
  display: inline-flex;
  width: 38px;
  min-height: 36px;
  flex: 0 0 38px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0;
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
  background: transparent;
  border-radius: 7px;
}

.batch-action-bar button:hover:not(:disabled) {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.batch-action-bar__actions {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
}

.batch-action-bar__actions :deep(button) {
  display: inline-flex;
  width: 38px;
  min-height: 36px;
  flex: 0 0 38px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 0;
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
  background: transparent;
  border-radius: 7px;
}

.batch-action-bar__actions :deep(button:hover:not(:disabled)) {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.batch-action-bar__actions :deep(button.is-danger:hover:not(:disabled)) {
  color: var(--danger);
  background: var(--danger-soft);
}

.batch-action-bar__done {
  width: 36px;
  flex: 0 0 36px;
  padding: 0;
}

.batch-action-bar__select-label,
.batch-action-bar__actions :deep(button span) {
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

@media (max-width: 860px) {
  .batch-action-bar {
    position: fixed;
    z-index: 39;
    right: 0;
    bottom: var(--mobile-nav-height);
    left: 0;
  }
}

@media (max-width: 560px) {
  .batch-action-bar {
    gap: 3px;
    padding-inline: 5px;
  }

  .batch-action-bar > strong {
    max-width: 76px;
    font-size: 10px;
  }

}
</style>
