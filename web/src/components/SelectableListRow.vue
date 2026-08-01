<script setup lang="ts">
import { Check } from '@lucide/vue'

const props = defineProps<{
  active: boolean
  selected: boolean
  label: string
}>()

const emit = defineEmits<{
  toggle: []
}>()

function intercept(event: MouseEvent): void {
  if (!props.active) return
  event.preventDefault()
  event.stopPropagation()
  emit('toggle')
}

function interceptKey(event: KeyboardEvent): void {
  if (!props.active || (event.key !== 'Enter' && event.key !== ' ')) return
  event.preventDefault()
  event.stopPropagation()
  emit('toggle')
}
</script>

<template>
  <div
    class="selectable-list-row"
    :class="{
      'is-selecting': active,
      'is-checked': selected
    }"
    @click.capture="intercept"
    @keydown.capture="interceptKey"
  >
    <span
      v-if="active"
      class="selectable-list-row__check"
      role="checkbox"
      :aria-checked="selected"
      :aria-label="label"
    >
      <Check v-if="selected" :size="15" stroke-width="3" aria-hidden="true" />
    </span>
    <slot />
  </div>
</template>

<style scoped>
.selectable-list-row {
  position: relative;
  min-width: 0;
}

.selectable-list-row__check {
  position: absolute;
  z-index: 4;
  top: 50%;
  left: 14px;
  display: grid;
  width: 22px;
  height: 22px;
  place-items: center;
  color: var(--on-accent);
  background: var(--surface);
  border: 2px solid var(--faint);
  border-radius: 5px;
  pointer-events: none;
  transform: translateY(-50%);
}

.selectable-list-row.is-checked .selectable-list-row__check {
  background: var(--accent);
  border-color: var(--accent);
}

.selectable-list-row.is-selecting :deep(.list-item) {
  padding-left: 50px;
  cursor: pointer;
}

.selectable-list-row.is-selecting :deep(.list-item.is-selected)::before {
  display: none;
}
</style>
