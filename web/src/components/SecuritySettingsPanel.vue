<script setup lang="ts">
import { ShieldCheck } from '@lucide/vue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import type { AccountSession } from '../api/types'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import { sessionState } from '../state/session'
import AccountSecurityForm from './AccountSecurityForm.vue'
import AccountSessionsPanel from './AccountSessionsPanel.vue'
import SettingsLoadBoundary from './settings/SettingsLoadBoundary.vue'

const { t } = useI18n()
const sessions = ref<AccountSession[]>([])
const status = ref<'loading' | 'ready' | 'error'>('loading')
const loadError = ref('')
const revokingID = ref('')
const revokingOthers = ref(false)

async function loadSessions(): Promise<void> {
  status.value = 'loading'
  loadError.value = ''
  try {
    sessions.value = await gateway.listAccountSessions()
    status.value = 'ready'
  } catch (cause) {
    loadError.value =
      cause instanceof Error ? cause.message : t('account.sessionsLoadFailed')
    status.value = 'error'
  }
}

async function revokeSession(target: AccountSession, label: string): Promise<void> {
  if (revokingID.value || revokingOthers.value) return
  const pairing = target.kind === 'ios'
  // Removing an iOS session revokes its pairing credential, not just a sign-in.
  const confirmed = await requestConfirmation(
    pairing
      ? {
          title: t('iosPairing.revokeTitle'),
          message: t('iosPairing.revokeMessage'),
          confirmLabel: t('iosPairing.revokeConfirm'),
          tone: 'danger'
        }
      : {
          title: t('account.logoutDeviceTitle'),
          message: t('account.logoutDeviceMessage', { device: label }),
          confirmLabel: t('account.logoutDevice'),
          tone: 'danger'
        }
  )
  if (!confirmed) return
  const id = target.id
  revokingID.value = id
  try {
    await gateway.logoutAccountSession(id)
    sessions.value = sessions.value.filter(session => session.id !== id)
    showSuccess(pairing ? t('account.pairingRevoked') : t('account.sessionLoggedOut'))
  } catch (cause) {
    const fallback = pairing ? t('iosPairing.revokeFailed') : t('account.sessionLogoutFailed')
    showError(cause instanceof Error ? cause.message : fallback)
  } finally {
    revokingID.value = ''
  }
}

async function revokeOtherSessions(): Promise<void> {
  if (revokingID.value || revokingOthers.value) return
  // The server also revokes every iOS pairing credential for this account.
  const includesPairing = sessions.value.some(session => session.kind === 'ios')
  const confirmed = await requestConfirmation({
    title: t('account.logoutOtherDevicesTitle'),
    message: includesPairing
      ? t('account.logoutOtherDevicesWithPairingMessage')
      : t('account.logoutOtherDevicesMessage'),
    confirmLabel: t('account.logoutOtherDevicesConfirm'),
    tone: 'danger'
  })
  if (!confirmed) return
  revokingOthers.value = true
  try {
    await gateway.logoutOtherAccountSessions()
    sessions.value = sessions.value.filter(session => session.current)
    showSuccess(t('account.otherSessionsLoggedOut'))
  } catch (cause) {
    showError(cause instanceof Error ? cause.message : t('account.sessionLogoutFailed'))
    await loadSessions()
  } finally {
    revokingOthers.value = false
  }
}

onMounted(() => {
  void loadSessions()
})
</script>

<template>
  <SettingsLoadBoundary
    :loading="status === 'loading'"
    :error="status === 'error'"
    :loading-title="t('common.loading')"
    loading-shape="form"
    :error-title="t('account.sessionsLoadFailed')"
    :detail="loadError"
    retryable
    @retry="loadSessions"
  >
    <div class="security-settings-panel">
      <section class="security-identity" aria-labelledby="security-identity-title">
        <span class="security-identity__icon" aria-hidden="true">
          <ShieldCheck :size="20" />
        </span>
        <span class="security-identity__copy">
          <strong id="security-identity-title">{{ sessionState.username }}</strong>
          <small>
            {{
              sessionState.role === 'admin'
                ? t('account.administratorRole')
                : t('account.memberRole')
            }}
          </small>
        </span>
        <span class="security-identity__role">
          {{
            sessionState.role === 'admin'
              ? t('account.administrator')
              : t('account.member')
          }}
        </span>
      </section>

      <AccountSecurityForm />

      <AccountSessionsPanel
        :sessions="sessions"
        :revoking-session-id="revokingID"
        :revoking-others="revokingOthers"
        @revoke="revokeSession"
        @revoke-others="revokeOtherSessions"
      />
    </div>
  </SettingsLoadBoundary>
</template>

<style scoped>
.security-settings-panel {
  display: grid;
  width: 100%;
  max-width: var(--settings-preference-content-max);
  gap: 28px;
}

.security-identity {
  display: flex;
  max-width: var(--settings-preference-content-max);
  min-height: 66px;
  align-items: center;
  gap: 11px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.security-identity__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.security-identity__copy {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}

.security-identity__copy strong {
  overflow: hidden;
  color: var(--text);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.security-identity__copy small {
  color: var(--muted);
  font-size: 11px;
}

.security-identity__role {
  flex: 0 0 auto;
  padding: 5px 9px;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
  background: var(--accent-soft);
  border-radius: 999px;
}
</style>
