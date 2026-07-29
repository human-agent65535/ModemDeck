<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, LoaderCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { SystemLanguage, SystemSettings } from '../api/types'
import { gateway } from '../api/client'
import { setSystemLanguage, systemLanguage } from '../i18n'
import AccountSecurityForm from './AccountSecurityForm.vue'

const { t } = useI18n()
const settings = ref<SystemSettings>()
const selected = ref<SystemLanguage>(systemLanguage())
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref(false)

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
  error.value = ''
  try {
    const loaded = await gateway.getSystemSettings()
    settings.value = loaded
    selected.value = loaded.language
    setSystemLanguage(loaded.language)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('settings.systemLoadFailed')
  } finally {
    loading.value = false
  }
}

async function selectLanguage(language: SystemLanguage): Promise<void> {
  if (saving.value || language === selected.value || !settings.value) return

  const previous = selected.value
  selected.value = language
  setSystemLanguage(language)
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    const updated = await gateway.updateSystemSettings({
      language,
      expected_revision: settings.value.revision
    })
    settings.value = updated
    selected.value = updated.language
    setSystemLanguage(updated.language)
    saved.value = true
  } catch (cause) {
    selected.value = previous
    setSystemLanguage(previous)
    error.value = cause instanceof Error ? cause.message : t('settings.languageSaveFailed')
  } finally {
    saving.value = false
  }
}

function onLanguageChange(event: Event): void {
  void selectLanguage((event.target as HTMLSelectElement).value as SystemLanguage)
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="system-settings">
    <section aria-labelledby="system-language-title">
      <div class="system-settings__row">
        <span class="system-settings__icon"><Globe2 :size="20" /></span>
        <div class="system-settings__copy">
          <h3 id="system-language-title">{{ t('settings.systemLanguage') }}</h3>
          <p>{{ selectedOption?.description }}</p>
        </div>

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
          <LoaderCircle v-if="saving" class="spin" :size="18" aria-hidden="true" />
          <ChevronDown v-else :size="18" aria-hidden="true" />
        </label>
      </div>

      <p v-if="error" class="system-settings__feedback is-error" role="alert">
        {{ error }}
        <button v-if="!settings" type="button" @click="load">
          {{ t('common.retry') }}
        </button>
      </p>
      <p v-else-if="saved" class="system-settings__feedback" role="status">
        {{ t('settings.languageSaved') }}
      </p>
    </section>

    <AccountSecurityForm />
  </div>
</template>

<style scoped>
.system-settings {
  max-width: 680px;
}

.system-settings > section {
  padding-bottom: 22px;
  border-bottom: 1px solid var(--border);
}

.system-settings__row {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 11px;
}

.system-settings__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.system-settings__copy {
  min-width: 0;
  flex: 1;
}

.system-settings h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.system-settings__copy p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.system-settings__state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.system-language-select {
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
  margin-top: 12px;
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

.system-settings :deep(.account-security) {
  margin-top: 22px;
}

@media (max-width: 640px) {
  .system-settings__row {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .system-language-select {
    width: 100%;
    margin-left: 47px;
  }
}
</style>
