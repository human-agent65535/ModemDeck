<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ChevronDown, LoaderCircle, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  bootstrapResource,
  lineKey,
  lineLabel,
  loadBootstrap,
  updateDefaultLine
} from '../state/workspace'

const { t } = useI18n()
const selected = ref('')
const saving = ref(false)
const error = ref('')

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
  try {
    await updateDefaultLine(lineID)
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
</script>

<template>
  <section class="default-line-settings" aria-labelledby="default-line-settings-title">
    <div class="default-line-settings__row">
      <span class="default-line-settings__icon"><RadioTower :size="20" /></span>
      <div class="default-line-settings__copy">
        <h3 id="default-line-settings-title">{{ t('users.defaultLine') }}</h3>
        <p>{{ t('account.defaultLineDescription') }}</p>
      </div>

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
    </div>

    <p v-if="error" class="default-line-settings__feedback" role="alert">
      {{ error }}
    </p>
  </section>
</template>

<style scoped>
.default-line-settings {
  max-width: 680px;
  padding-bottom: 22px;
  border-bottom: 1px solid var(--border);
}

.default-line-settings__row {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 11px;
}

.default-line-settings__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.default-line-settings__copy {
  min-width: 0;
  flex: 1;
}

.default-line-settings h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.default-line-settings__copy p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.default-line-settings__state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.default-line-select {
  position: relative;
  display: flex;
  width: min(240px, 45%);
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
  margin-top: 12px;
  color: var(--danger);
  font-size: 12px;
}

@media (max-width: 640px) {
  .default-line-settings__row {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .default-line-select,
  .default-line-settings__state {
    width: 100%;
    margin-left: 47px;
  }
}
</style>
