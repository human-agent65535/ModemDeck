<script setup lang="ts">
import { BookUser, LoaderCircle } from '@lucide/vue'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '../api/types'
import {
  readNativeIOSContacts,
  type NativeIOSContact
} from '../api/nativeIOS'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import { saveContact } from '../state/workspace'

defineProps<{
  disabled?: boolean
}>()

const { t } = useI18n()
const importing = ref(false)

function contactInput(contact: NativeIOSContact) {
  const seen = new Set<string>()
  const phones = contact.phones.flatMap(phone => {
    const number = phone.number.trim()
    const identity = number.replace(/[^+\d]/g, '')
    if (!number || !identity || seen.has(identity)) return []
    seen.add(identity)
    return [{
      label: phone.label.trim().slice(0, 64) || t('contacts.phone'),
      number,
      ...(phone.region?.trim() ? { region: phone.region.trim() } : {})
    }]
  })
  return {
    display_name: contact.displayName.trim().slice(0, 128),
    favorite: false,
    phones: phones.map((phone, index) => ({
      ...phone,
      primary: index === 0
    }))
  }
}

async function importContacts(): Promise<void> {
  if (importing.value) return
  importing.value = true
  try {
    const nativeContacts = (await readNativeIOSContacts())
      .map(contactInput)
      .filter(contact => contact.display_name && contact.phones.length > 0)
    if (nativeContacts.length === 0) {
      showError(t('contacts.importEmpty'))
      return
    }
    const confirmed = await requestConfirmation({
      title: t('contacts.importConfirmTitle'),
      message: t('contacts.importConfirmMessage', { count: nativeContacts.length }),
      confirmLabel: t('contacts.importFromIPhone')
    })
    if (!confirmed) return

    let cursor = 0
    let imported = 0
    let skipped = 0
    let failed = 0
    const worker = async () => {
      while (cursor < nativeContacts.length) {
        const contact = nativeContacts[cursor]
        cursor += 1
        if (!contact) continue
        try {
          await saveContact(contact)
          imported += 1
        } catch (cause) {
          if (cause instanceof ApiError && cause.code === 'phone_conflict') {
            skipped += 1
          } else {
            failed += 1
          }
        }
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(3, nativeContacts.length) }, () => worker())
    )
    const summary = t('contacts.importSummary', { imported, skipped, failed })
    if (failed > 0) showError(summary)
    else showSuccess(summary)
  } catch (cause) {
    const code = (cause as { code?: string } | undefined)?.code
    showError(
      code === 'CONTACTS_PERMISSION_DENIED'
        ? t('contacts.importPermissionDenied')
        : cause instanceof Error && cause.message
          ? cause.message
          : t('contacts.importFailed')
    )
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <button
    class="primary-button native-contact-import"
    type="button"
    :disabled="disabled || importing"
    @click="importContacts"
  >
    <LoaderCircle v-if="importing" class="spin" :size="16" />
    <BookUser v-else :size="16" />
    <span>{{ t('contacts.importFromIPhone') }}</span>
  </button>
</template>

<style scoped>
.native-contact-import {
  flex: 1 1 auto;
}
</style>
