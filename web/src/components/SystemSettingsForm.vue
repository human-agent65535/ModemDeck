<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, Globe2, LoaderCircle } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { SystemLanguage, SystemSettings } from '../api/types'
import { gateway } from '../api/client'
import { setSystemLanguage, systemLanguage } from '../i18n'

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
    value: 'en-US',
    label: t('language.enUS'),
    description: t('language.enUSDescription')
  }
])

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

onMounted(() => {
  void load()
})
</script>

<template>
  <section class="system-settings" aria-labelledby="system-language-title">
    <header>
      <span class="system-settings__icon"><Globe2 :size="20" /></span>
      <div>
        <h3 id="system-language-title">{{ t('settings.systemLanguage') }}</h3>
        <p>{{ t('settings.systemLanguageDescription') }}</p>
      </div>
    </header>

    <div v-if="loading" class="system-settings__state" role="status">
      <LoaderCircle class="spin" :size="18" />
      {{ t('settings.loadingSystem') }}
    </div>

    <div v-else class="system-language-options" role="radiogroup">
      <label
        v-for="option in options"
        :key="option.value"
        class="system-language-option"
        :class="{ 'is-selected': selected === option.value }"
      >
        <input
          type="radio"
          name="system-language"
          :value="option.value"
          :checked="selected === option.value"
          :disabled="saving || !settings"
          @change="selectLanguage(option.value)"
        />
        <span>
          <strong>{{ option.label }}</strong>
          <small>{{ option.description }}</small>
        </span>
        <LoaderCircle
          v-if="saving && selected === option.value"
          class="spin"
          :size="18"
          aria-hidden="true"
        />
        <Check
          v-else-if="selected === option.value"
          :size="18"
          aria-hidden="true"
        />
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
</template>

<style scoped>
.system-settings {
  max-width: 680px;
}

.system-settings > header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
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

.system-settings h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.system-settings header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.system-settings__state {
  display: flex;
  min-height: 72px;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 12px;
}

.system-language-options {
  display: grid;
  gap: 8px;
  padding-top: 16px;
}

.system-language-option {
  display: grid;
  min-height: 68px;
  align-items: center;
  grid-template-columns: 20px minmax(0, 1fr) 20px;
  gap: 12px;
  padding: 10px 14px;
  border: 1px solid var(--border);
  border-radius: 7px;
  cursor: pointer;
}

.system-language-option:hover {
  background: var(--surface-hover);
}

.system-language-option.is-selected {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: var(--accent);
}

.system-language-option input {
  width: 16px;
  height: 16px;
  accent-color: var(--accent-strong);
}

.system-language-option > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.system-language-option strong {
  color: var(--text);
  font-size: 13px;
}

.system-language-option small {
  color: var(--muted);
  font-size: 11px;
}

.system-language-option:has(input:disabled) {
  cursor: wait;
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
</style>
