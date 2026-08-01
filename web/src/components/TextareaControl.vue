<script setup lang="ts">
import { computed, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    label: string
    description?: string
    error?: string
    placeholder?: string
    rows?: number
    maxLength?: number
    disabled?: boolean
    monospace?: boolean
    sensitive?: boolean
    spellcheck?: boolean
    wrap?: 'soft' | 'hard' | 'off'
  }>(),
  {
    description: '',
    error: '',
    placeholder: '',
    rows: 4,
    maxLength: undefined,
    disabled: false,
    monospace: false,
    sensitive: false,
    spellcheck: true,
    wrap: 'soft'
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const controlID = `${useId()}-textarea`
const descriptionID = `${controlID}-description`
const errorID = `${controlID}-error`
const describedBy = computed(() =>
  [props.description ? descriptionID : '', props.error ? errorID : '']
    .filter(Boolean)
    .join(' ')
)

function updateValue(event: Event): void {
  emit('update:modelValue', (event.currentTarget as HTMLTextAreaElement).value)
}
</script>

<template>
  <div
    class="textarea-control"
    :class="{
      'is-disabled': disabled,
      'is-error': error,
      'is-monospace': monospace
    }"
  >
    <label class="textarea-control__label" :for="controlID">{{ label }}</label>
    <div class="textarea-control__surface">
      <textarea
        :id="controlID"
        class="textarea-control__input"
        :value="modelValue"
        :rows="rows"
        :wrap="wrap"
        :placeholder="placeholder"
        :maxlength="maxLength"
        :disabled="disabled"
        :spellcheck="spellcheck"
        autocomplete="off"
        autocapitalize="off"
        :data-1p-ignore="sensitive ? 'true' : undefined"
        :data-lpignore="sensitive ? 'true' : undefined"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy || undefined"
        @input="updateValue"
      ></textarea>
    </div>
    <small
      v-if="description"
      :id="descriptionID"
      class="textarea-control__description"
    >
      {{ description }}
    </small>
    <p v-if="error" :id="errorID" class="textarea-control__error" role="alert">
      {{ error }}
    </p>
  </div>
</template>

<style scoped>
.textarea-control {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.textarea-control__label {
  color: var(--text);
  font-size: 12px;
  font-weight: 700;
}

.textarea-control__surface {
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  transition:
    border-color var(--motion-fast) var(--ease-standard),
    box-shadow var(--motion-fast) var(--ease-standard),
    background var(--motion-fast) var(--ease-standard);
}

.textarea-control__surface:hover {
  background: var(--surface-hover);
}

.textarea-control:focus-within .textarea-control__surface {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px rgb(17 120 100 / 20%);
}

.textarea-control.is-error .textarea-control__surface {
  border-color: var(--danger);
}

.textarea-control__input {
  display: block;
  width: 100%;
  min-width: 0;
  padding: 11px 12px;
  color: var(--text);
  font: inherit;
  font-size: 12px;
  line-height: 1.5;
  appearance: none;
  resize: none;
  background: transparent;
  border: 0;
  border-radius: 0;
  outline: 0;
}

.textarea-control__input:focus,
.textarea-control__input:focus-visible {
  outline: 0;
  box-shadow: none;
}

.textarea-control__input::placeholder {
  color: var(--muted);
}

.textarea-control.is-monospace .textarea-control__input {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.textarea-control__description {
  color: var(--muted);
  font-size: 11px;
}

.textarea-control__error {
  margin: 0;
  color: var(--danger);
  font-size: 11px;
}

.textarea-control.is-disabled {
  opacity: 0.5;
}

.textarea-control.is-disabled .textarea-control__surface,
.textarea-control.is-disabled .textarea-control__surface:hover {
  background: var(--surface-subtle);
}

@media (prefers-reduced-motion: reduce) {
  .textarea-control__surface {
    transition: none;
  }
}
</style>
