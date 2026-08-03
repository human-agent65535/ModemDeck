<script setup lang="ts">
import { Check } from '@lucide/vue'
import type { LineSummary } from '../../api/types'
import LineIdentity from '../LineIdentity.vue'

type LineScopeOption = {
  id: string
  label: string
  details: string
  line?: LineSummary
}

withDefaults(
  defineProps<{
    options: LineScopeOption[]
    selectedIds: string[]
    disabled?: boolean
    allSelected?: boolean
    allLabel?: string
    allDescription?: string
  }>(),
  {
    disabled: false,
    allSelected: false,
    allLabel: '',
    allDescription: ''
  }
)

const emit = defineEmits<{
  selectAll: []
  toggleLine: [id: string, checked: boolean]
}>()

function onLineChange(id: string, event: Event): void {
  emit('toggleLine', id, (event.currentTarget as HTMLInputElement).checked)
}
</script>

<template>
  <div class="settings-line-scope-list">
    <label
      v-if="allLabel"
      class="settings-line-scope-option"
      :class="{
        'is-selected': allSelected,
        'is-disabled': disabled
      }"
    >
      <input
        class="ui-choice-input--hidden"
        :checked="allSelected"
        type="checkbox"
        :disabled="disabled"
        @click.prevent="emit('selectAll')"
      />
      <LineIdentity
        :name="allLabel"
        :details="allDescription"
        all
      />
      <span class="settings-line-scope-option__check" aria-hidden="true">
        <Check v-if="allSelected" :size="16" />
      </span>
    </label>

    <label
      v-for="option in options"
      :key="option.id"
      class="settings-line-scope-option"
      :class="{
        'is-selected': selectedIds.includes(option.id),
        'is-disabled': disabled
      }"
    >
      <input
        class="ui-choice-input--hidden"
        :checked="selectedIds.includes(option.id)"
        type="checkbox"
        :disabled="disabled"
        @change="onLineChange(option.id, $event)"
      />
      <LineIdentity
        :name="option.label"
        :details="option.details"
        :line="option.line"
      />
      <span class="settings-line-scope-option__check" aria-hidden="true">
        <Check v-if="selectedIds.includes(option.id)" :size="16" />
      </span>
    </label>
  </div>
</template>

<style scoped>
.settings-line-scope-list {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.settings-line-scope-option {
  position: relative;
  display: grid;
  min-width: 0;
  min-height: 64px;
  align-items: center;
  grid-template-columns: minmax(0, 1fr) 24px;
  gap: 10px;
  padding: 8px 12px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  cursor: pointer;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    box-shadow var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard),
    transform var(--motion-fast) var(--ease-standard);
}

.settings-line-scope-option:hover {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.settings-line-scope-option.is-selected {
  background: var(--surface-selected);
  border-color: var(--accent);
}

.settings-line-scope-option:active {
  transform: scale(0.995);
}

.settings-line-scope-option.is-disabled {
  cursor: not-allowed;
  opacity: 0.72;
}

.settings-line-scope-option:has(input:focus-visible) {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px rgb(17 120 100 / 12%);
}

.settings-line-scope-option__check {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  color: transparent;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.settings-line-scope-option.is-selected .settings-line-scope-option__check {
  color: var(--on-accent);
  background: var(--accent);
  border-color: var(--accent);
}

@media (max-width: 860px) {
  .settings-line-scope-list {
    grid-template-columns: minmax(0, 1fr);
  }
}

@container (max-width: 700px) {
  .settings-line-scope-list {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
