<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Check, ChevronDown, LoaderCircle, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
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
const saving = ref(false)
const error = ref('')
const saved = ref(false)
let savedTimer: ReturnType<typeof globalThis.setTimeout> | undefined

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
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    await updateDefaultLine(lineID)
    saved.value = true
    if (savedTimer) globalThis.clearTimeout(savedTimer)
    savedTimer = globalThis.setTimeout(() => {
      saved.value = false
      savedTimer = undefined
    }, 2200)
  } catch (cause) {
    selected.value = previous
    error.value =
      cause instanceof Error ? cause.message : t('device.defaultLineSaveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadBootstrap()
})

onBeforeUnmount(() => {
  if (savedTimer) globalThis.clearTimeout(savedTimer)
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
        <LoaderCircle v-if="saving" class="spin" :size="18" aria-hidden="true" />
        <ChevronDown v-else :size="18" aria-hidden="true" />
      </label>
    </template>
    <template v-if="error || saved" #feedback>
      <p v-if="error" class="default-line-settings__feedback is-error" role="alert">
        {{ error }}
      </p>
      <p v-else class="default-line-settings__feedback" role="status">
        <Check :size="15" /> {{ t('common.saved') }}
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
