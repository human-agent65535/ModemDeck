<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, LoaderCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { SystemLanguage, SystemSettings } from '../api/types'
import { gateway } from '../api/client'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { setSystemLanguage, systemLanguage } from '../i18n'
import { bootstrapResource, loadBootstrap } from '../state/workspace'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const initialSettings = bootstrapResource.data?.system_settings
const settings = ref<SystemSettings | undefined>(
  initialSettings ? { ...initialSettings } : undefined
)
const selected = ref<SystemLanguage>(initialSettings?.language || systemLanguage())
const loading = ref(!initialSettings)
const loadError = ref('')
const saveMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error ? cause.message : t('settings.languageSaveFailed')
})
const saving = saveMutation.saving

const options = computed<Array<{
  value: SystemLanguage
  label: string
  description: string
}>>(() => [
  {
    value: 'auto',
    label: t('language.auto'),
    description: t('language.autoDescription')
  },
  {
    value: 'zh-CN',
    label: t('language.zhCN'),
    description: t('language.zhCNDescription')
  },
  {
    value: 'zh-TW',
    label: t('language.zhTW'),
    description: t('language.zhTWDescription')
  },
  {
    value: 'en-US',
    label: t('language.enUS'),
    description: t('language.enUSDescription')
  },
  {
    value: 'ja-JP',
    label: t('language.jaJP'),
    description: t('language.jaJPDescription')
  },
  {
    value: 'vi-VN',
    label: t('language.viVN'),
    description: t('language.viVNDescription')
  },
  {
    value: 'es-ES',
    label: t('language.esES'),
    description: t('language.esESDescription')
  },
  {
    value: 'de-DE',
    label: t('language.deDE'),
    description: t('language.deDEDescription')
  },
  {
    value: 'fr-FR',
    label: t('language.frFR'),
    description: t('language.frFRDescription')
  },
  {
    value: 'pt-BR',
    label: t('language.ptBR'),
    description: t('language.ptBRDescription')
  }
])
const selectedOption = computed(
  () => options.value.find(option => option.value === selected.value) || options.value[0]
)

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const bootstrap = await loadBootstrap()
    const loaded = bootstrap?.system_settings || bootstrapResource.data?.system_settings
    if (!loaded) {
      throw new Error(bootstrapResource.error || t('settings.systemLoadFailed'))
    }
    settings.value = { ...loaded }
    selected.value = loaded.language
    await setSystemLanguage(loaded.language)
  } catch (cause) {
    loadError.value =
      cause instanceof Error ? cause.message : t('settings.systemLoadFailed')
  } finally {
    loading.value = false
  }
}

async function selectLanguage(language: SystemLanguage): Promise<void> {
  if (saving.value || language === selected.value || !settings.value) return

  const previous = selected.value
  selected.value = language
  await setSystemLanguage(language)
  const payload = {
    language,
    expected_revision: settings.value.revision
  }
  const result = await saveMutation.run(() =>
    gateway.updateSystemSettings(payload)
  )
  if (result.ok) {
    const updated = result.value
    settings.value = updated
    if (bootstrapResource.data) {
      bootstrapResource.data.system_settings = updated
    }
    selected.value = updated.language
    await setSystemLanguage(updated.language)
  } else {
    selected.value = previous
    await setSystemLanguage(previous)
  }
}

function onLanguageChange(event: Event): void {
  void selectLanguage((event.target as HTMLSelectElement).value as SystemLanguage)
}

onMounted(() => {
  if (!settings.value) void load()
})
</script>

<template>
  <SettingsPreferenceRow
    class="system-settings"
    :title="t('settings.systemLanguage')"
    title-id="system-language-title"
    :description="selectedOption?.description"
    control-size="wide"
  >
    <template #icon>
      <Globe2 :size="20" />
    </template>
    <template #control>
      <div v-if="loading" class="system-settings__state" role="status">
        <LoaderCircle class="spin" :size="18" />
        {{ t('settings.loadingSystem') }}
      </div>
      <label v-else class="system-language-select">
        <span class="sr-only">{{ t('settings.systemLanguage') }}</span>
        <select
          :value="selected"
          :disabled="saving || !settings"
          @change="onLanguageChange"
        >
          <option v-for="option in options" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
        <ChevronDown :size="18" aria-hidden="true" />
      </label>
    </template>
    <template v-if="loadError || saveMutation.error.value" #feedback>
      <p class="system-settings__feedback is-error" role="alert">
        {{ loadError || saveMutation.error.value }}
        <button v-if="!settings" type="button" @click="load">
          {{ t('common.retry') }}
        </button>
      </p>
    </template>
  </SettingsPreferenceRow>
</template>

<style scoped>
.system-settings__state {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.system-language-select {
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

.system-language-select select {
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

.system-language-select svg {
  position: absolute;
  right: 13px;
  color: var(--muted);
  pointer-events: none;
}

.system-language-select:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.system-language-select:has(select:disabled) {
  color: var(--muted);
  background: var(--surface-subtle);
}

.system-settings__feedback {
  display: flex;
  align-items: center;
  gap: 5px;
  margin: 0;
  color: var(--accent-strong);
  font-size: 12px;
}

.system-settings__feedback.is-error {
  color: var(--danger);
}

.system-settings__feedback button {
  margin-left: 6px;
  color: inherit;
  font-weight: 650;
  background: transparent;
}

</style>
