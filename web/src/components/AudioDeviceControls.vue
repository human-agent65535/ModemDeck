<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { AlertCircle, LoaderCircle, Mic, Square, Volume2 } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
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

const props = withDefaults(
  defineProps<{
    compact?: boolean
    refreshOnMount?: boolean
  }>(),
  {
    compact: false,
    refreshOnMount: true
  }
)

const { t } = useI18n()
const missingInput = computed(() => inputDeviceMissing())
const missingOutput = computed(() => outputDeviceMissing())
const microphoneTestRunning = computed(
  () =>
    audioState.microphoneTestStatus === 'requesting' ||
    audioState.microphoneTestStatus === 'active'
)

function deviceLabel(
  device: MediaDeviceInfo,
  kind: 'microphone' | 'speaker',
  index: number
): string {
  const kindLabel = t(`audio.${kind}`)
  return device.label || t('audio.deviceNumber', { kind: kindLabel, number: index + 1 })
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

onMounted(() => {
  if (props.refreshOnMount) void refreshAudioDevices()
})

onBeforeUnmount(() => {
  stopMicrophoneTest()
})
</script>

<template>
  <div class="audio-device-controls" :class="{ 'is-compact': compact }">
    <div class="audio-device-controls__fields">
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
            class="text-button audio-device-controls__default"
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
    </div>

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

    <p v-if="audioState.devicesStatus === 'error'" class="audio-device-controls__error" role="alert">
      <AlertCircle :size="15" />
      {{ audioState.devicesError }}
    </p>
  </div>
</template>

<style scoped>
.audio-device-controls {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.audio-device-controls__fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.audio-device-controls.is-compact {
  gap: 16px;
}

.audio-device-controls.is-compact .audio-device-controls__fields {
  grid-template-columns: minmax(0, 1fr);
}

.field > span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.field select {
  min-height: 40px;
  font-size: 13px;
}

.field small {
  font-size: 10px;
}

.audio-device-controls__default {
  align-self: flex-start;
}

.microphone-test {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}

.microphone-test > div:first-child {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.microphone-test strong {
  font-size: 12px;
}

.microphone-test small {
  color: var(--muted);
  font-variant-numeric: tabular-nums;
  font-size: 10px;
}

.microphone-level {
  height: 8px;
  overflow: hidden;
  background: var(--surface-hover);
  border-radius: 4px;
}

.microphone-level > span {
  display: block;
  width: 0;
  height: 100%;
  background: var(--accent);
  border-radius: inherit;
  transition: width 80ms linear;
}

.microphone-test__button {
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  align-self: flex-start;
  gap: 6px;
  padding: 0 11px;
}

.microphone-test .field-error {
  margin: 0;
}

.audio-device-controls__error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 0;
  color: var(--danger);
  font-size: 10px;
  line-height: 1.45;
}

@media (max-width: 680px) {
  .audio-device-controls__fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
