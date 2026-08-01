<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { Upload } from '@lucide/vue'

const props = withDefaults(
  defineProps<{
    file: File | null
    label: string
    prompt: string
    description: string
    accept?: string
    disabled?: boolean
    error?: string
  }>(),
  {
    accept: '',
    disabled: false,
    error: ''
  }
)

const emit = defineEmits<{
  select: [file: File]
}>()

const input = ref<HTMLInputElement>()
const dragDepth = ref(0)
const dragging = ref(false)
const controlID = `${useId()}-file`
const descriptionID = `${controlID}-description`
const errorID = `${controlID}-error`
const describedBy = computed(() =>
  [descriptionID, props.error ? errorID : ''].filter(Boolean).join(' ')
)

function openPicker(): void {
  if (!props.disabled) input.value?.click()
}

function selectFile(file?: File): void {
  if (!props.disabled && file) emit('select', file)
}

function handleInput(event: Event): void {
  const target = event.currentTarget as HTMLInputElement
  selectFile(target.files?.[0])
  target.value = ''
}

function handleDragEnter(event: DragEvent): void {
  event.preventDefault()
  if (props.disabled || !event.dataTransfer?.types.includes('Files')) return
  dragDepth.value += 1
  dragging.value = true
}

function handleDragOver(event: DragEvent): void {
  event.preventDefault()
  if (props.disabled || !event.dataTransfer) return
  event.dataTransfer.dropEffect = 'copy'
}

function handleDragLeave(event: DragEvent): void {
  event.preventDefault()
  if (props.disabled || !dragging.value) return
  dragDepth.value = Math.max(0, dragDepth.value - 1)
  if (dragDepth.value === 0) dragging.value = false
}

function handleDrop(event: DragEvent): void {
  event.preventDefault()
  dragDepth.value = 0
  dragging.value = false
  selectFile(event.dataTransfer?.files[0])
}
</script>

<template>
  <div
    class="file-drop-control"
    :class="{
      'has-file': file,
      'is-disabled': disabled,
      'is-dragging': dragging,
      'is-error': error
    }"
  >
    <span class="file-drop-control__label">{{ label }}</span>
    <button
      class="file-drop-control__surface"
      type="button"
      :disabled="disabled"
      :aria-label="`${label}: ${file?.name || prompt}`"
      :aria-describedby="describedBy"
      @click="openPicker"
      @dragenter="handleDragEnter"
      @dragover="handleDragOver"
      @dragleave="handleDragLeave"
      @drop="handleDrop"
    >
      <span class="file-drop-control__icon" aria-hidden="true">
        <slot name="icon"><Upload :size="19" /></slot>
      </span>
      <span class="file-drop-control__copy">
        <strong>{{ file?.name || prompt }}</strong>
        <small :id="descriptionID">{{ description }}</small>
      </span>
      <Upload class="file-drop-control__action" :size="17" aria-hidden="true" />
    </button>
    <input
      :id="controlID"
      ref="input"
      class="file-drop-control__input"
      type="file"
      :accept="accept"
      :disabled="disabled"
      @change="handleInput"
    />
    <p v-if="error" :id="errorID" class="file-drop-control__error" role="alert">
      {{ error }}
    </p>
  </div>
</template>

<style scoped>
.file-drop-control {
  display: grid;
  min-width: 0;
  gap: 7px;
}

.file-drop-control__label {
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
}

.file-drop-control__input {
  display: none;
}

.file-drop-control__surface {
  display: grid;
  width: 100%;
  min-height: 84px;
  align-items: center;
  gap: 11px;
  padding: 12px;
  grid-template-columns: auto minmax(0, 1fr) auto;
  color: var(--accent-strong);
  text-align: left;
  background: var(--surface-subtle);
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  cursor: pointer;
  transition:
    border-color var(--motion-fast) var(--ease-standard),
    box-shadow var(--motion-fast) var(--ease-standard),
    background var(--motion-fast) var(--ease-standard);
}

.file-drop-control__surface:hover,
.file-drop-control.is-dragging .file-drop-control__surface {
  background: var(--surface-hover);
  border-color: var(--accent);
}

.file-drop-control__surface:focus-visible {
  border-color: var(--accent);
  outline: 0;
  box-shadow: 0 0 0 3px rgb(17 120 100 / 20%);
}

.file-drop-control.has-file .file-drop-control__surface {
  border-style: solid;
}

.file-drop-control.is-error .file-drop-control__surface {
  border-color: var(--danger);
}

.file-drop-control__icon {
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.file-drop-control__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.file-drop-control__copy strong,
.file-drop-control__copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-drop-control__copy strong {
  color: var(--text);
  font-size: 12px;
}

.file-drop-control__copy small {
  color: var(--muted);
  font-size: 10px;
}

.file-drop-control__action {
  flex: 0 0 auto;
}

.file-drop-control__error {
  margin: 0;
  color: var(--danger);
  font-size: 11px;
}

.file-drop-control.is-disabled {
  opacity: 0.46;
}

.file-drop-control.is-disabled .file-drop-control__surface {
  cursor: not-allowed;
}

@media (prefers-reduced-motion: reduce) {
  .file-drop-control__surface {
    transition: none;
  }
}
</style>
