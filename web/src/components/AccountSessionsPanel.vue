<script setup lang="ts">
import { LoaderCircle, LogOut, Monitor, ShieldCheck, Smartphone } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountSession } from '../api/types'
import { formatDateTime } from '../utils/format'
import SettingsControlRow from './settings/SettingsControlRow.vue'
import SettingsSection from './settings/SettingsSection.vue'

const props = defineProps<{
  sessions: AccountSession[]
  revokingSessionId: string
  revokingOthers: boolean
}>()

const emit = defineEmits<{
  revoke: [id: string]
  revokeOthers: []
}>()

const { t } = useI18n()
const otherSessionCount = computed(
  () => props.sessions.filter(session => !session.current).length
)

function browserName(userAgent = ''): string {
  if (/Edg(?:A|iOS)?\//.test(userAgent)) return 'Microsoft Edge'
  if (/OPR\//.test(userAgent)) return 'Opera'
  if (/FxiOS\/|Firefox\//.test(userAgent)) return 'Firefox'
  if (/CriOS\/|Chrome\//.test(userAgent)) return 'Chromium'
  if (/Version\/.*Safari\//.test(userAgent)) return 'Safari'
  return t('account.unknownBrowser')
}

function platformName(userAgent = ''): string {
  if (/iPad|iPhone|iPod/.test(userAgent)) return 'iOS'
  if (/Android/.test(userAgent)) return 'Android'
  if (/Windows/.test(userAgent)) return 'Windows'
  if (/Macintosh|Mac OS X/.test(userAgent)) return 'macOS'
  if (/Linux/.test(userAgent)) return 'Linux'
  return t('account.unknownDevice')
}

function sessionTitle(session: AccountSession): string {
  if (session.kind === 'ios') {
    if (session.device?.device_name) return session.device.device_name
    return session.paired ? t('account.pairedIPhone') : t('account.pendingIPhone')
  }
  return `${browserName(session.user_agent)} · ${platformName(session.user_agent)}`
}

function sessionDescription(session: AccountSession): string {
  if (session.kind === 'ios') {
    const model = [
      session.device?.device_model,
      session.device?.device_model_identifier
    ].filter(Boolean).join(' · ')
    const operatingSystem = [
      session.device?.os_name,
      session.device?.os_version
    ].filter(Boolean).join(' ')
    const appVersion = session.device?.app_build
      ? `${session.device?.app_version || ''} (${session.device.app_build})`
      : session.device?.app_version
    return [
      model ? `${t('iosPairing.deviceModel')}: ${model}` : '',
      operatingSystem
        ? `${t('iosPairing.operatingSystem')}: ${operatingSystem}`
        : '',
      appVersion ? `${t('iosPairing.appVersion')}: ${appVersion}` : '',
      session.created_at
        ? `${t('iosPairing.createdAt')}: ${formatDateTime(session.created_at)}`
        : '',
      session.paired_at
        ? `${t('iosPairing.pairedAt')}: ${formatDateTime(session.paired_at)}`
        : '',
      session.last_seen_at
        ? `${t('iosPairing.lastSeenAt')}: ${formatDateTime(session.last_seen_at)}`
        : ''
    ].filter(Boolean).join(t('common.listSeparator'))
  }
  const details = [session.access_ip?.trim(), session.access_host?.trim()]
  const activityAt = session.last_seen_at || session.created_at
  details.push(
    t('account.lastActiveAt', {
      date: formatDateTime(activityAt)
    })
  )
  return details.filter(Boolean).join(t('common.listSeparator'))
}
</script>

<template>
  <SettingsSection
    :title="t('account.signedInDevices')"
    title-id="account-sessions-title"
    :description="t('account.signedInDevicesDescription')"
  >
    <template #icon><ShieldCheck :size="20" /></template>

    <div class="account-sessions">
      <SettingsControlRow
        v-for="session in sessions"
        :key="session.id"
        :title="sessionTitle(session)"
        :description="sessionDescription(session)"
      >
        <template #icon>
          <Smartphone v-if="session.kind === 'ios'" :size="19" />
          <Monitor v-else :size="19" />
        </template>

        <span v-if="session.current" class="account-sessions__current">
          {{ t('account.currentDevice') }}
        </span>
        <button
          v-else
          class="secondary-button account-sessions__logout"
          type="button"
          :disabled="Boolean(revokingSessionId) || revokingOthers"
          @click="emit('revoke', session.id)"
        >
          <LoaderCircle v-if="revokingSessionId === session.id" class="spin" :size="17" />
          <LogOut v-else :size="17" />
          {{ t('account.logoutDevice') }}
        </button>
      </SettingsControlRow>

      <div v-if="otherSessionCount" class="account-sessions__actions">
        <button
          class="danger-button"
          type="button"
          :disabled="Boolean(revokingSessionId) || revokingOthers"
          @click="emit('revokeOthers')"
        >
          <LoaderCircle v-if="revokingOthers" class="spin" :size="17" />
          <LogOut v-else :size="17" />
          {{ t('account.logoutOtherDevices') }}
        </button>
      </div>
    </div>
  </SettingsSection>
</template>

<style scoped>
.account-sessions {
  display: grid;
}

.account-sessions__current {
  padding: 5px 9px;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
  background: var(--accent-soft);
  border-radius: 999px;
}

.account-sessions__logout {
  min-height: 36px;
  padding: 7px 11px;
}

.account-sessions__actions {
  display: flex;
  justify-content: flex-end;
  padding-top: 18px;
}

@media (max-width: 860px) {
  .account-sessions__logout,
  .account-sessions__actions .danger-button {
    width: 100%;
  }
}
</style>
