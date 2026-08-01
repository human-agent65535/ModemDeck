<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, LoaderCircle, ShieldCheck, Trash2 } from '@lucide/vue'
import { gateway } from '../api/client'
import type { CloudflareOriginTLSStatus } from '../api/types'
import { ApiError } from '../api/types'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { requestConfirmation } from '../state/confirmation'
import CertificateFacts from './CertificateFacts.vue'
import SettingsModuleCard from './settings/SettingsModuleCard.vue'
import TextareaControl from './TextareaControl.vue'

const MAX_PEM_BYTES = 1024 * 1024

const props = withDefaults(
  defineProps<{
    status: CloudflareOriginTLSStatus
    headingLevel?: 3 | 4
  }>(),
  {
    headingLevel: 3
  }
)
const emit = defineEmits<{
  updated: [status: CloudflareOriginTLSStatus]
}>()
const { t } = useI18n()

const certificatePEM = ref('')
const privateKeyPEM = ref('')
const certificateError = ref('')
const privateKeyError = ref('')
const hasPEMInput = computed(
  () => Boolean(certificatePEM.value.trim()) && Boolean(privateKeyPEM.value.trim())
)
const contentHeadingTag = computed(() => `h${props.headingLevel + 1}`)

const certificateHealthy = computed(
  () => props.status.enabled && !props.status.expired && props.status.covers_routes
)
const statusLabel = computed(() => {
  if (!props.status.enabled) return t('originTLS.disabled')
  if (props.status.expired) return t('originTLS.expired')
  if (!props.status.covers_routes) return t('originTLS.hostnameMismatch')
  return t('tls.valid')
})
const mutation = useSettingsMutation({
  errorMessage: cause => originTLSError(cause),
  successMessage: () => t('originTLS.saved'),
  toast: 'both'
})

function originTLSError(cause: unknown): string {
  if (cause instanceof ApiError) {
    if (cause.status === 403) return t('originTLS.forbidden')
    if (cause.code === 'invalid_cloudflare_origin_certificate') {
      return t('originTLS.invalidCertificate')
    }
    if (cause.code === 'cloudflare_origin_tls_activation_failed') {
      return t('originTLS.activationFailed')
    }
    if (cause.code === 'cloudflare_origin_tls_already_enabled') {
      return t('originTLS.alreadyEnabled')
    }
  }
  return cause instanceof Error && cause.message
    ? cause.message
    : t('originTLS.saveFailed')
}

function handlePEMInput(kind: 'certificate' | 'private-key'): void {
  mutation.reset()
  if (kind === 'certificate') {
    certificateError.value = ''
  } else {
    privateKeyError.value = ''
  }
}

function validatePEM(
  value: string,
  kind: 'certificate' | 'private-key'
): string {
  const text = value.trim()
  if (new TextEncoder().encode(text).byteLength > MAX_PEM_BYTES) {
    throw new Error(
      kind === 'certificate'
        ? t('tls.certificateTooLarge')
        : t('tls.privateKeyTooLarge')
    )
  }
  const valid =
    kind === 'certificate'
      ? /-----BEGIN CERTIFICATE-----[\s\S]+-----END CERTIFICATE-----/.test(text)
      : /-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----[\s\S]+-----END (?:[A-Z0-9]+ )*PRIVATE KEY-----/.test(
          text
        )
  if (!text.trim() || text.includes('\0') || text.includes('\uFFFD') || !valid) {
    throw new Error(
      kind === 'certificate'
        ? t('tls.certificateInvalidPEM')
        : t('tls.privateKeyInvalidPEM')
    )
  }
  return `${text}\n`
}

function clearPEM(): void {
  certificatePEM.value = ''
  privateKeyPEM.value = ''
  certificateError.value = ''
  privateKeyError.value = ''
}

async function install(): Promise<void> {
  if (!hasPEMInput.value || mutation.saving.value) {
    return
  }
  certificateError.value = ''
  privateKeyError.value = ''
  let validatedCertificate = ''
  let validatedPrivateKey = ''
  try {
    validatedCertificate = validatePEM(certificatePEM.value, 'certificate')
  } catch (cause) {
    certificateError.value = originTLSError(cause)
  }
  try {
    validatedPrivateKey = validatePEM(privateKeyPEM.value, 'private-key')
  } catch (cause) {
    privateKeyError.value = originTLSError(cause)
  }
  if (certificateError.value || privateKeyError.value) return

  const result = await mutation.run(() =>
    gateway.installCloudflareOriginTLS({
      certificate_pem: validatedCertificate,
      private_key_pem: validatedPrivateKey
    })
  )
  if (!result.ok) return
  clearPEM()
  emit('updated', result.value)
}

