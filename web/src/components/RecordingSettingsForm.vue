<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Circle, LoaderCircle } from '@lucide/vue'
import {
  loadRecordingSettings,
  recordingSettingsState,
  updateDefaultRecording
} from '../state/recording'

const { t } = useI18n()
const pendingEnabled = ref(false)
const displayedEnabled = computed(() =>
  recordingSettingsState.saving
    ? pendingEnabled.value
    : Boolean(recordingSettingsState.data?.default_enabled)
)

async function changeDefault(event: Event): Promise<void> {
  pendingEnabled.value = (event.target as HTMLInputElement).checked
  await updateDefaultRecording(pendingEnabled.value)
}

onMounted(() => {
  void loadRecordingSettings()
})
</script>

<template>
  <section class="recording-settings" aria-labelledby="recording-settings-title">
    <header>
      <span class="recording-settings__icon"><Circle :size="19" fill="currentColor" /></span>
      <div>
        <h3 id="recording-settings-title">{{ t('recordingSettings.title') }}</h3>
        <p>{{ t('recordingSettings.description') }}</p>
      </div>
    </header>

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

    <label v-else class="recording-settings__toggle">
      <span>
        <strong>{{ t('recordingSettings.defaultRecording') }}</strong>
        <small>
          {{ displayedEnabled ? t('recordingSettings.enabled') : t('recordingSettings.disabled') }}
        </small>
      </span>
      <span class="recording-settings__control">
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
      </span>
    </label>

    <p
      v-if="recordingSettingsState.error && recordingSettingsState.status === 'ready'"
      class="recording-settings__error"
      role="alert"
    >
      {{ recordingSettingsState.error }}
    </p>
  </section>
</template>

<style scoped>
.recording-settings {
  max-width: 680px;
}

.recording-settings > header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.recording-settings__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--danger);
  background: var(--danger-soft);
  border-radius: 50%;
}

.recording-settings h3 {
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.recording-settings header p,
.recording-settings__toggle small {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.recording-settings__state {
  display: flex;
  min-height: 72px;
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

.recording-settings__toggle {
  display: flex;
  min-height: 74px;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  border-bottom: 1px solid var(--border);
}

.recording-settings__toggle > span:first-child {
  display: flex;
  flex-direction: column;
}

.recording-settings__toggle strong {
  font-size: 13px;
}

.recording-settings__control {
  display: flex;
  align-items: center;
  gap: 9px;
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
  margin-top: 10px;
  font-size: 11px;
}
</style>
