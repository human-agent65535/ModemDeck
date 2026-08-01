<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ImagePlus, Trash2 } from '@lucide/vue'
import { createContactAvatar } from '../utils/contactAvatar'
import BaseAvatar from './BaseAvatar.vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    modelValue: string
    name: string
    disabled?: boolean
  }>(),
  { disabled: false }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  processing: [value: boolean]
}>()

const input = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const dragging = ref(false)
const error = ref('')

function openPicker(): void {
  if (!props.disabled && !busy.value) input.value?.click()
}

async function processFile(file?: File): Promise<void> {
  if (!file || props.disabled || busy.value) return
  busy.value = true
  emit('processing', true)
  dragging.value = false
  error.value = ''
  try {
    emit('update:modelValue', await createContactAvatar(file))
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('contacts.avatarProcessFailed')
  } finally {
    busy.value = false
    emit('processing', false)
  }
}

function selectFile(event: Event): void {
  const target = event.currentTarget as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  void processFile(file)
}

function dropFile(event: DragEvent): void {
  dragging.value = false
  void processFile(event.dataTransfer?.files[0])
}

function removeAvatar(): void {
  if (props.disabled || busy.value) return
  emit('update:modelValue', '')
  error.value = ''
}
</script>

<template>
  <div class="contact-avatar-picker">
    <button
      class="contact-avatar-picker__target"
      :class="{ 'is-dragging': dragging, 'has-image': modelValue }"
      type="button"
      :disabled="disabled || busy"
      :aria-label="
        modelValue ? t('contacts.replaceAvatar') : t('contacts.addAvatar')
      "
      @click="openPicker"
      @dragenter.prevent="dragging = true"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="dropFile"
    >
      <BaseAvatar :name="name || '#'" :src="modelValue" size="large" />
      <span class="contact-avatar-picker__overlay" aria-hidden="true">
        <ImagePlus :size="21" />
      </span>
      <span v-if="busy" class="contact-avatar-picker__busy">
        {{ t('contacts.processing') }}
      </span>
    </button>

    <div class="contact-avatar-picker__copy">
      <strong>{{ t('contacts.avatar') }}</strong>
      <span>
        {{
          modelValue
            ? t('contacts.avatarReplaceHint')
            : t('contacts.avatarEmptyHint')
        }}
      </span>
      <div class="contact-avatar-picker__actions">
        <button
          class="text-button"
          type="button"
          :disabled="disabled || busy"
          @click="openPicker"
        >
          <ImagePlus :size="15" />
          {{
            modelValue
              ? t('contacts.replaceAvatarAction')
              : t('contacts.uploadAvatar')
          }}
        </button>
        <button
          v-if="modelValue"
          class="text-button contact-avatar-picker__remove"
          type="button"
          :disabled="disabled || busy"
          @click="removeAvatar"
        >
          <Trash2 :size="15" />
          {{ t('contacts.deleteAvatar') }}
        </button>
      </div>
      <small v-if="error" class="field-error" role="alert">{{ error }}</small>
    </div>

    <input
      ref="input"
      class="contact-avatar-picker__input"
      type="file"
      accept="image/*"
      :disabled="disabled || busy"
      @change="selectFile"
    />
  </div>
</template>

<style scoped>
.contact-avatar-picker {
  display: flex;
  align-items: center;
  gap: 16px;
  min-width: 0;
  padding: 2px 0 8px;
}

.contact-avatar-picker__target {
  position: relative;
  display: grid;
  width: 82px;
  height: 82px;
  flex: 0 0 82px;
  place-items: center;
  padding: 0;
  overflow: hidden;
  color: var(--text);
  background: transparent;
  border: 2px dashed var(--border-strong);
  border-radius: 50%;
  cursor: pointer;
  transition:
    border-color 150ms ease,
    box-shadow 150ms ease,
    transform 150ms ease;
}

.contact-avatar-picker__target :deep(.avatar) {
  width: 74px;
  height: 74px;
  font-size: 20px;
}

.contact-avatar-picker__target:hover:not(:disabled),
.contact-avatar-picker__target:focus-visible,
.contact-avatar-picker__target.is-dragging {
  border-color: var(--accent);
  box-shadow: 0 0 0 4px rgb(17 120 100 / 13%);
}

.contact-avatar-picker__target.is-dragging {
  transform: scale(1.04);
}

.contact-avatar-picker__target:focus-visible {
  outline: none;
}

.contact-avatar-picker__target:disabled {
  cursor: wait;
  opacity: 0.72;
}

.contact-avatar-picker__overlay {
  position: absolute;
  inset: 3px;
  display: grid;
  place-items: center;
  color: var(--on-accent);
  background: rgb(15 23 42 / 46%);
  border-radius: 50%;
  opacity: 0;
  transition: opacity 150ms ease;
}

.contact-avatar-picker__target:hover .contact-avatar-picker__overlay,
.contact-avatar-picker__target:focus-visible .contact-avatar-picker__overlay,
.contact-avatar-picker__target.is-dragging .contact-avatar-picker__overlay {
  opacity: 1;
}

.contact-avatar-picker__target:not(.has-image) .contact-avatar-picker__overlay {
  top: auto;
  right: 2px;
  bottom: 2px;
  left: auto;
  width: 28px;
  height: 28px;
  background: var(--accent);
  box-shadow: 0 2px 7px rgb(15 23 42 / 22%);
  opacity: 1;
}

.contact-avatar-picker__busy {
  position: absolute;
  inset: 3px;
  display: grid;
  place-items: center;
  color: var(--on-accent);
  font-size: 12px;
  font-weight: 700;
  background: rgb(15 23 42 / 64%);
  border-radius: 50%;
}

.contact-avatar-picker__copy {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.contact-avatar-picker__copy > span {
  color: var(--muted);
  font-size: 12px;
}

.contact-avatar-picker__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-top: 2px;
}

.contact-avatar-picker__actions .text-button {
  min-height: 30px;
  padding: 0;
}

.contact-avatar-picker__remove {
  color: var(--danger);
}

.contact-avatar-picker__input {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

.contact-avatar-picker .field-error {
  margin-top: 2px;
}

@media (max-width: 560px) {
  .contact-avatar-picker {
    align-items: flex-start;
  }
}
</style>
