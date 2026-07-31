<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { ContactRound, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { sessionState, setSessionProfileContact } from '../state/session'
import { contactsResource, loadContacts } from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import AccountSecurityForm from './AccountSecurityForm.vue'
import DefaultLineSettingsForm from './DefaultLineSettingsForm.vue'
import RecordingSettingsForm from './RecordingSettingsForm.vue'
import SystemSettingsForm from './SystemSettingsForm.vue'
import SettingsSaveStatus from './settings/SettingsSaveStatus.vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    showIdentity?: boolean
  }>(),
  {
    showIdentity: true
  }
)
const emit = defineEmits<{
  profileSaved: []
}>()
const selectedContactID = ref(sessionState.profileContactID)
const profileMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error ? cause.message : t('account.profileSaveFailed')
})
const saving = profileMutation.saving

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
  const result = await profileMutation.run(() =>
    gateway.setAccountContact(selectedContactID.value)
  )
  if (result.ok) {
    setSessionProfileContact(selectedContactID.value)
    emit('profileSaved')
  } else {
    selectedContactID.value = sessionState.profileContactID
  }
}

function changeProfile(): void {
  void nextTick(saveProfile)
}

onMounted(() => {
  void loadContacts()
})

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

    <section
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
            @change="changeProfile"
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
        <SettingsSaveStatus
          :status="profileMutation.status.value"
          :error="profileMutation.error.value"
        />
      </div>
      <p
        v-if="profileMutation.error.value"
        class="account-profile__feedback is-error"
        role="alert"
      >
        {{ profileMutation.error.value }}
      </p>
    </section>

    <div class="account-preferences">
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

.account-profile__state {
  display: inline-flex;
  min-width: 70px;
  min-height: 40px;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
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

  .account-profile__state {
    min-width: 0;
    grid-column: 2;
  }
}
</style>
