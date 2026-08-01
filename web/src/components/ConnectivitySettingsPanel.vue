<script setup lang="ts">
import { Cloud } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import ExternalAccessSettingsPanel from './ExternalAccessSettingsPanel.vue'
import WebCertificateSettingsPanel from './WebCertificateSettingsPanel.vue'
import SettingsSection from './settings/SettingsSection.vue'

const { t } = useI18n()
</script>

<template>
  <div class="connectivity-settings-panel">
    <WebCertificateSettingsPanel />
    <SettingsSection
      :title="t('connectivity.remoteAccess')"
      title-id="remote-access-title"
      :description="t('connectivity.remoteAccessDescription')"
      mode="modules"
      icon-tone="blue"
    >
      <template #icon>
        <Cloud :size="18" />
      </template>
      <ExternalAccessSettingsPanel mode="connectivity" :heading-level="4" />
    </SettingsSection>
  </div>
</template>

<style scoped>
.connectivity-settings-panel {
  display: grid;
  width: 100%;
  gap: 28px;
}

</style>
