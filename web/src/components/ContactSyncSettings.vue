<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CheckCircle2,
  Cloud,
  Download,
  ExternalLink,
  FileUp,
  Link2Off,
  LoaderCircle,
  RefreshCw,
  Save
} from '@lucide/vue'
import type { TransferContact } from '../utils/contactTransfer'
import {
  parseVCard,
  planContactImport,
  serializeContactsToVCard
} from '../utils/contactTransfer'
import {
  fetchGoogleContacts,
  requestGoogleContactsToken,
  revokeGoogleContactsToken,
  validGoogleClientID
} from '../utils/googleContacts'
import {
  bootstrapResource,
  contactsResource,
  loadBootstrap,
  loadContacts,
  saveContact
} from '../state/workspace'
import { useInitialLoadBarrier } from '../composables/useInitialLoadBarrier'
import { showSuccess } from '../state/feedback'
import SettingsLoadBoundary from './settings/SettingsLoadBoundary.vue'
import SettingsModuleCard from './settings/SettingsModuleCard.vue'

const GOOGLE_CLIENT_STORAGE_KEY = 'modemdeck.contacts.googleClientId'
const MAX_VCARD_BYTES = 5 * 1024 * 1024
const MAX_IMPORT_CONTACTS = 1000

const { t } = useI18n()
const clientID = ref('')
const googleToken = ref('')
const googleBusy = ref(false)
const googleError = ref('')
const googleNotice = ref('')
const vcardBusy = ref(false)
const vcardError = ref('')
const vcardNotice = ref('')
const vcardInput = ref<HTMLInputElement | null>(null)
const { loading: initialLoading, waitFor: waitForInitialLoad } = useInitialLoadBarrier()
const initialResourcesReady = computed(
  () =>
    bootstrapResource.status === 'ready' &&
    contactsResource.status === 'ready'
)

const connected = computed(() => Boolean(googleToken.value))
const clientIDValid = computed(() => validGoogleClientID(clientID.value))
const currentOrigin = computed(() =>
  typeof window === 'undefined' ? '' : window.location.origin
)
const defaultRegion = computed(() => {
  const bootstrap = bootstrapResource.data
  if (!bootstrap) return ''
  const defaultLineID = bootstrap.line_settings.default_line_id
  return (
    bootstrap.lines.find(line => line.id === defaultLineID)?.home_country_iso ||
    bootstrap.lines.find(line => line.home_country_iso)?.home_country_iso ||
    ''
  )
})

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

function saveGoogleClientID(notify = false): boolean {
  googleError.value = ''
  googleNotice.value = ''
  if (!clientIDValid.value) {
    googleError.value = t('contactSync.googleClientIDInvalid')
    return false
  }
  window.localStorage.setItem(GOOGLE_CLIENT_STORAGE_KEY, clientID.value.trim())
  if (notify) showSuccess(t('contactSync.googleClientIDSaved'))
  return true
}

async function importContacts(transfers: TransferContact[]): Promise<{
  created: number
  updated: number
  skipped: number
  failed: number
}> {
  await loadContacts(true)
  let created = 0
  let updated = 0
  let skipped = 0
  let failed = 0

  for (const transfer of transfers.slice(0, MAX_IMPORT_CONTACTS)) {
    const plan = planContactImport(
      transfer,
      contactsResource.data,
      defaultRegion.value
    )
    if (plan.conflict || !plan.input) {
      skipped += 1
      continue
    }
    try {
      await saveContact(plan.input, plan.existing?.id)
      if (plan.existing) updated += 1
      else created += 1
    } catch {
      failed += 1
    }
  }
  return { created, updated, skipped, failed }
}

function importSummary(result: {
  created: number
  updated: number
  skipped: number
  failed: number
}): string {
  return t('contactSync.importSummary', result)
}

async function syncGoogle(): Promise<void> {
  if (googleBusy.value) return
  if (!saveGoogleClientID()) return

  googleBusy.value = true
  googleError.value = ''
  googleNotice.value = ''
  try {
    const token = await requestGoogleContactsToken(
      clientID.value.trim(),
      googleToken.value ? '' : 'consent'
    )
    googleToken.value = token
    await loadBootstrap()
    const contacts = await fetchGoogleContacts(token, defaultRegion.value)
    if (contacts.length > MAX_IMPORT_CONTACTS) {
      throw new Error(t('contactSync.tooManyContacts', { count: MAX_IMPORT_CONTACTS }))
    }
    const result = await importContacts(contacts)
    googleNotice.value = importSummary(result)
  } catch (error) {
    googleError.value = errorMessage(error, t('contactSync.googleSyncFailed'))
  } finally {
    googleBusy.value = false
  }
}

