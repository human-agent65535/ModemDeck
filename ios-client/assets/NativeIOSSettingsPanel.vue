<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  BellRing,
  LoaderCircle,
  MessageSquareText,
  PhoneIncoming,
  Smartphone
} from '@lucide/vue'
import {
  openNativeIOSNotificationSettings,
  readNativeIOSNotificationStatus,
  requestNativeIOSNotificationPermission,
  showNativeIOSNotification,
  type NativeIOSNotificationStatus
} from '../api/nativeIOS'
import { sessionState } from '../state/session'
import { showError, showSuccess } from '../state/feedback'
import IncomingCallModeControl from './IncomingCallModeControl.vue'
import SettingsControlRow from './settings/SettingsControlRow.vue'
import SettingsSection from './settings/SettingsSection.vue'

const { t } = useI18n()
const notification = ref<NativeIOSNotificationStatus>({
  authorization: 'unknown',
  enabled: false,
  canRequest: false
})
const loading = ref(false)
const testing = ref(false)
const error = ref('')

const notificationStatusLabel = computed(() => {
  switch (notification.value.authorization) {
    case 'authorized':
    case 'ephemeral':
      return t('nativeIOS.notificationAuthorized')
    case 'provisional':
      return t('nativeIOS.notificationProvisional')
    case 'denied':
      return t('nativeIOS.notificationDenied')
    case 'notDetermined':
      return t('nativeIOS.notificationNotDetermined')
    default:
      return t('nativeIOS.notificationUnavailable')
  }
})

const notificationActionLabel = computed(() =>
  notification.value.canRequest
    ? t('nativeIOS.enableNotifications')
    : notification.value.authorization === 'unknown'
      ? t('incomingCallMode.reload')
      : t('nativeIOS.openNotificationSettings')
)

async function refreshNotifications(): Promise<void> {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    notification.value = await readNativeIOSNotificationStatus()
  } catch {
    notification.value = {
      authorization: 'unknown',
      enabled: false,
      canRequest: false
    }
    error.value = t('nativeIOS.notificationPermissionFailed')
  } finally {
    loading.value = false
  }
}

async function manageNotifications(): Promise<void> {
  if (loading.value) return
  if (notification.value.authorization === 'unknown') {
    await refreshNotifications()
    return
  }
  loading.value = true
  error.value = ''
  try {
    if (notification.value.canRequest) {
      notification.value = await requestNativeIOSNotificationPermission()
    } else {
      await openNativeIOSNotificationSettings()
    }
  } catch {
    error.value = t('nativeIOS.notificationPermissionFailed')
  } finally {
    loading.value = false
  }
}

async function sendTestSMSNotification(): Promise<void> {
  if (testing.value || !notification.value.enabled) return
  testing.value = true
  error.value = ''
  try {
    const scheduled = await showNativeIOSNotification({
      title: t('nativeIOS.testSMSNotificationTitle'),
      body: t('nativeIOS.testSMSNotificationBody'),
      identifier: `modemdeck-test-sms-${Date.now()}`
    })
    if (!scheduled) {
      await refreshNotifications()
      error.value = t('nativeIOS.testNotificationFailed')
      showError(error.value)
    } else {
      showSuccess(t('nativeIOS.testNotificationScheduled'))
    }
  } catch {
    error.value = t('nativeIOS.testNotificationFailed')
    showError(error.value)
  } finally {
    testing.value = false
  }
}

function refreshAfterForeground(): void {
  if (document.visibilityState === 'visible') void refreshNotifications()
}

onMounted(() => {
  void refreshNotifications()
  window.addEventListener('focus', refreshAfterForeground)
  document.addEventListener('visibilitychange', refreshAfterForeground)
})

onBeforeUnmount(() => {
  window.removeEventListener('focus', refreshAfterForeground)
  document.removeEventListener('visibilitychange', refreshAfterForeground)
})
</script>

<template>
  <SettingsSection
    class="native-ios-settings"
    :title="t('nativeIOS.settingsTitle')"
    title-id="native-ios-settings-title"
    :description="t('nativeIOS.settingsDescription')"
    icon-tone="blue"
  >
    <template #icon><Smartphone :size="19" /></template>

    <SettingsControlRow
      v-if="sessionState.role === 'admin'"
      :title="t('nativeIOS.incomingCalls')"
      :description="t('nativeIOS.incomingCallsDescription')"
    >
      <template #icon><PhoneIncoming :size="18" /></template>
      <IncomingCallModeControl />
    </SettingsControlRow>

    <SettingsControlRow
      :title="t('nativeIOS.notifications')"
      :description="t('nativeIOS.notificationsDescription')"
    >
      <template #icon><BellRing :size="18" /></template>
      <div class="native-ios-settings__permission">
        <span
          class="native-ios-settings__status"
          :class="{ 'is-enabled': notification.enabled }"
        >
          {{ notificationStatusLabel }}
        </span>
        <button
          class="native-ios-settings__action"
          type="button"
          :disabled="loading"
          @click="manageNotifications"
        >
          <LoaderCircle v-if="loading" class="spin" :size="15" />
          {{ notificationActionLabel }}
        </button>
      </div>
    </SettingsControlRow>

    <SettingsControlRow
      v-if="notification.enabled"
      :title="t('nativeIOS.sendTestSMSNotification')"
      :description="t('nativeIOS.testSMSNotificationBody')"
    >
      <template #icon><MessageSquareText :size="18" /></template>
      <button
        class="native-ios-settings__action is-secondary"
        type="button"
        :disabled="testing"
        @click="sendTestSMSNotification"
      >
        <LoaderCircle v-if="testing" class="spin" :size="15" />
        <MessageSquareText v-else :size="15" />
        {{ t('nativeIOS.sendTestSMSNotification') }}
      </button>
    </SettingsControlRow>

    <p v-if="error" class="native-ios-settings__error" role="alert">{{ error }}</p>
  </SettingsSection>
</template>

<style scoped>
.native-ios-settings {
  margin-top: 26px;
}

.native-ios-settings__status {
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.native-ios-settings__status.is-enabled {
  color: var(--accent-strong);
}

.native-ios-settings__permission {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.native-ios-settings__action {
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  gap: 6px;
  padding: 0 11px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 700;
  background: var(--accent-soft);
  border: 1px solid var(--success-border);
  border-radius: 8px;
}

.native-ios-settings__action.is-secondary {
  color: var(--text);
  background: var(--surface);
  border-color: var(--border);
}

.native-ios-settings__action:disabled {
  cursor: wait;
  opacity: 0.65;
}

.native-ios-settings__error {
  margin: 10px 0 0;
  color: var(--danger);
  font-size: 11px;
}

@media (max-width: 860px) {
  .native-ios-settings__permission {
    width: 100%;
    justify-content: space-between;
  }

  .native-ios-settings__action {
    justify-content: center;
    min-height: 40px;
  }

  .native-ios-settings__action.is-secondary {
    width: 100%;
  }
}
</style>
