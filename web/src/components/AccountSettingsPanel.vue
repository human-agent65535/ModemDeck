<script setup lang="ts">
import { ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { sessionState } from '../state/session'
import AccountProfileSetting from './AccountProfileSetting.vue'
import AccountSecurityForm from './AccountSecurityForm.vue'
import DefaultLineSettingsForm from './DefaultLineSettingsForm.vue'
import SystemSettingsForm from './SystemSettingsForm.vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    showIdentity?: boolean
    showProfile?: boolean
    showLanguage?: boolean
  }>(),
  {
    showIdentity: true,
    showProfile: true,
    showLanguage: true
  }
)
const emit = defineEmits<{
  profileSaved: []
}>()
</script>

<template>
  <div class="account-settings-panel">
    <section
      v-if="props.showIdentity"
      class="account-identity"
      aria-labelledby="account-identity-title"
    >
      <span class="account-identity__icon"><ShieldCheck :size="20" /></span>
      <div>
        <h3 id="account-identity-title">{{ sessionState.username }}</h3>
        <p>
          {{
            sessionState.role === 'admin'
              ? t('account.administratorRole')
              : t('account.memberRole')
          }}
        </p>
      </div>
      <span class="account-role">
        {{ sessionState.role === 'admin' ? t('account.administrator') : t('account.member') }}
      </span>
    </section>

    <AccountProfileSetting
      v-if="props.showProfile"
      @saved="emit('profileSaved')"
    />

    <div class="account-preferences">
      <DefaultLineSettingsForm />
      <SystemSettingsForm v-if="props.showLanguage" />
    </div>

    <AccountSecurityForm />
  </div>
</template>

<style scoped>
.account-settings-panel {
  display: grid;
  width: 100%;
  gap: 28px;
}

.account-preferences {
  display: grid;
  gap: 22px;
}

.account-preferences :deep(.settings-preference-row) {
  max-width: none;
}

.account-identity {
  display: flex;
  min-height: 66px;
  align-items: center;
  gap: 11px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.account-identity__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.account-identity > div {
  min-width: 0;
  flex: 1;
}

.account-settings-panel h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.account-settings-panel p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.account-role {
  padding: 5px 9px;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
  background: var(--accent-soft);
  border-radius: 999px;
}
</style>