async function deleteCertificate(): Promise<void> {
  if (!props.status.enabled || mutation.saving.value) return
  const confirmed = await requestConfirmation({
    title: t('originTLS.deleteTitle'),
    message: t('originTLS.deleteMessage'),
    confirmLabel: t('originTLS.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  const result = await mutation.run(() => gateway.disableCloudflareOriginTLS())
  if (!result.ok) return
  clearPEM()
  emit('updated', result.value)
}
</script>

<template>
  <SettingsModuleCard
    class="origin-tls-card"
    :title="t('originTLS.title')"
    title-id="cloudflare-origin-tls-title"
    :description="t('originTLS.description')"
    surface="subtle"
    :heading-level="headingLevel"
    has-body
  >
    <template #icon>
      <ShieldCheck :size="19" />
    </template>
    <template #status>
      <span
        class="origin-status"
        :class="{
          'is-active': certificateHealthy,
          'is-blocked': status.enabled && !certificateHealthy
        }"
      >
        {{ statusLabel }}
      </span>
    </template>

    <template v-if="status.enabled">
      <CertificateFacts
        :not-before="status.not_before"
        :not-after="status.not_after"
        :subject="status.subject"
        :issuer="status.issuer"
        :dns-names="status.dns_names"
        :fingerprint="status.fingerprint_sha256"
      />

      <footer class="origin-installed-actions">
        <p v-if="mutation.error.value" class="field-error" role="alert">
          {{ mutation.error.value }}
        </p>
        <button
          class="danger-button"
          type="button"
          :disabled="mutation.saving.value"
          @click="deleteCertificate"
        >
          <LoaderCircle v-if="mutation.saving.value" class="spin" :size="16" />
          <Trash2 v-else :size="16" />
          {{ t('originTLS.delete') }}
        </button>
      </footer>
    </template>

    <form v-else class="tls-install origin-install" @submit.prevent="install">
      <header>
        <div>
          <component :is="contentHeadingTag" class="tls-install__title">
            {{ t('originTLS.installTitle') }}
          </component>
          <p>{{ t('originTLS.pasteHint') }}</p>
        </div>
      </header>

      <div class="origin-pem-grid">
        <TextareaControl
          v-model="certificatePEM"
          :label="t('tls.certificatePEM')"
          :description="t('tls.certificateChain')"
          :error="certificateError"
          :rows="8"
          wrap="off"
          monospace
          :spellcheck="false"
          :max-length="MAX_PEM_BYTES"
          :disabled="mutation.saving.value"
          placeholder="-----BEGIN CERTIFICATE-----"
          @update:model-value="handlePEMInput('certificate')"
        />

        <TextareaControl
          v-model="privateKeyPEM"
          :label="t('tls.privateKeyPEM')"
          :description="t('tls.matchingPrivateKey')"
          :error="privateKeyError"
          :rows="8"
          wrap="off"
          monospace
          sensitive
          :spellcheck="false"
          :max-length="MAX_PEM_BYTES"
          :disabled="mutation.saving.value"
          placeholder="-----BEGIN PRIVATE KEY-----"
          @update:model-value="handlePEMInput('private-key')"
        />
      </div>

      <footer class="tls-install__actions">
        <p v-if="mutation.error.value" class="field-error" role="alert">
          {{ mutation.error.value }}
        </p>
        <button
          class="primary-button"
          type="submit"
          :disabled="mutation.saving.value || !hasPEMInput"
        >
          <LoaderCircle v-if="mutation.saving.value" class="spin" :size="17" />
          <Check v-else :size="17" />
          {{ t('originTLS.enable') }}
        </button>
      </footer>
    </form>
  </SettingsModuleCard>
</template>

<style scoped>
.origin-status {
  min-height: 26px;
  flex: 0 0 auto;
  padding: 4px 8px;
  color: var(--muted);
  font-size: 11px;
  font-weight: 700;
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.origin-status.is-active {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: var(--success-border);
}

.origin-status.is-blocked {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: var(--danger-border);
}

.origin-tls-card :deep(.settings-module-card__header) {
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}

.origin-tls-card :deep(.settings-module-card__body) {
  margin-top: 0;
}

.origin-install {
  margin-top: 0;
  border-top: 0;
}

.origin-install .tls-install__actions {
  min-height: 38px;
}

.origin-installed-actions {
  display: flex;
  min-height: 70px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding-top: 18px;
  border-top: 1px solid var(--border);
}

.origin-installed-actions .field-error {
  margin: 0;
}

.origin-installed-actions .danger-button {
  min-height: 40px;
  padding: 0 12px;
  background: var(--surface);
  border: 1px solid var(--danger-border);
}

.origin-pem-grid {
  display: grid;
  gap: 14px;
}

@media (max-width: 560px) {
  .origin-installed-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .origin-installed-actions .danger-button {
    width: 100%;
  }
}
</style>
