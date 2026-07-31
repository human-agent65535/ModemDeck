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
import { useI18n } from 'vue-i18n'
import { CardSim, ChevronDown, ListFilter, Star } from '@lucide/vue'
import type { CommunicationCapabilityName, LineSummary } from '../api/types'
import { lineKey, lineLabel, lineSupports } from '../state/workspace'
import { lineTone } from '../utils/lineTone'
import LineIdentity from './LineIdentity.vue'
import PopoverTransition from './PopoverTransition.vue'

type SelectorOption = {
  value: string
  name: string
  details: string
  status: string
  defaultLine: boolean
  disabled: boolean
  line?: LineSummary
}

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    modelValue: string
    lines: LineSummary[]
    label?: string
    placeholder?: string
    defaultLineId?: string
    capability?: CommunicationCapabilityName
    unavailableLabel?: string
    includeAll?: boolean
    allValue?: string
    allLabel?: string
    allDescription?: string
    placement?: 'down' | 'up'
    disabled?: boolean
    disabledValues?: string[]
    disabledValueLabel?: string
    statusValues?: string[]
    statusValueLabel?: string
    compact?: boolean
    filterMode?: boolean
  }>(),
  {
    label: '',
    placeholder: '',
    defaultLineId: '',
    capability: undefined,
    unavailableLabel: '',
    includeAll: false,
    allValue: 'all',
    allLabel: '',
    allDescription: '',
    placement: 'down',
    disabled: false,
    disabledValues: () => [],
    disabledValueLabel: '',
    statusValues: () => [],
    statusValueLabel: '',
    compact: false,
    filterMode: false
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
const resolvedLabel = computed(() => props.label || t('lines.line'))
const resolvedPlaceholder = computed(() => props.placeholder || t('lines.selectLine'))
const resolvedUnavailableLabel = computed(
  () => props.unavailableLabel || t('lines.unavailable')
)
const resolvedAllLabel = computed(() => props.allLabel || t('lines.allLines'))
const resolvedAllDescription = computed(
  () => props.allDescription || t('lines.showAllLines')
)
const disabledValueSet = computed(() => new Set(props.disabledValues))
const statusValueSet = computed(() => new Set(props.statusValues))

function lineValue(line: LineSummary): string {
  return lineKey(line)
}

const selectedLine = computed(() =>
  props.lines.find(line => lineValue(line) === props.modelValue)
)
const selectedIsDefault = computed(
  () =>
    Boolean(selectedLine.value?.id) &&
    selectedLine.value?.id === props.defaultLineId
)
const isAllSelected = computed(
  () => props.includeAll && props.modelValue === props.allValue
)
function toneStyle(line?: LineSummary): Record<string, string> | undefined {
  if (!line) return undefined
  const tone = lineTone(line)
  return {
    '--line-tone-color': tone.foreground,
    '--line-tone-background': tone.background,
    '--line-tone-border': tone.border
  }
}
const identityVariant = computed<'control' | 'compact' | 'filter'>(() =>
  props.filterMode ? 'filter' : props.compact ? 'compact' : 'control'
)

function lineDetails(line: LineSummary, value: string): string {
  const name = lineLabel(line)
  const details = [
    line.phone_number,
    line.operator && line.operator !== name ? line.operator : ''
  ].filter(Boolean)
  if (
    props.capability &&
    lineSupports(line, props.capability) === false
  ) {
    details.push(resolvedUnavailableLabel.value)
  }
  if (disabledValueSet.value.has(value) && props.disabledValueLabel) {
    details.push(props.disabledValueLabel)
  }
  return details.join(' · ') || t('lines.cellularLine')
}

const options = computed<SelectorOption[]>(() => {
  const result: SelectorOption[] = props.includeAll
    ? [
        {
          value: props.allValue,
          name: resolvedAllLabel.value,
          details: resolvedAllDescription.value,
          status: '',
          defaultLine: false,
          disabled: false
        }
      ]
    : []
  for (const line of props.lines) {
    const value = lineValue(line)
    if (!value) continue
    result.push({
      value,
      name: lineLabel(line),
      details: lineDetails(line, value),
      status: statusValueSet.value.has(value) ? props.statusValueLabel : '',
      line,
      disabled: disabledValueSet.value.has(value),
      defaultLine:
        Boolean(line.id) && line.id === props.defaultLineId
    })
  }
  return result
})

const selectedOption = computed(() =>
  options.value.find(option => option.value === props.modelValue)
)
const displayName = computed(
  () => selectedOption.value?.name || resolvedPlaceholder.value
)
const displayDetails = computed(
  () => selectedOption.value?.details || t('lines.chooseLine')
)
const displayStatus = computed(() => selectedOption.value?.status || '')

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

function selectOption(option: SelectorOption): void {
  if (option.disabled) return
  if (option.value !== props.modelValue) {
    emit('update:modelValue', option.value)
    emit('change', option.value)
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
    class="line-selector"
    :class="{
      'is-disabled': disabled,
      'is-open': open,
      'is-compact': compact,
      'is-filter': filterMode,
      'is-filtered': filterMode && Boolean(selectedLine),
      'is-all': isAllSelected
    }"
  >
    <span :id="labelID" class="line-selector__label">{{ resolvedLabel }}</span>
    <button
      ref="trigger"
      class="line-selector__control"
      type="button"
      :disabled="disabled"
      :aria-label="`${resolvedLabel}: ${displayName}`"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listboxID"
      @click="toggleMenu"
      @keydown="onTriggerKeydown"
    >
      <LineIdentity
        class="line-selector__selected"
        :name="displayName"
        :details="displayDetails"
        :line="selectedLine"
        :status="displayStatus"
        :is-default="selectedIsDefault"
        :default-label="t('lines.default')"
        :all="isAllSelected"
        :variant="identityVariant"
      />
      <ChevronDown
        class="line-selector__chevron"
        :class="{ 'is-open': open }"
        :size="18"
        aria-hidden="true"
      />
    </button>
    <PopoverTransition>
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
            'is-selected': modelValue === option.value,
            'is-disabled': option.disabled
          }"
          type="button"
          role="option"
          :aria-selected="modelValue === option.value"
          :aria-disabled="option.disabled"
          :data-option-index="index"
          tabindex="-1"
          @pointermove="activeIndex = index"
          @click="selectOption(option)"
          @keydown="onOptionKeydown($event, index)"
        >
          <span
            class="line-selector__option-icon"
            :style="toneStyle(option.line)"
            aria-hidden="true"
          >
            <CardSim v-if="option.line" :size="17" />
            <ListFilter v-else :size="17" />
          </span>
          <span class="line-selector__option-copy">
            <strong>{{ option.name }}</strong>
            <small>
              <span>{{ option.details }}</span>
              <em v-if="option.status">{{ option.status }}</em>
            </small>
          </span>
          <span v-if="option.defaultLine" class="line-selector__option-default">
            <Star :size="12" fill="currentColor" />
            {{ t('lines.default') }}
          </span>
        </button>
      </div>
    </PopoverTransition>
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
  grid-template-columns: minmax(0, 1fr) auto;
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

.line-selector__option-copy small > em {
  flex: 0 0 auto;
  color: var(--danger);
  font-style: normal;
  font-weight: 700;
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
  z-index: var(--layer-popover);
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

.line-selector__option.is-disabled {
  cursor: not-allowed;
}

.line-selector__option.is-disabled .line-selector__option-copy {
  opacity: 0.62;
}

.line-selector__option:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.line-selector.is-compact {
  width: 112px;
  max-width: 112px;
  gap: 0;
}

.line-selector.is-compact .line-selector__label {
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

.line-selector.is-compact .line-selector__control {
  min-height: 40px;
  grid-template-columns: minmax(0, 1fr) 14px;
  gap: 6px;
  padding: 5px 8px;
  border-radius: 6px;
}

.line-selector.is-compact.is-all .line-selector__control {
  color: var(--muted);
  background: var(--surface-subtle);
  border-color: var(--border);
}

.line-selector.is-filter {
  width: 36px;
  max-width: 36px;
  gap: 0;
}

.line-selector.is-filter .line-selector__label,
.line-selector.is-filter .line-selector__chevron {
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

.line-selector.is-filter .line-selector__control {
  width: 36px;
  min-height: 36px;
  grid-template-columns: 1fr;
  gap: 0;
  padding: 4px;
  background: var(--surface-subtle);
  border-color: transparent;
  border-radius: 6px;
}

.line-selector.is-filter.is-filtered .line-selector__control {
  background: var(--surface);
  border-color: var(--line-tone-border);
}

.line-selector__option-icon[style] {
  color: var(--line-tone-color);
  background: var(--line-tone-background);
  border-color: var(--line-tone-border);
}

.line-selector.is-filter .line-selector__options {
  right: 0;
  left: auto;
  width: min(300px, calc(100vw - 28px));
}

.line-selector.is-compact .line-selector__chevron {
  width: 14px;
  height: 14px;
}

.line-selector.is-compact .line-selector__options {
  right: 0;
  left: auto;
  width: min(300px, calc(100vw - 28px));
}

@media (max-width: 560px) {
  .line-selector.is-compact {
    width: 96px;
    max-width: 96px;
  }
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
.line-selector__option-copy small > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-selector__option-copy strong {
  font-size: 14px;
}

.line-selector__option-copy small {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 12px;
}

.line-selector__option-copy small > span {
  min-width: 0;
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

</style>
