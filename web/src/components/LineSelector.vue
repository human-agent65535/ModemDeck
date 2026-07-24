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
import { ChevronDown, RadioTower, Star } from '@lucide/vue'
import type { CommunicationCapabilityName, LineSummary } from '../api/types'
import { lineKey, lineLabel, lineSupports } from '../state/workspace'

type SelectorOption = {
  value: string
  name: string
  details: string
  defaultLine: boolean
}

const props = withDefaults(
  defineProps<{
    modelValue: string
    lines: LineSummary[]
    label?: string
    placeholder?: string
    defaultDeviceImei?: string
    capability?: CommunicationCapabilityName
    unavailableLabel?: string
    includeAll?: boolean
    allValue?: string
    allLabel?: string
    allDescription?: string
    valueField?: 'line_key' | 'device_imei'
    placement?: 'down' | 'up'
    disabled?: boolean
  }>(),
  {
    label: '线路',
    placeholder: '选择线路',
    defaultDeviceImei: '',
    capability: undefined,
    unavailableLabel: '不可用',
    includeAll: false,
    allValue: 'all',
    allLabel: '全部线路',
    allDescription: '显示所有线路',
    valueField: 'line_key',
    placement: 'down',
    disabled: false
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
const componentID = useId()
const labelID = `${componentID}-label`
const listboxID = `${componentID}-listbox`

function lineValue(line: LineSummary): string {
  return props.valueField === 'device_imei' ? line.device_imei : lineKey(line)
}

const selectedLine = computed(() =>
  props.lines.find(line => lineValue(line) === props.modelValue)
)
const selectedIsDefault = computed(
  () =>
    Boolean(selectedLine.value?.device_imei) &&
    selectedLine.value?.device_imei === props.defaultDeviceImei
)

function lineDetails(line: LineSummary): string {
  const name = lineLabel(line)
  const details = [
    line.phone_number,
    line.operator && line.operator !== name ? line.operator : ''
  ].filter(Boolean)
  if (
    props.capability &&
    lineSupports(line, props.capability) === false
  ) {
    details.push(props.unavailableLabel)
  }
  return details.join(' · ') || '蜂窝线路'
}

const options = computed<SelectorOption[]>(() => {
  const result: SelectorOption[] = props.includeAll
    ? [
        {
          value: props.allValue,
          name: props.allLabel,
          details: props.allDescription,
          defaultLine: false
        }
      ]
    : []
  for (const line of props.lines) {
    const value = lineValue(line)
    if (!value) continue
    result.push({
      value,
      name: lineLabel(line),
      details: lineDetails(line),
      defaultLine:
        Boolean(line.device_imei) && line.device_imei === props.defaultDeviceImei
    })
  }
  return result
})

const selectedOption = computed(() =>
  options.value.find(option => option.value === props.modelValue)
)
const displayName = computed(
  () => selectedOption.value?.name || props.placeholder
)
const displayDetails = computed(
  () => selectedOption.value?.details || '请选择要使用的线路'
)

function selectedIndex(): number {
  const index = options.value.findIndex(option => option.value === props.modelValue)
  return index >= 0 ? index : 0
}

function focusOption(): void {
  void nextTick(() => {
    root.value
      ?.querySelector<HTMLElement>(`[data-option-index="${activeIndex.value}"]`)
      ?.focus()
  })
}

function openMenu(edge?: 'first' | 'last'): void {
  if (props.disabled || options.value.length === 0) return
  open.value = true
  activeIndex.value =
    edge === 'first'
      ? 0
      : edge === 'last'
        ? options.value.length - 1
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

function selectOption(value: string): void {
  if (value !== props.modelValue) {
    emit('update:modelValue', value)
    emit('change', value)
  }
  closeMenu(true)
}

function moveActive(offset: number): void {
  const count = options.value.length
  if (count === 0) return
  activeIndex.value = (activeIndex.value + offset + count) % count
  focusOption()
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
  const option = options.value[index]
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
      activeIndex.value = 0
      focusOption()
      break
    case 'End':
      event.preventDefault()
      activeIndex.value = options.value.length - 1
      focusOption()
      break
    case 'Enter':
    case ' ':
      event.preventDefault()
      if (option) selectOption(option.value)
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

function onFocusOut(): void {
  void nextTick(() => {
    if (open.value && !root.value?.contains(document.activeElement)) closeMenu()
  })
}

watch(
  () => props.disabled,
  disabled => {
    if (disabled) closeMenu()
  }
)

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))
</script>

<template>
  <div
    ref="root"
    class="line-selector"
    :class="{ 'is-disabled': disabled, 'is-open': open }"
    @focusout="onFocusOut"
  >
    <span :id="labelID" class="line-selector__label">{{ label }}</span>
    <button
      ref="trigger"
      class="line-selector__control"
      type="button"
      :disabled="disabled"
      :aria-label="`${label}：${displayName}`"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listboxID"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    >
      <span class="line-selector__icon" aria-hidden="true">
        <RadioTower :size="20" />
      </span>
      <span class="line-selector__identity">
        <strong>{{ displayName }}</strong>
        <small>{{ displayDetails }}</small>
      </span>
      <span v-if="selectedIsDefault" class="line-selector__default">
        <Star :size="12" fill="currentColor" aria-hidden="true" />
        默认
      </span>
      <ChevronDown
        class="line-selector__chevron"
        :class="{ 'is-open': open }"
        :size="18"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="open"
      :id="listboxID"
      class="line-selector__options"
      :class="{ 'is-up': placement === 'up' }"
      role="listbox"
      :aria-labelledby="labelID"
    >
      <button
        v-for="(option, index) in options"
        :key="option.value"
        class="line-selector__option"
        :class="{
          'is-active': activeIndex === index,
          'is-selected': modelValue === option.value
        }"
        type="button"
        role="option"
        :aria-selected="modelValue === option.value"
        :data-option-index="index"
        tabindex="-1"
        @pointermove="activeIndex = index"
        @click="selectOption(option.value)"
        @keydown="onOptionKeydown($event, index)"
      >
        <span class="line-selector__option-icon" aria-hidden="true">
          <RadioTower :size="17" />
        </span>
        <span class="line-selector__option-copy">
          <strong>{{ option.name }}</strong>
          <small>{{ option.details }}</small>
        </span>
        <span v-if="option.defaultLine" class="line-selector__option-default">
          <Star :size="12" fill="currentColor" />
          默认
        </span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.line-selector {
  position: relative;
  display: grid;
  min-width: 0;
  gap: 7px;
}

.line-selector__label {
  color: var(--text);
  font-size: 13px;
  font-weight: 700;
}

.line-selector__control {
  position: relative;
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 64px;
  align-items: center;
  grid-template-columns: 38px minmax(0, 1fr) auto auto;
  gap: 10px;
  padding: 8px 12px;
  color: inherit;
  font: inherit;
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  cursor: pointer;
  transition:
    border-color 140ms ease,
    box-shadow 140ms ease,
    background 140ms ease;
}

.line-selector__control:hover {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.line-selector__control:focus-visible,
.line-selector.is-open .line-selector__control {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px rgb(17 120 100 / 12%);
}

.line-selector__icon {
  display: inline-flex;
  width: 38px;
  height: 38px;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.line-selector__identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.line-selector__identity strong,
.line-selector__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-selector__identity strong {
  color: var(--text);
  font-size: 15px;
  font-weight: 700;
}

.line-selector__identity small {
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
}

.line-selector__default {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 700;
  background: var(--accent-soft);
  border-radius: 5px;
}

.line-selector__chevron {
  color: var(--muted);
  transition: transform 140ms ease;
}

.line-selector__chevron.is-open {
  transform: rotate(180deg);
}

.line-selector__options {
  position: absolute;
  z-index: 60;
  top: calc(100% + 6px);
  right: 0;
  left: 0;
  display: grid;
  width: 100%;
  max-height: min(320px, 45vh);
  padding: 5px;
  overflow-y: auto;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  box-shadow: 0 14px 32px rgb(24 39 55 / 18%);
}

.line-selector__options.is-up {
  top: auto;
  bottom: calc(100% + 6px);
}

.line-selector__option {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 54px;
  align-items: center;
  grid-template-columns: 32px minmax(0, 1fr) auto;
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

.line-selector__option.is-active,
.line-selector__option:hover {
  background: var(--surface-subtle);
}

.line-selector__option.is-selected {
  background: var(--accent-soft);
}

.line-selector__option:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.line-selector__option-icon {
  display: inline-flex;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 50%;
}

.line-selector__option-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.line-selector__option-copy strong,
.line-selector__option-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-selector__option-copy strong {
  font-size: 14px;
}

.line-selector__option-copy small {
  color: var(--muted);
  font-size: 12px;
}

.line-selector__option-default {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
}

.line-selector.is-disabled {
  opacity: 0.6;
}

.line-selector.is-disabled .line-selector__control {
  cursor: not-allowed;
}

@media (max-width: 420px) {
  .line-selector__control {
    grid-template-columns: 38px minmax(0, 1fr) auto;
  }

  .line-selector__default {
    display: none;
  }
}
</style>
