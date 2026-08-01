<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { LoaderCircle, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import {
  bootstrapResource,
  lineKey,
  lineLabel,
  loadBootstrap,
  updateDefaultLine
} from '../state/workspace'
import SelectControl from './SelectControl.vue'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const selected = ref('')
const saveMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error ? cause.message : t('device.defaultLineSaveFailed')
})
const saving = saveMutation.saving

const lines = computed(() => bootstrapResource.data?.lines || [])
const options = computed(() =>
  lines.value.map(line => ({
    value: lineKey(line),
    label: lineLabel(line),
    description: line.phone_number || ''
  }))
)
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

async function changeDefault(lineID: string): Promise<void> {
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
      <SelectControl
        v-else
        :model-value="selected"
        :options="options"
        :label="t('users.defaultLine')"
        :disabled="saving"
        @change="changeDefault"
      />
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
