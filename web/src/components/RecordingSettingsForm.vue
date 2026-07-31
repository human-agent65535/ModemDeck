<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Circle, LoaderCircle } from '@lucide/vue'
import {
  loadRecordingSettings,
  recordingSettingsState,
  updateDefaultRecording
} from '../state/recording'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const pendingEnabled = ref(false)
const saved = ref(false)
let savedTimer: ReturnType<typeof globalThis.setTimeout> | undefined
const displayedEnabled = computed(() =>
  recordingSettingsState.saving
    ? pendingEnabled.value
    : Boolean(recordingSettingsState.data?.default_enabled)
)

async function changeDefault(event: Event): Promise<void> {
  pendingEnabled.value = (event.target as HTMLInputElement).checked
  saved.value = false
  const updated = await updateDefaultRecording(pendingEnabled.value)
  if (!updated) return
  saved.value = true
  if (savedTimer) globalThis.clearTimeout(savedTimer)
  savedTimer = globalThis.setTimeout(() => {
    saved.value = false
    savedTimer = undefined
  }, 2200)
}

onMounted(() => {
  void loadRecordingSettings()
})

onBeforeUnmount(() => {
  if (savedTimer) globalThis.clearTimeout(savedTimer)
})
</script>

<template>
  <SettingsPreferenceRow
    class="recording-settings"
    :title="t('recordingSettings.title')"
    title-id="recording-settings-title"
    :description="t('recordingSettings.description')"
    icon-tone="danger"
  >
    <template #icon>
      <Circle :size="19" fill="currentColor" />
    </template>
    <template #control>
      <div
        v-if="
          recordingSettingsState.status === 'loading' ||
          recordingSettingsState.status === 'idle'
        "
        class="recording-settings__state"
        role="status"
      >
        <LoaderCircle class="spin" :size="18" />
        {{ t('recordingSettings.loading') }}
      </div>

      <div
        v-else-if="
          recordingSettingsState.status === 'error' ||
          recordingSettingsState.status === 'forbidden'
        "
        class="recording-settings__state recording-settings__state--error"
        role="alert"
      >
        <span>{{ recordingSettingsState.error }}</span>
        <button
          v-if="recordingSettingsState.status === 'error'"
          type="button"
          @click="loadRecordingSettings(true)"
        >
          {{ t('common.retry') }}
        </button>
      </div>

      <label v-else class="recording-settings__control">
        <span class="recording-settings__value">
          {{ displayedEnabled ? t('recordingSettings.enabled') : t('recordingSettings.disabled') }}
        </span>
        <span
          v-if="saved"
          class="recording-settings__saved"
          role="status"
          :title="t('common.saved')"
        >
          <Check :size="15" aria-hidden="true" />
          <span class="sr-only">{{ t('common.saved') }}</span>
        </span>
        <LoaderCircle
          v-if="recordingSettingsState.saving"
          class="spin"
          :size="17"
          aria-hidden="true"
        />
        <input
          type="checkbox"
          role="switch"
          :checked="displayedEnabled"
          :disabled="recordingSettingsState.saving"
          :aria-label="t('recordingSettings.defaultForNewCalls')"
          @change="changeDefault"
        />
      </label>
    </template>
    <template
      v-if="recordingSettingsState.error && recordingSettingsState.status === 'ready'"
      #feedback
    >
      <p class="recording-settings__error" role="alert">
        {{ recordingSettingsState.error }}
      </p>
    </template>
  </SettingsPreferenceRow>
</template>

<style scoped>
.recording-settings__state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.recording-settings__state--error,
.recording-settings__error {
  color: var(--danger);
}

.recording-settings__state button {
  color: inherit;
  font-weight: 650;
  background: transparent;
}

.recording-settings__control {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 9px;
}

.recording-settings__value {
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
}

.recording-settings__saved {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  color: #fff;
  background: var(--success);
  border-radius: 50%;
}

.recording-settings__control input {
  position: relative;
  width: 42px;
  height: 24px;
  appearance: none;
  background: #d8dde2;
  border-radius: 12px;
  cursor: pointer;
}

.recording-settings__control input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #ffffff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.recording-settings__control input:checked {
  background: var(--danger);
}

.recording-settings__control input:checked::before {
  transform: translateX(18px);
}

.recording-settings__control input:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.recording-settings__error {
  margin: 0;
  font-size: 11px;
}

@media (max-width: 480px) {
  .recording-settings__value {
    display: none;
  }
}
</style>
