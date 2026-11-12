<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { Check, ChevronDown } from '@lucide/vue'
import PopoverTransition from './PopoverTransition.vue'
import { useListbox } from '../composables/useListbox'

type SelectOption = {
  value: string
  label: string
  description?: string
  disabled?: boolean
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    options: SelectOption[]
    label: string
    placeholder?: string
    disabled?: boolean
    placement?: 'down' | 'up'
    compact?: boolean
  }>(),
  {
    placeholder: '',
    disabled: false,
    placement: 'down',
    compact: false
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const listboxID = `${useId()}-select-listbox`
const selectedOption = computed(() =>
  props.options.find(option => option.value === props.modelValue)
)
const displayLabel = computed(
  () => selectedOption.value?.label || props.placeholder || props.modelValue
)

const { open, activeIndex, closeMenu, toggleMenu, onTriggerKeydown, onOptionKeydown } =
  useListbox<SelectOption>({
    root,
    trigger,
    options: () => props.options,
    disabled: () => props.disabled,
    selectedIndex: () => props.options.findIndex(option => option.value === props.modelValue),
    optionSelector: index => `[data-select-index="${index}"]`,
    select: selectOption
  })

function selectOption(option: SelectOption): void {
  if (option.disabled) return
  if (option.value !== props.modelValue) {
    emit('update:modelValue', option.value)
    emit('change', option.value)
  }
  closeMenu(true)
}
</script>

<template>
  <div
    ref="root"
    class="select-control"
    :class="{
      'is-disabled': disabled,
      'is-open': open,
      'is-compact': compact
    }"
  >
    <button
      ref="trigger"
      class="select-control__trigger"
      type="button"
      :disabled="disabled"
      :aria-label="`${label}: ${displayLabel}`"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listboxID"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    >
      <span>{{ displayLabel }}</span>
      <ChevronDown
        class="select-control__chevron"
        :class="{ 'is-open': open }"
        :size="18"
        aria-hidden="true"
      />
    </button>

    <PopoverTransition>
      <div
        v-if="open"
        :id="listboxID"
        class="select-control__options"
        :class="{ 'is-up': placement === 'up' }"
        role="listbox"
        :aria-label="label"
      >
        <button
          v-for="(option, index) in options"
          :key="option.value"
          class="select-control__option"
          :class="{
            'is-active': activeIndex === index,
            'is-selected': modelValue === option.value,
            'is-disabled': option.disabled
          }"
          type="button"
          role="option"
          :aria-selected="modelValue === option.value"
          :aria-disabled="option.disabled"
          :data-select-index="index"
          tabindex="-1"
          @pointermove="activeIndex = index"
          @click="selectOption(option)"
          @keydown="onOptionKeydown($event, index)"
        >
          <span class="select-control__option-copy">
            <strong>{{ option.label }}</strong>
            <small v-if="option.description">{{ option.description }}</small>
          </span>
          <Check v-if="modelValue === option.value" :size="16" aria-hidden="true" />
        </button>
      </div>
    </PopoverTransition>
  </div>
</template>

<style scoped>
.select-control {
  position: relative;
  width: 100%;
  min-width: 0;
}

.select-control__trigger {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 42px;
  align-items: center;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  padding: 0 13px;
  color: var(--text);
  font: inherit;
  font-size: var(--text-sm);
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition:
    border-color var(--motion-fast) var(--ease-standard),
    box-shadow var(--motion-fast) var(--ease-standard),
    background var(--motion-fast) var(--ease-standard);
}

.select-control__trigger > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.select-control__trigger:hover {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.select-control__trigger:focus-visible,
.select-control.is-open .select-control__trigger {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.select-control__trigger:disabled {
  color: var(--muted);
  background: var(--surface-subtle);
  border-color: var(--border);
  cursor: not-allowed;
}

.select-control__chevron {
  color: var(--muted);
  transition: transform var(--motion-fast) var(--ease-standard);
}

.select-control__chevron.is-open {
  transform: rotate(180deg);
}

.select-control__options {
  position: absolute;
  z-index: var(--layer-popover);
  top: calc(100% + 6px);
  right: 0;
  left: 0;
  display: grid;
  max-height: min(320px, 45vh);
  padding: 5px;
  overflow-y: auto;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-lg);
}

.select-control__options.is-up {
  top: auto;
  bottom: calc(100% + 6px);
}

.select-control__option {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 40px;
  align-items: center;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  padding: 7px 9px;
  color: var(--text);
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
  border-radius: var(--radius-xs);
  cursor: pointer;
}

.select-control__option.is-active,
.select-control__option:hover {
  background: var(--surface-subtle);
}

.select-control__option.is-selected {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.select-control__option.is-disabled {
  color: var(--muted);
  cursor: not-allowed;
  opacity: 0.62;
}

.select-control__option:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.select-control__option-copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.select-control__option-copy strong,
.select-control__option-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.select-control__option-copy strong {
  font-size: var(--text-xs);
  font-weight: var(--weight-strong);
}

.select-control__option-copy small {
  color: var(--muted);
  font-size: var(--text-2xs);
}

.select-control.is-compact .select-control__trigger {
  min-height: 36px;
  padding: 0 10px;
  border-radius: var(--radius-sm);
}

.select-control.is-compact .select-control__options {
  min-width: 180px;
}

@media (prefers-reduced-motion: reduce) {
  .select-control__trigger,
  .select-control__chevron {
    transition: none;
  }
}
</style>
