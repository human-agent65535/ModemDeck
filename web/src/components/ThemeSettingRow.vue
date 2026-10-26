<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SunMoon } from '@lucide/vue'
import { setThemePreference, themePreference, type ThemePreference } from '../state/theme'
import SelectControl from './SelectControl.vue'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

const { t } = useI18n()
const options = computed(() => [
  { value: 'system', label: t('settings.themeSystem'), description: t('settings.themeSystemDescription') },
  { value: 'light', label: t('settings.themeLight'), description: t('settings.themeLightDescription') },
  { value: 'dark', label: t('settings.themeDark'), description: t('settings.themeDarkDescription') }
])
const selectedOption = computed(() => options.value.find(option => option.value === themePreference.value))
</script>

<template>
  <SettingsPreferenceRow
    :title="t('settings.appearance')"
    title-id="appearance-title"
    :description="selectedOption?.description"
    control-size="wide"
  >
    <template #icon>
      <SunMoon :size="20" />
    </template>
    <template #control>
      <SelectControl
        :model-value="themePreference"
        :options="options"
        :label="t('settings.appearance')"
        @change="setThemePreference($event as ThemePreference)"
      />
    </template>
  </SettingsPreferenceRow>
</template>