async function disconnectGoogle(): Promise<void> {
  if (googleBusy.value) return
  googleBusy.value = true
  googleError.value = ''
  googleNotice.value = ''
  try {
    await revokeGoogleContactsToken(googleToken.value)
    googleToken.value = ''
    googleNotice.value = t('contactSync.googleDisconnected')
  } catch (error) {
    googleError.value = errorMessage(error, t('contactSync.googleDisconnectFailed'))
  } finally {
    googleBusy.value = false
  }
}

function chooseVCard(): void {
  if (!vcardBusy.value) vcardInput.value?.click()
}

async function importVCard(event: Event): Promise<void> {
  const input = event.currentTarget as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || vcardBusy.value) return

  vcardError.value = ''
  vcardNotice.value = ''
  if (file.size > MAX_VCARD_BYTES) {
    vcardError.value = t('contactSync.vcardTooLarge')
    return
  }

  vcardBusy.value = true
  try {
    await loadBootstrap()
    const transfers = parseVCard(await file.text(), defaultRegion.value)
    if (transfers.length === 0) {
      throw new Error(t('contactSync.vcardEmpty'))
    }
    if (transfers.length > MAX_IMPORT_CONTACTS) {
      throw new Error(t('contactSync.tooManyContacts', { count: MAX_IMPORT_CONTACTS }))
    }
    const result = await importContacts(transfers)
    vcardNotice.value = importSummary(result)
  } catch (error) {
    vcardError.value = errorMessage(error, t('contactSync.vcardImportFailed'))
  } finally {
    vcardBusy.value = false
  }
}

