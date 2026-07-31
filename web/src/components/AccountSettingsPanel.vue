<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { Check, ContactRound, LoaderCircle, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { showSuccess } from '../state/feedback'
import { sessionState, setSessionProfileContact } from '../state/session'
import { contactsResource, loadContacts } from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import AccountSecurityForm from './AccountSecurityForm.vue'
import DefaultLineSettingsForm from './DefaultLineSettingsForm.vue'
import RecordingSettingsForm from './RecordingSettingsForm.vue'
import SystemSettingsForm from './SystemSettingsForm.vue'

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
const saving = ref(false)
const error = ref('')
const saved = ref(false)
let savedTimer: ReturnType<typeof globalThis.setTimeout> | undefined

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
    if (savedTimer) globalThis.clearTimeout(savedTimer)
    savedTimer = globalThis.setTimeout(() => {
      saved.value = false
      savedTimer = undefined
    }, 2200)
    showSuccess(t('account.profileSaved'))
    emit('profileSaved')
  } catch (cause) {
    selectedContactID.value = sessionState.profileContactID
    error.value = cause instanceof Error ? cause.message : t('account.profileSaveFailed')
  } finally {
    saving.value = false
  }
}

function changeProfile(): void {
  void nextTick(saveProfile)
}

onMounted(() => {
  void loadContacts()
})

onBeforeUnmount(() => {
  if (savedTimer) globalThis.clearTimeout(savedTimer)
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
        <span
          v-if="saving || saved"
          class="account-profile__state"
          role="status"
          aria-live="polite"
        >
          <LoaderCircle v-if="saving" class="spin" :size="17" />
          <Check v-else :size="17" />
          {{ saving ? t('common.saving') : t('common.saved') }}
        </span>
      </div>
      <p v-if="error" class="account-profile__feedback is-error" role="alert">
        {{ error }}
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
