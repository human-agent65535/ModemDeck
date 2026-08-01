<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { gateway } from '../api/client'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { sessionState, setSessionProfileContact } from '../state/session'
import { contactsResource, loadContacts } from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import SelectControl from './SelectControl.vue'
import SettingsPreferenceRow from './settings/SettingsPreferenceRow.vue'

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
const profileOptions = computed(() => [
  {
    value: '',
    label: t('account.noProfileContact')
  },
  ...contactsResource.data.map(contact => ({
    value: contact.id,
    label: contact.display_name,
    description:
      contact.phones.find(phone => phone.primary)?.number ||
      contact.phones[0]?.number ||
      ''
  }))
])

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

function changeProfile(contactID: string): void {
  selectedContactID.value = contactID
  void saveProfile()
}

onMounted(() => {
  void loadContacts()
})
</script>

<template>
  <SettingsPreferenceRow
    class="account-profile-setting"
    :title="t('account.profileContact')"
    title-id="account-profile-contact-title"
    :description="t('account.profileContactDescription')"
    control-size="wide"
    icon-tone="bare"
  >
    <template #icon>
      <BaseAvatar
        :name="profileContact?.display_name || sessionState.username"
        :src="profileContact?.avatar"
        size="small"
        :fallback="profileContact ? 'initials' : 'person'"
        :palette-key="profileContact?.id || sessionState.userID"
      />
    </template>
    <template #control>
      <SelectControl
        :model-value="selectedContactID"
        :options="profileOptions"
        :label="t('account.profileContact')"
        :disabled="saving || contactsResource.status === 'loading'"
        @change="changeProfile"
      />
    </template>
    <template v-if="profileNumber || profileMutation.error.value" #feedback>
      <small
        v-if="profileMutation.error.value"
        class="account-profile-setting__error"
        role="alert"
      >
        {{ profileMutation.error.value }}
      </small>
      <small v-else class="account-profile-setting__number">
        {{ profileNumber }}
      </small>
    </template>
  </SettingsPreferenceRow>
</template>

<style scoped>
.account-profile-setting__number {
  color: var(--muted-strong);
}

.account-profile-setting__error {
  color: var(--danger);
}
</style>