async function exportVCard(): Promise<void> {
  if (vcardBusy.value) return
  vcardBusy.value = true
  vcardError.value = ''
  vcardNotice.value = ''
  try {
    await loadContacts(true)
    if (contactsResource.data.length === 0) {
      throw new Error(t('contactSync.noContactsToExport'))
    }
    const blob = new Blob([serializeContactsToVCard(contactsResource.data)], {
      type: 'text/vcard;charset=utf-8'
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `modemdeck-contacts-${new Date().toISOString().slice(0, 10)}.vcf`
    link.click()
    URL.revokeObjectURL(url)
    vcardNotice.value = t('contactSync.exportedContacts', {
      count: contactsResource.data.length
    })
  } catch (error) {
    vcardError.value = errorMessage(error, t('contactSync.vcardExportFailed'))
  } finally {
    vcardBusy.value = false
  }
}

onMounted(() => {
  clientID.value = window.localStorage.getItem(GOOGLE_CLIENT_STORAGE_KEY) || ''
  void waitForInitialLoad([() => loadBootstrap(), () => loadContacts()])
})
</script>

<template>
  <SettingsLoadBoundary
    :loading="initialLoading && !initialResourcesReady"
    :loading-title="t('contacts.loading')"
  >
  <section class="contact-sync-settings" :aria-label="t('contactSync.title')">
    <SettingsModuleCard
      :title="t('contactSync.googleTitle')"
      title-id="contact-sync-google-title"
      :description="t('contactSync.googleDescription')"
      icon-tone="blue"
    >
      <template #icon>
        <Cloud :size="21" />
      </template>
      <template #status>
        <span class="contact-sync-status" :class="{ 'is-connected': connected }">
          {{ connected ? t('contactSync.connected') : t('contactSync.notConnected') }}
        </span>
      </template>

      <div class="contact-sync-module">
        <label class="field contact-sync-client">
          <span>{{ t('contactSync.googleClientID') }}</span>
          <div class="contact-sync-client__control">
            <input
              v-model.trim="clientID"
              type="text"
              inputmode="url"
              autocomplete="off"
              spellcheck="false"
              placeholder="000000000000-example.apps.googleusercontent.com"
              :disabled="googleBusy || connected"
              @input="googleError = ''; googleNotice = ''"
            />
            <button
              class="secondary-button"
              type="button"
              :disabled="googleBusy || connected || !clientIDValid"
              @click="saveGoogleClientID(true)"
            >
              <Save :size="15" />
              <span>{{ t('common.save') }}</span>
            </button>
          </div>
        </label>

        <p class="contact-sync-help">
          {{ t('contactSync.googleOriginHelp') }}
          <code>{{ currentOrigin }}</code>
          <a
            href="https://console.cloud.google.com/apis/credentials"
            target="_blank"
            rel="noreferrer"
          >
            {{ t('contactSync.openGoogleConsole') }}
            <ExternalLink :size="13" />
          </a>
        </p>

        <div class="contact-sync-direction">
          <strong>{{ t('contactSync.syncDirection') }}</strong>
          <span>{{ t('contactSync.googleToModemDeck') }}</span>
          <small>{{ t('contactSync.manualSyncOnly') }}</small>
        </div>

        <p v-if="googleError" class="field-error" role="alert">{{ googleError }}</p>
        <p v-if="googleNotice" class="contact-sync-notice" role="status">
          <CheckCircle2 :size="16" />
          <span>{{ googleNotice }}</span>
        </p>

        <div class="contact-sync-actions">
          <button
            class="primary-button"
            type="button"
            :disabled="googleBusy || !clientIDValid"
            @click="syncGoogle"
          >
            <LoaderCircle v-if="googleBusy" class="spin" :size="16" />
            <RefreshCw v-else :size="16" />
            <span>
              {{
                connected
                  ? t('contactSync.syncNow')
                  : t('contactSync.connectAndSync')
              }}
            </span>
          </button>
          <button
            v-if="connected"
            class="secondary-button"
            type="button"
            :disabled="googleBusy"
            @click="disconnectGoogle"
          >
            <Link2Off :size="16" />
            <span>{{ t('contactSync.disconnect') }}</span>
          </button>
        </div>
      </div>
    </SettingsModuleCard>

    <SettingsModuleCard
      :title="t('contactSync.appleTitle')"
      title-id="contact-sync-apple-title"
      :description="t('contactSync.appleDescription')"
      icon-tone="neutral"
    >
      <template #icon>
        <Download :size="21" />
      </template>

      <div class="contact-sync-module">
        <div class="contact-sync-direction">
          <strong>{{ t('contactSync.vcardFormat') }}</strong>
          <span>{{ t('contactSync.vcardCompatibility') }}</span>
          <small>{{ t('contactSync.appleNoLiveSync') }}</small>
        </div>

        <p v-if="vcardError" class="field-error" role="alert">{{ vcardError }}</p>
        <p v-if="vcardNotice" class="contact-sync-notice" role="status">
          <CheckCircle2 :size="16" />
          <span>{{ vcardNotice }}</span>
        </p>

        <div class="contact-sync-actions">
          <button
            class="primary-button"
            type="button"
            :disabled="vcardBusy"
            @click="chooseVCard"
          >
            <LoaderCircle v-if="vcardBusy" class="spin" :size="16" />
            <FileUp v-else :size="16" />
            <span>{{ t('contactSync.importVCard') }}</span>
          </button>
          <button
            class="secondary-button"
            type="button"
            :disabled="vcardBusy"
            @click="exportVCard"
          >
            <Download :size="16" />
            <span>{{ t('contactSync.exportVCard') }}</span>
          </button>
          <input
            ref="vcardInput"
            class="contact-sync-file-input"
            type="file"
            accept=".vcf,.vcard,text/vcard,text/x-vcard"
            @change="importVCard"
          />
        </div>
      </div>
    </SettingsModuleCard>
  </section>
  </SettingsLoadBoundary>
</template>

<style scoped>
.contact-sync-settings {
  display: grid;
  width: 100%;
  gap: 18px;
}

.contact-sync-help,
.contact-sync-direction small {
  margin: 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.55;
}

.contact-sync-status {
  padding: 5px 8px;
  color: var(--muted);
  background: var(--surface-hover);
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
}

.contact-sync-status.is-connected {
  color: var(--accent);
  background: var(--accent-soft);
}

.contact-sync-module {
  display: grid;
  gap: 15px;
}

.contact-sync-client {
  max-width: 680px;
}

.contact-sync-client__control {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}

.contact-sync-client__control input {
  min-width: 0;
}

.contact-sync-help {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px;
}

.contact-sync-help code {
  padding: 2px 5px;
  color: var(--text);
  background: var(--surface-hover);
  border-radius: 4px;
}

.contact-sync-help a {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--blue);
  font-weight: 700;
}

.contact-sync-direction {
  display: grid;
  max-width: 680px;
  grid-template-columns: minmax(112px, max-content) minmax(0, 1fr);
  gap: 4px 16px;
  padding: 12px 14px;
  text-align: left;
  background: var(--surface-hover);
  border-radius: 9px;
}

.contact-sync-direction strong,
.contact-sync-direction span {
  font-size: 13px;
}

.contact-sync-direction small {
  grid-column: 2;
}

.contact-sync-actions,
.contact-sync-notice {
  display: flex;
  align-items: center;
  gap: 8px;
}

.contact-sync-actions {
  flex-wrap: wrap;
}

.contact-sync-notice {
  margin: 0;
  color: var(--accent);
  font-size: 12px;
}

.contact-sync-file-input {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  white-space: nowrap;
}

@media (max-width: 860px) {
  .contact-sync-status {
    margin-left: 51px;
  }

  .contact-sync-client__control,
  .contact-sync-direction {
    grid-template-columns: 1fr;
  }

  .contact-sync-direction small {
    grid-column: 1;
  }

  .contact-sync-actions > button {
    flex: 1 1 auto;
  }
}
</style>
