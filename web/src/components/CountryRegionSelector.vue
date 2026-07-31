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
import { Check, ChevronDown, Globe2, Search } from '@lucide/vue'
import {
  filterPhoneRegionOptions,
  phoneRegionOptions,
  type PhoneRegionOption
} from '../utils/phoneRegions'
import PopoverTransition from './PopoverTransition.vue'

const { locale, t } = useI18n()
const props = defineProps<{
  modelValue?: string
  regions: string[]
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const searchInput = ref<HTMLInputElement>()
const open = ref(false)
const activeIndex = ref(0)
const query = ref('')
const listboxID = `${useId()}-country-listbox`
const selectedRegion = computed(() => props.modelValue?.trim().toUpperCase() || '')
const preferredRegions = computed(() => [selectedRegion.value, ...props.regions])
const countryOptions = computed(() =>
  phoneRegionOptions(locale.value, preferredRegions.value)
)
const filteredCountries = computed(() =>
  filterPhoneRegionOptions(countryOptions.value, query.value)
)
const showInternational = computed(() => {
  const normalized = query.value.trim().toLocaleLowerCase()
  if (!normalized) return true
  return (
    t('contacts.internationalNumber').toLocaleLowerCase().includes(normalized) ||
    normalized === '+'
  )
})
const options = computed<Array<PhoneRegionOption | undefined>>(() => [
  ...(showInternational.value ? [undefined] : []),
  ...filteredCountries.value
])
const selectedOption = computed(() =>
  countryOptions.value.find(option => option.region === selectedRegion.value)
)
const triggerText = computed(() =>
  selectedOption.value ? `+${selectedOption.value.callingCode}` : '+'
)

function optionRegion(option: PhoneRegionOption | undefined): string {
  return option?.region || ''
}

function selectedIndex(): number {
  const index = options.value.findIndex(
    option => optionRegion(option) === selectedRegion.value
  )
  return index >= 0 ? index : 0
}

function focusOption(): void {
  void nextTick(() => {
    root.value
      ?.querySelector<HTMLElement>(`[data-country-index="${activeIndex.value}"]`)
      ?.focus()
  })
}

function openMenu(): void {
  open.value = true
  query.value = ''
  activeIndex.value = selectedIndex()
  void nextTick(() => searchInput.value?.focus())
}

function closeMenu(returnFocus = false): void {
  open.value = false
  if (returnFocus) void nextTick(() => trigger.value?.focus())
}

function select(region: string): void {
  emit('update:modelValue', region)
  closeMenu(true)
}

function move(offset: number): void {
  activeIndex.value =
    (activeIndex.value + offset + options.value.length) % options.value.length
  focusOption()
}

function onTriggerKeydown(event: KeyboardEvent): void {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!open.value) openMenu()
    else move(event.key === 'ArrowDown' ? 1 : -1)
  }
}

function onOptionKeydown(event: KeyboardEvent, index: number): void {
  activeIndex.value = index
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    move(event.key === 'ArrowDown' ? 1 : -1)
  } else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    const option = options.value[index]
    select(optionRegion(option))
  } else if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu(true)
  } else if (event.key === 'Tab') {
    closeMenu()
  }
}

function onSearchKeydown(event: KeyboardEvent): void {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (options.value.length === 0) return
    activeIndex.value = event.key === 'ArrowDown' ? 0 : options.value.length - 1
    focusOption()
  } else if (event.key === 'Escape') {
    event.preventDefault()
    closeMenu(true)
  }
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (open.value && !root.value?.contains(event.target as Node)) closeMenu()
}

watch(options, current => {
  if (activeIndex.value >= current.length) activeIndex.value = Math.max(0, current.length - 1)
})

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))
</script>

<template>
  <div ref="root" class="country-region-selector">
    <button
      ref="trigger"
      class="country-region-selector__trigger"
      type="button"
      :title="t('contacts.numberCountry')"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listboxID"
      @click="open ? closeMenu(true) : openMenu()"
      @keydown="onTriggerKeydown"
    >
      <Globe2 :size="16" />
      <span>{{ triggerText }}</span>
      <ChevronDown :size="14" />
    </button>
    <PopoverTransition>
      <div
        v-if="open"
        class="country-region-selector__menu"
      >
        <label class="country-region-selector__search">
          <Search :size="16" />
          <input
            ref="searchInput"
            v-model="query"
            type="search"
            autocomplete="off"
            :placeholder="t('contacts.searchCountries')"
            @keydown="onSearchKeydown"
          />
        </label>
        <div
          :id="listboxID"
          class="country-region-selector__options"
          role="listbox"
          :aria-label="t('contacts.numberCountry')"
        >
          <button
            v-for="(option, index) in options"
            :key="option?.region || 'international'"
            class="country-region-selector__option"
            type="button"
            role="option"
            :aria-selected="selectedRegion === optionRegion(option)"
            :data-country-index="index"
            @click="select(optionRegion(option))"
            @keydown="onOptionKeydown($event, index)"
          >
            <span v-if="option" class="country-region-selector__identity">
              <strong>{{ option.name }}</strong>
              <small>{{ option.region }} · +{{ option.callingCode }}</small>
            </span>
            <span v-else class="country-region-selector__identity">
              <strong>{{ t('contacts.internationalNumber') }}</strong>
            </span>
            <Check v-if="selectedRegion === optionRegion(option)" :size="16" />
          </button>
          <p v-if="options.length === 0" class="country-region-selector__empty">
            {{ t('contacts.noCountries') }}
          </p>
        </div>
      </div>
    </PopoverTransition>
  </div>
</template>

<style scoped>
.country-region-selector {
  position: relative;
  min-width: 96px;
}

.country-region-selector__trigger {
  width: 100%;
  min-height: 42px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 10px;
  color: var(--muted);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
}

.country-region-selector__trigger span {
  min-width: 22px;
  color: var(--ink);
  font-size: 14px;
  font-weight: 700;
  text-align: center;
}

.country-region-selector__menu {
  position: absolute;
  z-index: var(--layer-popover);
  top: calc(100% + 6px);
  left: 0;
  width: max-content;
  min-width: 290px;
  max-width: min(340px, 84vw);
  padding: 5px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: var(--shadow-lg);
}

.country-region-selector__search {
  min-height: 42px;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
  padding: 0 10px;
  color: var(--muted);
  background: var(--surface-subtle);
  border-radius: 4px;
}

.country-region-selector__search input {
  width: 100%;
  min-width: 0;
  padding: 0;
  background: transparent;
  border: 0;
  outline: 0;
}

.country-region-selector__options {
  max-height: min(360px, 52vh);
  overflow-y: auto;
}

.country-region-selector__option {
  width: 100%;
  min-height: 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  color: var(--ink);
  background: transparent;
  border: 0;
  border-radius: 4px;
  cursor: pointer;
  text-align: left;
}

.country-region-selector__identity {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.country-region-selector__identity strong,
.country-region-selector__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.country-region-selector__identity strong {
  font-size: 14px;
}

.country-region-selector__identity small {
  color: var(--muted);
  font-size: 12px;
}

.country-region-selector__empty {
  margin: 0;
  padding: 12px 10px;
  color: var(--muted);
  font-size: 13px;
}

.country-region-selector__option:hover,
.country-region-selector__option:focus-visible,
.country-region-selector__option[aria-selected="true"] {
  background: var(--surface-subtle);
}
</style>
