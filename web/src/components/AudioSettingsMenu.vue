<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { AudioLines, LoaderCircle, X } from '@lucide/vue'
import { audioState, stopMicrophoneTest } from '../state/audio'
import AudioDeviceControls from './AudioDeviceControls.vue'
import PopoverTransition from './PopoverTransition.vue'

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const dialog = ref<HTMLElement | null>(null)
const open = ref(false)
const dialogId = `audio-settings-${useId()}`

async function openDialog(): Promise<void> {
  open.value = true
  await nextTick()
  dialog.value
    ?.querySelector<HTMLElement>(
      'button:not(:disabled), select:not(:disabled), input:not(:disabled), [tabindex]:not([tabindex="-1"])'
    )
    ?.focus()
}

function toggle(): void {
  if (open.value) close()
  else void openDialog()
}

function close(restoreFocus = false): void {
  open.value = false
  stopMicrophoneTest()
  if (restoreFocus) void nextTick(() => trigger.value?.focus())
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) close()
}

function onDocumentFocusIn(event: FocusEvent): void {
  if (open.value && root.value && !root.value.contains(event.target as Node)) close()
}

function onDialogKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  close(true)
}

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
  <div ref="root" class="audio-settings">
    <button
      ref="trigger"
      class="icon-button"
      type="button"
      :title="t('audio.devices')"
      :aria-label="t('audio.devices')"
      aria-haspopup="dialog"
      :aria-controls="dialogId"
      :aria-expanded="open"
      @click="toggle"
    >
      <AudioLines :size="19" />
    </button>

    <PopoverTransition>
      <section
        v-if="open"
        :id="dialogId"
        ref="dialog"
        class="audio-settings-menu"
        role="dialog"
        :aria-label="t('audio.devices')"
        @keydown="onDialogKeydown"
      >
        <header>
          <div>
            <h2>{{ t('audio.devices') }}</h2>
            <small v-if="audioState.devicesStatus === 'loading'">
              <LoaderCircle class="spin" :size="13" />
              {{ t('audio.loadingDevices') }}
            </small>
          </div>
          <button
            class="icon-button"
            type="button"
            :title="t('common.close')"
            :aria-label="t('audio.closeDevices')"
            @click="close(true)"
          >
            <X :size="18" />
          </button>
        </header>

        <div class="audio-settings-menu__body">
          <AudioDeviceControls compact />
        </div>
      </section>
    </PopoverTransition>
  </div>
</template>
