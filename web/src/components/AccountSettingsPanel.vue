<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, ContactRound, LoaderCircle, Save, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { sessionState, setSessionProfileContact } from '../state/session'
import { contactsResource, loadContacts } from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import AccountSecurityForm from './AccountSecurityForm.vue'
import DefaultLineSettingsForm from './DefaultLineSettingsForm.vue'
import RecordingSettingsForm from './RecordingSettingsForm.vue'
import SystemSettingsForm from './SystemSettingsForm.vue'

const { t } = useI18n()
const emit = defineEmits<{
  profileSaved: []
}>()
const selectedContactID = ref(sessionState.profileContactID)
const saving = ref(false)
const error = ref('')
const saved = ref(false)

const profileContact = computed(() =>
  contactsResource.data.find(contact => contact.id === selectedContactID.value)
)
const profileNumber = computed(
  () =>
    profileContact.value?.phones.find(phone => phone.primary)?.number ||
    profileContact.value?.phones[0]?.number ||
    ''
)
const profileChanged = computed(
  () => selectedContactID.value !== sessionState.profileContactID
)

async function saveProfile(): Promise<void> {
  if (!profileChanged.value || saving.value) return
  saving.value = true
  error.value = ''
  saved.value = false
  try {
    await gateway.setAccountContact(selectedContactID.value)
    setSessionProfileContact(selectedContactID.value)
    saved.value = true
    emit('profileSaved')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('account.profileSaveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  if (!sessionState.mustChangePassword) void loadContacts()
})
</script>

<template>
  <div class="account-settings-panel">
    <section class="account-identity" aria-labelledby="account-identity-title">
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

    <div v-if="sessionState.mustChangePassword" class="account-required" role="alert">
      {{ t('account.passwordChangeRequired') }}
    </div>

    <section
      v-else
      class="account-profile"
      aria-labelledby="account-profile-title"
    >
      <header>
        <span class="account-profile__icon"><ContactRound :size="20" /></span>
        <div>
          <h3 id="account-profile-title">{{ t('account.profileContact') }}</h3>
          <p>{{ t('account.profileContactDescription') }}</p>
        </div>
      </header>

      <div class="account-profile__editor">
        <BaseAvatar
          :name="profileContact?.display_name || sessionState.username"
          :src="profileContact?.avatar"
          size="large"
          :fallback="profileContact ? 'initials' : 'person'"
          :palette-key="profileContact?.id || sessionState.userID"
        />
        <label class="field">
          <span>{{ t('account.contact') }}</span>
          <select
            v-model="selectedContactID"
            :disabled="saving || contactsResource.status === 'loading'"
            @change="saved = false; error = ''"
          >
            <option value="">{{ t('account.noProfileContact') }}</option>
            <option
              v-for="contact in contactsResource.data"
              :key="contact.id"
              :value="contact.id"
            >
              {{ contact.display_name }}
            </option>
          </select>
          <small v-if="profileNumber">{{ profileNumber }}</small>
          <small v-else>{{ t('account.profileContactHint') }}</small>
        </label>
        <button
          class="primary-button"
          type="button"
          :disabled="saving || !profileChanged"
          @click="saveProfile"
        >
          <LoaderCircle v-if="saving" class="spin" :size="17" />
          <Save v-else :size="17" />
          {{ t('common.save') }}
        </button>
      </div>
      <p v-if="error" class="account-profile__feedback is-error" role="alert">
        {{ error }}
      </p>
      <p v-else-if="saved" class="account-profile__feedback" role="status">
        <Check :size="15" /> {{ t('account.profileSaved') }}
      </p>
    </section>

    <div
      v-if="!sessionState.mustChangePassword"
      class="account-preferences"
    >
      <DefaultLineSettingsForm />
      <SystemSettingsForm />
      <RecordingSettingsForm />
    </div>

    <AccountSecurityForm />
  </div>
</template>

<style scoped>
.account-settings-panel {
  display: grid;
  max-width: 760px;
  gap: 28px;
}

.account-preferences {
  display: grid;
  gap: 22px;
}

.account-preferences :deep(.system-settings),
.account-preferences :deep(.recording-settings) {
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

.account-identity__icon,
.account-profile__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.account-identity > div,
.account-profile header > div {
  min-width: 0;
  flex: 1;
}

.account-settings-panel h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.account-settings-panel p,
.account-profile .field small {
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

.account-required {
  padding: 12px 14px;
  color: #7a4b00;
  font-size: 12px;
  background: #fff6dd;
  border: 1px solid #e9cf93;
  border-radius: 8px;
}

.account-profile > header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.account-profile__editor {
  display: grid;
  align-items: end;
  gap: 14px;
  padding-top: 18px;
  grid-template-columns: auto minmax(0, 1fr) auto;
}

.account-profile__editor .field {
  min-width: 0;
}

.account-profile__editor .primary-button {
  min-height: 40px;
}

.account-profile__feedback {
  display: flex;
  align-items: center;
  gap: 5px;
  margin: 10px 0 0;
  color: var(--accent-strong);
}

.account-profile__feedback.is-error {
  color: var(--danger);
}

@media (max-width: 620px) {
  .account-profile__editor {
    align-items: center;
    grid-template-columns: auto minmax(0, 1fr);
  }

  .account-profile__editor .primary-button {
    width: 100%;
    grid-column: 1 / -1;
  }
}
</style>
