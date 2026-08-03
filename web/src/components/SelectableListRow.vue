<script setup lang="ts">
import SelectionCheckIndicator from './SelectionCheckIndicator.vue'

const props = defineProps<{
  active: boolean
  selected: boolean
  label: string
  arriving?: boolean
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
      'is-checked': selected,
      'is-arriving': arriving
    }"
    @click.capture="intercept"
    @keydown.capture="interceptKey"
  >
    <SelectionCheckIndicator
      v-if="active"
      class="selectable-list-row__check"
      role="checkbox"
      :checked="selected"
      :aria-checked="selected"
      :aria-label="label"
    />
    <slot />
  </div>
</template>

<style scoped>
.selectable-list-row {
  position: relative;
  min-width: 0;
}

.selectable-list-row.is-arriving {
  animation: list-row-arrival var(--motion-slow) var(--ease-standard) both;
}

.selectable-list-row.is-arriving::after {
  position: absolute;
  z-index: 5;
  inset: 0;
  background: var(--accent-soft);
  box-shadow: inset 3px 0 var(--accent);
  content: '';
  pointer-events: none;
  animation: list-row-arrival-highlight var(--motion-slow) var(--ease-standard) both;
}

@keyframes list-row-arrival {
  from {
    opacity: 0;
    transform: translateY(var(--space-2));
  }
}

@keyframes list-row-arrival-highlight {
  from {
    opacity: 0.72;
  }

  to {
    opacity: 0;
  }
}

.selectable-list-row__check {
  position: absolute;
  z-index: 4;
  top: 50%;
  left: 14px;
  pointer-events: none;
  transform: translateY(-50%);
}

.selectable-list-row.is-selecting :deep(.list-item) {
  padding-left: 50px;
  cursor: pointer;
}

.selectable-list-row.is-selecting :deep(.list-item.is-selected)::before {
  display: none;
}

@media (prefers-reduced-motion: reduce) {
  .selectable-list-row.is-arriving,
  .selectable-list-row.is-arriving::after {
    animation: none;
  }

  .selectable-list-row.is-arriving::after {
    display: none;
  }
}
</style>
