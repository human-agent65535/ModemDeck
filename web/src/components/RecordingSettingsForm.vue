<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Circle, LoaderCircle } from '@lucide/vue'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import {
  loadRecordingSettings,
  recordingSettingsState,
  updateDefaultRecording
} from '../state/recording'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const pendingEnabled = ref(false)
const saveMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error
      ? cause.message
      : recordingSettingsState.error || t('runtime.recordingSettingsSaveFailed')
})
const displayedEnabled = computed(() =>
  recordingSettingsState.saving
    ? pendingEnabled.value
    : Boolean(recordingSettingsState.data?.default_enabled)
)

async function changeDefault(event: Event): Promise<void> {
  pendingEnabled.value = (event.target as HTMLInputElement).checked
  const result = await saveMutation.run(async () => {
    const updated = await updateDefaultRecording(pendingEnabled.value)
    if (!updated) {
      throw new Error(
        recordingSettingsState.error || t('runtime.recordingSettingsSaveFailed')
      )
    }
    return updated
  })
  if (!result.ok) {
    pendingEnabled.value = Boolean(recordingSettingsState.data?.default_enabled)
  }
}

onMounted(() => {
  void loadRecordingSettings()
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
        <input
          class="ui-switch ui-switch--danger"
          type="checkbox"
          role="switch"
          :checked="displayedEnabled"
          :disabled="recordingSettingsState.saving"
          :aria-label="t('recordingSettings.defaultForNewCalls')"
          @change="changeDefault"
        />
      </label>
    </template>
    <template v-if="saveMutation.error.value" #feedback>
      <p class="recording-settings__error" role="alert">
        {{ saveMutation.error.value }}
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

.recording-settings__error {
  margin: 0;
  font-size: 11px;
}

@media (max-width: 560px) {
  .recording-settings__value {
    display: none;
  }
}
</style>
