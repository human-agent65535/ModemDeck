<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  Download,
  FileText,
  KeyRound,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
  Upload
} from '@lucide/vue'
import { gateway } from '../api/client'
import { tlsCAPath } from '../api/contract'
import type { TLSSettings } from '../api/types'
import { ApiError } from '../api/types'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import FileDropControl from './FileDropControl.vue'
import CertificateFacts from './CertificateFacts.vue'
import SettingsLoadBoundary from './settings/SettingsLoadBoundary.vue'

const MAX_PEM_BYTES = 1024 * 1024
const PEM_ACCEPT =
  '.pem,.crt,.cer,.key,application/x-pem-file,application/pem-certificate-chain,text/plain'
const { t } = useI18n()

const settings = ref<TLSSettings | null>(null)
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const operationError = ref('')

const certificateFile = ref<File | null>(null)
const privateKeyFile = ref<File | null>(null)
const certificateError = ref('')
const privateKeyError = ref('')

const modeLabel = computed(() =>
  settings.value?.mode === 'automatic'
    ? t('tls.automaticCertificate')
    : t('tls.userCertificate')
)
const renewalLabel = computed(() =>
  settings.value?.renews_automatically
    ? t('tls.automaticRenewal')
    : t('tls.noAutomaticRenewal')
)

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  return `${Math.ceil(bytes / 1024)} KiB`
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 403) {
    return t('tls.updateForbidden')
  }
  return error instanceof Error && error.message ? error.message : fallback
}

async function loadTLSSettings(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    settings.value = await gateway.getTLSSettings()
  } catch (error) {
    loadError.value = errorMessage(error, t('tls.loadFailed'))
  } finally {
    loading.value = false
  }
}

function handleFileSelection(kind: 'certificate' | 'private-key', file: File): void {
  const tooLarge = file.size > MAX_PEM_BYTES

  operationError.value = ''
  if (kind === 'certificate') {
    certificateFile.value = tooLarge ? null : file
    certificateError.value = tooLarge ? t('tls.certificateTooLarge') : ''
  } else {
    privateKeyFile.value = tooLarge ? null : file
    privateKeyError.value = tooLarge ? t('tls.privateKeyTooLarge') : ''
  }
}

async function readPEM(file: File, kind: 'certificate' | 'private-key'): Promise<string> {
  if (file.size > MAX_PEM_BYTES) {
    throw new Error(
      kind === 'certificate' ? t('tls.certificateTooLarge') : t('tls.privateKeyTooLarge')
    )
  }

  let text: string
  try {
    text = await file.text()
  } catch {
    throw new Error(
      kind === 'certificate' ? t('tls.readCertificateFailed') : t('tls.readPrivateKeyFailed')
    )
  }
  if (!text.trim() || text.includes('\0') || text.includes('\uFFFD')) {
    throw new Error(
      kind === 'certificate' ? t('tls.certificateMustBePEM') : t('tls.privateKeyMustBePEM')
    )
  }
  const validPEM =
    kind === 'certificate'
      ? /-----BEGIN CERTIFICATE-----[\s\S]+-----END CERTIFICATE-----/.test(text)
      : /-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----[\s\S]+-----END (?:[A-Z0-9]+ )*PRIVATE KEY-----/.test(
          text
        )
  if (!validPEM) {
    throw new Error(
      kind === 'certificate' ? t('tls.certificateInvalidPEM') : t('tls.privateKeyInvalidPEM')
    )
  }
  return text
}

function clearSelectedFiles(): void {
  certificateFile.value = null
  privateKeyFile.value = null
  certificateError.value = ''
  privateKeyError.value = ''
}

async function installUserCertificate(): Promise<void> {
  operationError.value = ''
  certificateError.value = certificateFile.value ? '' : t('tls.selectCertificate')
  privateKeyError.value = privateKeyFile.value ? '' : t('tls.selectPrivateKey')
  if (!certificateFile.value || !privateKeyFile.value || saving.value) return

  saving.value = true
  try {
    let certificatePEM = ''
    let privateKeyPEM = ''
    try {
      certificatePEM = await readPEM(certificateFile.value, 'certificate')
    } catch (error) {
      certificateError.value = errorMessage(error, t('tls.readCertificateFailed'))
    }
    try {
      privateKeyPEM = await readPEM(privateKeyFile.value, 'private-key')
    } catch (error) {
      privateKeyError.value = errorMessage(error, t('tls.readPrivateKeyFailed'))
    }
    if (certificateError.value || privateKeyError.value) return

    settings.value = await gateway.updateTLSSettings({
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    })
    clearSelectedFiles()
    showSuccess(t('tls.savedNotice'))
  } catch (error) {
    operationError.value = errorMessage(error, t('tls.installFailed'))
    showError(operationError.value)
  } finally {
    saving.value = false
  }
}

