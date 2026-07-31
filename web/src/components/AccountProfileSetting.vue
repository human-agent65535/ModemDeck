<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { sessionState, setSessionProfileContact } from '../state/session'
import { contactsResource, loadContacts } from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import SettingsSaveStatus from './settings/SettingsSaveStatus.vue'

const { t } = useI18n()
const emit = defineEmits<{
  saved: []
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
    emit('saved')
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
  <section
    class="account-profile-setting"
    :aria-label="t('account.profileContact')"
  >
    <BaseAvatar
      :name="profileContact?.display_name || sessionState.username"
      :src="profileContact?.avatar"
      size="large"
      :fallback="profileContact ? 'initials' : 'person'"
      :palette-key="profileContact?.id || sessionState.userID"
    />
    <label class="field account-profile-setting__field">
      <span>{{ t('account.profileContact') }}</span>
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
      <small>{{ t('account.profileContactDescription') }}</small>
      <small v-if="profileNumber" class="account-profile-setting__number">
        {{ profileNumber }}
      </small>
    </label>
    <SettingsSaveStatus
      :status="profileMutation.status.value"
      :error="profileMutation.error.value"
    />
  </section>
</template>

<style scoped>
.account-profile-setting {
  display: grid;
  min-width: 0;
  align-items: center;
  gap: 14px;
  padding: 18px 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  border-bottom: 1px solid var(--border);
}

.account-profile-setting__field {
  min-width: 0;
}

.account-profile-setting__field small {
  margin-top: 3px;
  overflow: visible;
  color: var(--muted);
  font-size: 11px;
  text-overflow: clip;
  white-space: normal;
}

.account-profile-setting__number {
  color: var(--muted-strong);
}

@media (max-width: 620px) {
  .account-profile-setting {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .account-profile-setting > :deep(.settings-save-status) {
    grid-column: 2;
  }
}
</style>
