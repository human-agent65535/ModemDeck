<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ChevronDown, LoaderCircle, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import {
  bootstrapResource,
  lineKey,
  lineLabel,
  loadBootstrap,
  updateDefaultLine
} from '../state/workspace'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const selected = ref('')
const saveMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error ? cause.message : t('device.defaultLineSaveFailed')
})
const saving = saveMutation.saving

const lines = computed(() => bootstrapResource.data?.lines || [])
const loading = computed(
  () =>
    bootstrapResource.status === 'idle' ||
    bootstrapResource.status === 'loading'
)

watch(
  () => bootstrapResource.data?.line_settings.default_line_id || '',
  value => {
    if (!saving.value) selected.value = value
  },
  { immediate: true }
)

async function changeDefault(event: Event): Promise<void> {
  const lineID = (event.target as HTMLSelectElement).value
  if (!lineID || lineID === selected.value || saving.value) return
  const previous = selected.value
  selected.value = lineID
  const result = await saveMutation.run(() => updateDefaultLine(lineID))
  if (!result.ok) selected.value = previous
}

onMounted(() => {
  void loadBootstrap()
})

</script>

<template>
  <SettingsPreferenceRow
    class="default-line-settings"
    :title="t('users.defaultLine')"
    title-id="default-line-settings-title"
    :description="t('account.defaultLineDescription')"
    control-size="wide"
  >
    <template #icon>
      <RadioTower :size="20" />
    </template>
    <template #control>
      <div v-if="loading" class="default-line-settings__state" role="status">
        <LoaderCircle class="spin" :size="18" />
        {{ t('common.loading') }}
      </div>
      <span v-else-if="lines.length === 0" class="default-line-settings__state">
        {{ t('users.noLines') }}
      </span>
      <label v-else class="default-line-select">
        <span class="sr-only">{{ t('users.defaultLine') }}</span>
        <select :value="selected" :disabled="saving" @change="changeDefault">
          <option
            v-for="line in lines"
            :key="lineKey(line)"
            :value="lineKey(line)"
          >
            {{ lineLabel(line) }}
          </option>
        </select>
        <ChevronDown :size="18" aria-hidden="true" />
      </label>
    </template>
    <template v-if="saveMutation.error.value" #feedback>
      <p class="default-line-settings__feedback is-error" role="alert">
        {{ saveMutation.error.value }}
      </p>
    </template>
  </SettingsPreferenceRow>
</template>

<style scoped>
.default-line-settings__state {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.default-line-select {
  position: relative;
  display: flex;
  width: 100%;
  height: 42px;
  flex: 0 0 auto;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
}

.default-line-select select {
  width: 100%;
  height: 100%;
  padding: 0 42px 0 13px;
  color: var(--text);
  font-size: 13px;
  background: transparent;
  border: 0;
  outline: 0;
  appearance: none;
  cursor: pointer;
}

.default-line-select svg {
  position: absolute;
  right: 13px;
  color: var(--muted);
  pointer-events: none;
}

.default-line-select:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.default-line-settings__feedback {
  display: flex;
  align-items: center;
  gap: 5px;
  margin: 0;
  color: var(--success);
  font-size: 12px;
}

.default-line-settings__feedback.is-error {
  color: var(--danger);
}
</style>
