<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  AudioLines,
  LoaderCircle,
  Mic,
  Square,
  Volume2,
  X
} from '@lucide/vue'
import {
  audioState,
  inputDeviceMissing,
  outputDeviceMissing,
  refreshAudioDevices,
  setSelectedAudioInput,
  setSelectedAudioOutput,
  startMicrophoneTest,
  stopMicrophoneTest
} from '../state/audio'

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const dialog = ref<HTMLElement | null>(null)
const open = ref(false)
const dialogId = `audio-settings-${useId()}`
const missingInput = computed(() => inputDeviceMissing())
const missingOutput = computed(() => outputDeviceMissing())
const microphoneTestRunning = computed(
  () =>
    audioState.microphoneTestStatus === 'requesting' ||
    audioState.microphoneTestStatus === 'active'
)

function deviceLabel(device: MediaDeviceInfo, kind: 'microphone' | 'speaker', index: number): string {
  const kindLabel = t(`audio.${kind}`)
  return device.label || t('audio.deviceNumber', { kind: kindLabel, number: index + 1 })
}

async function openDialog(): Promise<void> {
  open.value = true
  void refreshAudioDevices()
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

function selectInput(event: Event): void {
  setSelectedAudioInput((event.target as HTMLSelectElement).value)
}

function selectOutput(event: Event): void {
  setSelectedAudioOutput((event.target as HTMLSelectElement).value)
}

function useDefaultOutput(): void {
  setSelectedAudioOutput('')
}

function toggleMicrophoneTest(): void {
  if (microphoneTestRunning.value) stopMicrophoneTest()
  else void startMicrophoneTest()
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) close()
}

function onDialogKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  close(true)
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
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

    <Transition name="fade">
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
          <label class="field">
            <span><Mic :size="14" />{{ t('audio.microphone') }}</span>
            <select
              :value="audioState.selectedInputID"
              :disabled="audioState.inputRoutingStatus === 'switching'"
              @change="selectInput"
            >
              <option value="">{{ t('common.systemDefault') }}</option>
              <option v-if="missingInput" :value="audioState.selectedInputID">
                {{ t('audio.selectedMicrophoneUnavailable') }}
              </option>
              <option
                v-for="(device, index) in audioState.inputs"
                :key="device.deviceId"
                :value="device.deviceId"
              >
                {{ deviceLabel(device, 'microphone', index) }}
              </option>
            </select>
            <small v-if="missingInput" class="field-error">
              {{ t('audio.microphoneUnavailable') }}
            </small>
            <small v-else-if="audioState.inputRoutingStatus === 'switching'">
              {{ t('audio.switchingMicrophone') }}
            </small>
            <small v-else-if="audioState.inputRoutingError" class="field-error">
              {{ audioState.inputRoutingError }}
            </small>
          </label>

          <label class="field">
            <span><Volume2 :size="14" />{{ t('audio.speaker') }}</span>
            <select
              :value="audioState.selectedOutputID"
              :disabled="!audioState.outputSelectionSupported"
              @change="selectOutput"
            >
              <option value="">{{ t('common.systemDefault') }}</option>
              <option v-if="missingOutput" :value="audioState.selectedOutputID">
                {{ t('audio.selectedSpeakerUnavailable') }}
              </option>
              <option
                v-for="(device, index) in audioState.outputs"
                :key="device.deviceId"
                :value="device.deviceId"
              >
                {{ deviceLabel(device, 'speaker', index) }}
              </option>
            </select>
            <template v-if="!audioState.outputSelectionSupported">
              <small class="field-error">{{ t('audio.defaultOutputOnly') }}</small>
              <button
                v-if="audioState.selectedOutputID"
                class="text-button audio-settings-default-output"
                type="button"
                @click="useDefaultOutput"
              >
                {{ t('audio.useSystemDefault') }}
              </button>
            </template>
            <small v-else-if="audioState.outputRoutingError" class="field-error">
              {{ audioState.outputRoutingError }}
            </small>
          </label>

          <section class="microphone-test" aria-labelledby="microphone-test-title">
            <div>
              <strong id="microphone-test-title">{{ t('audio.microphoneTest') }}</strong>
              <small v-if="audioState.microphoneTestStatus === 'active'">
                {{ t('common.durationSeconds', { count: audioState.microphoneTestSeconds }) }}
              </small>
            </div>
            <div
              class="microphone-level"
              role="meter"
              :aria-label="t('audio.microphoneLevel')"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="Math.round(audioState.microphoneTestLevel * 100)"
            >
              <span :style="{ width: `${audioState.microphoneTestLevel * 100}%` }" />
            </div>
            <button
              class="secondary-button microphone-test__button"
              type="button"
              :disabled="audioState.microphoneTestStatus === 'requesting'"
              @click="toggleMicrophoneTest"
            >
              <LoaderCircle
                v-if="audioState.microphoneTestStatus === 'requesting'"
                class="spin"
                :size="15"
              />
              <Square v-else-if="audioState.microphoneTestStatus === 'active'" :size="14" />
              <Mic v-else :size="15" />
              {{ microphoneTestRunning ? t('audio.stopTest') : t('audio.testMicrophone') }}
            </button>
            <p v-if="audioState.microphoneTestError" class="field-error">
              {{ audioState.microphoneTestError }}
            </p>
          </section>

          <p v-if="audioState.devicesStatus === 'error'" class="audio-settings-error" role="alert">
            <AlertCircle :size="15" />
            {{ audioState.devicesError }}
          </p>
        </div>
      </section>
    </Transition>
  </div>
</template>
