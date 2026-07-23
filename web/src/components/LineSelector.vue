<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown, RadioTower, Star } from '@lucide/vue'
import type { CommunicationCapabilityName, LineSummary } from '../api/types'
import { lineKey, lineLabel, lineSupports } from '../state/workspace'

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
    allDescription: '显示所有模组',
    disabled: false
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const selectedLine = computed(() =>
  props.lines.find(line => lineKey(line) === props.modelValue)
)
const allSelected = computed(
  () => props.includeAll && props.modelValue === props.allValue
)
const selectedIsDefault = computed(
  () =>
    Boolean(selectedLine.value?.device_imei) &&
    selectedLine.value?.device_imei === props.defaultDeviceImei
)
const displayName = computed(() => {
  if (selectedLine.value) return lineLabel(selectedLine.value)
  if (allSelected.value) return props.allLabel
  return props.placeholder
})
const displayDetails = computed(() => {
  const line = selectedLine.value
  if (!line) return allSelected.value ? props.allDescription : '请选择要使用的模组'
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
  return details.join(' · ') || line.model || '模组线路'
})

function optionLabel(line: LineSummary): string {
  const name = lineLabel(line)
  const details = [
    line.phone_number,
    line.operator && line.operator !== name ? line.operator : '',
    line.device_imei === props.defaultDeviceImei ? '默认线路' : '',
    props.capability && lineSupports(line, props.capability) === false
      ? props.unavailableLabel
      : ''
  ].filter(Boolean)
  return details.length > 0 ? `${name} · ${details.join(' · ')}` : name
}

function changeSelection(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  emit('update:modelValue', value)
  emit('change', value)
}
</script>

<template>
  <label class="line-selector" :class="{ 'is-disabled': disabled }">
    <span class="line-selector__label">{{ label }}</span>
    <span class="line-selector__control">
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
      <ChevronDown class="line-selector__chevron" :size="18" aria-hidden="true" />
      <select
        :value="modelValue"
        :disabled="disabled"
        :aria-label="label"
        @change="changeSelection"
      >
        <option v-if="includeAll" :value="allValue">{{ allLabel }}</option>
        <option v-else value="" disabled>{{ placeholder }}</option>
        <option v-for="line in lines" :key="lineKey(line)" :value="lineKey(line)">
          {{ optionLabel(line) }}
        </option>
      </select>
    </span>
  </label>
</template>

<style scoped>
.line-selector {
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
  min-width: 0;
  min-height: 64px;
  align-items: center;
  grid-template-columns: 38px minmax(0, 1fr) auto auto;
  gap: 10px;
  padding: 8px 12px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  transition:
    border-color 140ms ease,
    box-shadow 140ms ease,
    background 140ms ease;
}

.line-selector__control:hover {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.line-selector__control:focus-within {
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
}

.line-selector select {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  cursor: pointer;
  opacity: 0;
}

.line-selector.is-disabled {
  opacity: 0.6;
}

.line-selector.is-disabled select {
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
