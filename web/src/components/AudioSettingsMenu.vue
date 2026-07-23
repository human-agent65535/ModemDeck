<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
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
  initializeAudioDevices,
  inputDeviceMissing,
  outputDeviceMissing,
  refreshAudioDevices,
  setSelectedAudioInput,
  setSelectedAudioOutput,
  shutdownAudioDevices,
  startMicrophoneTest,
  stopMicrophoneTest
} from '../state/audio'

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

function deviceLabel(device: MediaDeviceInfo, kind: '麦克风' | '扬声器', index: number): string {
  return device.label || `${kind} ${index + 1}`
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
  initializeAudioDevices()
  document.addEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  shutdownAudioDevices()
})
</script>

<template>
  <div ref="root" class="audio-settings">
    <button
      ref="trigger"
      class="icon-button"
      type="button"
      title="音频设备"
      aria-label="音频设备"
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
        aria-label="音频设备"
        @keydown="onDialogKeydown"
      >
        <header>
          <div>
            <h2>音频设备</h2>
            <small v-if="audioState.devicesStatus === 'loading'">
              <LoaderCircle class="spin" :size="13" />
              正在读取设备
            </small>
          </div>
          <button
            class="icon-button"
            type="button"
            title="关闭"
            aria-label="关闭音频设备"
            @click="close(true)"
          >
            <X :size="18" />
          </button>
        </header>

        <div class="audio-settings-menu__body">
          <label class="field">
            <span><Mic :size="14" />麦克风</span>
            <select
              :value="audioState.selectedInputID"
              :disabled="audioState.inputRoutingStatus === 'switching'"
              @change="selectInput"
            >
              <option value="">系统默认</option>
              <option v-if="missingInput" :value="audioState.selectedInputID">
                所选麦克风（当前不可用）
              </option>
              <option
                v-for="(device, index) in audioState.inputs"
                :key="device.deviceId"
                :value="device.deviceId"
              >
                {{ deviceLabel(device, '麦克风', index) }}
              </option>
            </select>
            <small v-if="missingInput" class="field-error">所选麦克风当前不可用</small>
            <small v-else-if="audioState.inputRoutingStatus === 'switching'">
              正在切换通话麦克风
            </small>
            <small v-else-if="audioState.inputRoutingError" class="field-error">
              {{ audioState.inputRoutingError }}
            </small>
          </label>

          <label class="field">
            <span><Volume2 :size="14" />扬声器</span>
            <select
              :value="audioState.selectedOutputID"
              :disabled="!audioState.outputSelectionSupported"
              @change="selectOutput"
            >
              <option value="">系统默认</option>
              <option v-if="missingOutput" :value="audioState.selectedOutputID">
                所选扬声器（当前不可用）
              </option>
              <option
                v-for="(device, index) in audioState.outputs"
                :key="device.deviceId"
                :value="device.deviceId"
              >
                {{ deviceLabel(device, '扬声器', index) }}
              </option>
            </select>
            <template v-if="!audioState.outputSelectionSupported">
              <small class="field-error">当前浏览器仅支持系统默认音频输出</small>
              <button
                v-if="audioState.selectedOutputID"
                class="text-button audio-settings-default-output"
                type="button"
                @click="useDefaultOutput"
              >
                改用系统默认
              </button>
            </template>
            <small v-else-if="audioState.outputRoutingError" class="field-error">
              {{ audioState.outputRoutingError }}
            </small>
          </label>

          <section class="microphone-test" aria-labelledby="microphone-test-title">
            <div>
              <strong id="microphone-test-title">麦克风测试</strong>
              <small v-if="audioState.microphoneTestStatus === 'active'">
                {{ audioState.microphoneTestSeconds }} 秒
              </small>
            </div>
            <div
              class="microphone-level"
              role="meter"
              aria-label="麦克风电平"
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
              {{ microphoneTestRunning ? '停止测试' : '测试麦克风' }}
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
