<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
  watch
} from 'vue'
import { Check, ChevronDown } from '@lucide/vue'
import PopoverTransition from './PopoverTransition.vue'

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
const open = ref(false)
const activeIndex = ref(0)
const listboxID = `${useId()}-select-listbox`
const selectedOption = computed(() =>
  props.options.find(option => option.value === props.modelValue)
)
const displayLabel = computed(
  () => selectedOption.value?.label || props.placeholder || props.modelValue
)

function selectedIndex(): number {
  const index = props.options.findIndex(option => option.value === props.modelValue)
  return index >= 0 ? index : firstEnabledIndex()
}

function firstEnabledIndex(): number {
  const index = props.options.findIndex(option => !option.disabled)
  return index >= 0 ? index : 0
}

function lastEnabledIndex(): number {
  for (let index = props.options.length - 1; index >= 0; index -= 1) {
    if (!props.options[index]?.disabled) return index
  }
  return 0
}

function focusOption(): void {
  void nextTick(() => {
    root.value
      ?.querySelector<HTMLElement>(`[data-select-index="${activeIndex.value}"]`)
      ?.focus()
  })
}

function openMenu(edge?: 'first' | 'last'): void {
  if (props.disabled || props.options.length === 0) return
  open.value = true
  activeIndex.value =
    edge === 'first'
      ? firstEnabledIndex()
      : edge === 'last'
        ? lastEnabledIndex()
        : selectedIndex()
  focusOption()
}

function closeMenu(returnFocus = false): void {
  open.value = false
  if (returnFocus) void nextTick(() => trigger.value?.focus())
}

function toggleMenu(): void {
  if (open.value) closeMenu(true)
  else openMenu()
}

function selectOption(option: SelectOption): void {
  if (option.disabled) return
  if (option.value !== props.modelValue) {
    emit('update:modelValue', option.value)
    emit('change', option.value)
  }
  closeMenu(true)
}

function moveActive(offset: number): void {
  const count = props.options.length
  if (count === 0) return
  let next = activeIndex.value
  for (let attempts = 0; attempts < count; attempts += 1) {
    next = (next + offset + count) % count
    if (!props.options[next]?.disabled) {
      activeIndex.value = next
      focusOption()
      return
    }
  }
}

function onTriggerKeydown(event: KeyboardEvent): void {
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault()
      if (open.value) moveActive(1)
      else openMenu('first')
      break
    case 'ArrowUp':
      event.preventDefault()
      if (open.value) moveActive(-1)
      else openMenu('last')
      break
    case 'Enter':
    case ' ':
      event.preventDefault()
      toggleMenu()
      break
    case 'Escape':
      if (open.value) {
        event.preventDefault()
        closeMenu(true)
      }
      break
  }
}

function onOptionKeydown(event: KeyboardEvent, index: number): void {
  activeIndex.value = index
  const option = props.options[index]
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault()
      moveActive(1)
      break
    case 'ArrowUp':
      event.preventDefault()
      moveActive(-1)
      break
    case 'Home':
      event.preventDefault()
      activeIndex.value = firstEnabledIndex()
      focusOption()
      break
    case 'End':
      event.preventDefault()
      activeIndex.value = lastEnabledIndex()
      focusOption()
      break
    case 'Enter':
    case ' ':
      event.preventDefault()
      if (option) selectOption(option)
      break
    case 'Escape':
      event.preventDefault()
      closeMenu(true)
      break
    case 'Tab':
      closeMenu()
      break
  }
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (open.value && !root.value?.contains(event.target as Node)) closeMenu()
}

function onDocumentFocusIn(event: FocusEvent): void {
  if (open.value && !root.value?.contains(event.target as Node)) closeMenu()
}

watch(
  () => props.disabled,
  disabled => {
    if (disabled) closeMenu()
  }
)

watch(
  () => props.options,
  options => {
    if (options.length === 0) closeMenu()
    if (activeIndex.value >= options.length) activeIndex.value = firstEnabledIndex()
  },
  { deep: true }
)

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('focusin', onDocumentFocusIn)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('focusin', onDocumentFocusIn)
})
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
  font-size: 13px;
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 7px;
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
  border-radius: 7px;
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
  border-radius: 5px;
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
  font-size: 12px;
  font-weight: 650;
}

.select-control__option-copy small {
  color: var(--muted);
  font-size: 10px;
}

.select-control.is-compact .select-control__trigger {
  min-height: 36px;
  padding: 0 10px;
  border-radius: 6px;
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
