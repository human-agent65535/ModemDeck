<script setup lang="ts">
import { AudioLines, BellRing, PhoneCall, Play, Square } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { browserNotificationState, toggleBrowserNotifications } from '../state/browserNotifications'
import {
  callTestInProgress,
  callTestSession,
  callTestState,
  callTestStatusLabel,
  callTestUnavailable,
  scheduleCallTest,
  stopCallTest
} from '../state/callTestRuntime'
import { showCallSurface } from '../state/ui'
import SettingsModuleCard from './settings/SettingsModuleCard.vue'

const { t } = useI18n()
</script>

<template>
  <SettingsModuleCard
    :title="t('callTest.title')"
    title-id="call-test-title"
    :description="t('callTest.description')"
    icon-tone="blue"
  >
    <template #icon><AudioLines :size="20" /></template>
    <div class="call-test">
      <div v-if="!browserNotificationState.active" class="call-test__notification">
        <p>{{ t('callTest.notificationHint') }}</p>
        <button
          class="text-button"
          type="button"
          :disabled="browserNotificationState.requesting"
          @click="toggleBrowserNotifications"
        >
          <BellRing :size="16" /> {{ t('shell.notificationsEnable') }}
        </button>
        <p v-if="browserNotificationState.error" class="call-test__error" role="alert">
          {{ browserNotificationState.error }}
        </p>
      </div>
      <p v-if="callTestState.status !== 'idle'" role="status">{{ callTestStatusLabel }}</p>
      <p v-if="callTestState.error" class="call-test__error" role="alert">{{ callTestState.error }}</p>
      <p v-if="callTestUnavailable && !callTestInProgress">{{ t('callTest.unavailable') }}</p>
      <div class="call-test__actions">
        <button
          v-if="!callTestInProgress"
          class="primary-button"
          type="button"
          :disabled="callTestUnavailable"
          @click="scheduleCallTest"
        >
          <Play :size="16" /> {{ t('callTest.start') }}
        </button>
        <button v-if="callTestSession" class="secondary-button" type="button" @click="showCallSurface">
          <PhoneCall :size="16" /> {{ t('shell.returnToCall') }}
        </button>
        <button v-if="callTestInProgress" class="secondary-button" type="button" @click="stopCallTest()">
          <Square :size="16" /> {{ t('callTest.stop') }}
        </button>
      </div>
    </div>
  </SettingsModuleCard>
</template>

<style scoped>
.call-test {
  display: grid;
  gap: 16px;
}
.call-test__notification {
  display: grid;
  justify-items: start;
  gap: 8px;
}
.call-test p {
  margin: 0;
  color: var(--muted);
  font-size: 13px;
  line-height: 1.6;
}
.call-test p.call-test__error {
  color: var(--danger);
}
.call-test__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
@media (max-width: 560px) {
  .call-test__actions > button {
    flex: 1 1 auto;
    justify-content: center;
  }
}
</style>
