<script setup lang="ts">
import ExternalAccessSettingsPanel from './ExternalAccessSettingsPanel.vue'
import WebCertificateSettingsPanel from './WebCertificateSettingsPanel.vue'
</script>

<template>
  <div class="connectivity-settings-panel">
    <WebCertificateSettingsPanel />
    <ExternalAccessSettingsPanel mode="connectivity" />
  </div>
</template>

<style scoped>
.connectivity-settings-panel {
  display: grid;
  width: 100%;
  gap: 28px;
}

.connectivity-settings-panel > :deep(.ios-settings) {
  padding-top: 28px;
  border-top: 1px solid var(--border);
}
</style>
