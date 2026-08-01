<script setup lang="ts">
import { ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { sessionState } from '../state/session'
import AccountSecurityForm from './AccountSecurityForm.vue'

const { t } = useI18n()
</script>

<template>
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
  </div>
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