async function useAutomaticCertificate(): Promise<void> {
  if (saving.value) return
  const confirmed = await requestConfirmation({
    title: t('tls.useAutomaticTitle'),
    message: t('tls.useAutomaticMessage'),
    confirmLabel: t('tls.useAutomatic')
  })
  if (!confirmed) return

  saving.value = true
  operationError.value = ''
  try {
    settings.value = await gateway.updateTLSSettings({ operation: 'use_automatic' })
    showSuccess(t('tls.savedNotice'))
  } catch (error) {
    operationError.value = errorMessage(error, t('tls.switchFailed'))
    showError(operationError.value)
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadTLSSettings()
})
</script>

<template>
  <section class="tls-settings" aria-labelledby="tls-settings-title">
    <SettingsLoadBoundary
      :loading="loading"
      :error="Boolean(loadError)"
      :loading-title="t('tls.loading')"
      loading-shape="detail-form"
      :error-title="t('tls.loadFailed')"
      :detail="loadError"
      retryable
      @retry="loadTLSSettings"
    >
    <template v-if="settings">
      <header class="tls-summary">
        <span class="tls-summary__icon"><ShieldCheck :size="21" /></span>
        <div class="tls-summary__heading">
          <h3 id="tls-settings-title">{{ t('tls.currentCertificate') }}</h3>
          <p>HTTPS :7577 · {{ modeLabel }} · {{ renewalLabel }}</p>
        </div>
        <div class="tls-summary__status" :aria-label="t('tls.certificateStatus')">
          <span class="status-label" :class="{ 'status-label--danger': settings.expired }">
            {{ settings.expired ? t('tls.expired') : t('tls.valid') }}
          </span>
          <span class="status-label status-label--neutral">{{ modeLabel }}</span>
        </div>
      </header>

      <p
        v-if="settings.mode === 'user' && settings.expired"
        class="tls-expired-warning"
        role="alert"
      >
        <AlertTriangle :size="16" />
        <span>{{ t('tls.expiredWarning') }}</span>
      </p>

      <CertificateFacts
        :not-before="settings.not_before"
        :not-after="settings.not_after"
        :subject="settings.subject"
        :issuer="settings.issuer"
        :dns-names="settings.dns_names"
        :ip-addresses="settings.ip_addresses"
        :fingerprint="settings.fingerprint_sha256"
      />

      <div v-if="settings.mode === 'automatic'" class="tls-ca-download">
        <a class="secondary-button" :href="tlsCAPath">
          <Download :size="16" />
          <span>{{ t('tls.downloadRoot') }}</span>
        </a>
      </div>

      <section class="tls-install" aria-labelledby="tls-install-title">
        <header>
          <div>
            <h3 id="tls-install-title" class="tls-install__title">
              {{ t('tls.installUser') }}
            </h3>
            <p>{{ t('tls.pemHint') }}</p>
          </div>
          <button
            v-if="settings.mode === 'user'"
            class="secondary-button"
            type="button"
            :disabled="saving"
            @click="useAutomaticCertificate"
          >
            <RefreshCw :size="16" />
            <span>{{ t('tls.useAutomatic') }}</span>
          </button>
        </header>

        <form @submit.prevent="installUserCertificate">
          <div class="tls-file-grid">
            <FileDropControl
              :file="certificateFile"
              :label="t('tls.certificatePEM')"
              :prompt="t('tls.chooseCertificate')"
              :description="
                certificateFile
                  ? formatFileSize(certificateFile.size)
                  : t('tls.certificateChain')
              "
              :accept="PEM_ACCEPT"
              :disabled="saving"
              :error="certificateError"
              @select="handleFileSelection('certificate', $event)"
            >
              <template #icon>
                <FileText :size="18" />
              </template>
            </FileDropControl>

            <FileDropControl
              :file="privateKeyFile"
              :label="t('tls.privateKeyPEM')"
              :prompt="t('tls.choosePrivateKey')"
              :description="
                privateKeyFile
                  ? formatFileSize(privateKeyFile.size)
                  : t('tls.matchingPrivateKey')
              "
              :accept="PEM_ACCEPT"
              :disabled="saving"
              :error="privateKeyError"
              @select="handleFileSelection('private-key', $event)"
            >
              <template #icon>
                <KeyRound :size="18" />
              </template>
            </FileDropControl>
          </div>

          <footer class="tls-install__actions">
            <div class="tls-install__feedback">
              <p v-if="operationError" class="field-error" role="alert">
                {{ operationError }}
              </p>
            </div>
            <button
              class="primary-button"
              type="submit"
              :disabled="saving || !certificateFile || !privateKeyFile"
            >
              <LoaderCircle v-if="saving" class="spin" :size="17" />
              <Upload v-else :size="17" />
              <span>{{ t('tls.install') }}</span>
            </button>
          </footer>
        </form>
      </section>
    </template>
    </SettingsLoadBoundary>
  </section>
</template>
