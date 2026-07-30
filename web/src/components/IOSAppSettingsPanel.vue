<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Check,
  Clipboard,
  KeyRound,
  LoaderCircle,
  QrCode,
  RefreshCw,
  ShieldCheck,
  Smartphone,
  Trash2,
  X
} from '@lucide/vue'
import QRCode from 'qrcode'
import { gateway } from '../api/client'
import type { IOSPairingStatus } from '../api/types'
import { ApiError } from '../api/types'
import { requestConfirmation } from '../state/confirmation'
import { sessionState } from '../state/session'

const { t, locale } = useI18n()
const isAdmin = computed(() => sessionState.role === 'admin')
const loading = ref(true)
const loadError = ref('')
const pairing = ref<IOSPairingStatus | null>(null)
const pairingPending = ref(false)
const pairingError = ref('')
const qrDataURL = ref('')
const pairingPayloadJSON = ref('')
const qrDialog = ref<HTMLElement | null>(null)
const qrCloseButton = ref<HTMLButtonElement | null>(null)
const copied = ref(false)

const pairingReady = computed(
  () =>
    pairing.value?.allowed &&
    pairing.value.cloudflare.enabled &&
    pairing.value.cloudflare.connected
)

function errorMessage(cause: unknown, fallback: string): string {
  if (cause instanceof ApiError) {
    if (cause.code === 'cloudflare_required') {
      return t('iosPairing.cloudflareRequired')
    }
    if (cause.code === 'cloudflare_unavailable') {
      return t('iosPairing.cloudflareUnavailable')
    }
    if (cause.code === 'ios_pairing_not_allowed') {
      return t('iosPairing.permissionRequired')
    }
  }
  return cause instanceof Error && cause.message ? cause.message : fallback
}

function formatTimestamp(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(date)
}

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    pairing.value = (await gateway.getIOSPairing()).pairing
  } catch (cause) {
    loadError.value = errorMessage(cause, t('iosPairing.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function createPairing(): Promise<void> {
  if (!pairingReady.value || pairingPending.value) return
  if (pairing.value?.has_credential) {
    const confirmed = await requestConfirmation({
      title: t('iosPairing.replaceTitle'),
      message: t('iosPairing.replaceMessage'),
      confirmLabel: t('iosPairing.replaceConfirm')
    })
    if (!confirmed) return
  }

  pairingPending.value = true
  pairingError.value = ''
  try {
    const result = await gateway.createIOSPairing()
    if (!result.payload) throw new Error(t('iosPairing.invalidPayload'))
    pairing.value = result.pairing
    pairingPayloadJSON.value = JSON.stringify(result.payload)
    qrDataURL.value = await QRCode.toDataURL(pairingPayloadJSON.value, {
      width: 320,
      margin: 2,
      errorCorrectionLevel: 'M',
      color: {
        dark: '#16211f',
        light: '#ffffff'
      }
    })
    copied.value = false
    await nextTick()
    qrCloseButton.value?.focus()
  } catch (cause) {
    closeQR()
    pairingError.value = errorMessage(cause, t('iosPairing.createFailed'))
  } finally {
    pairingPending.value = false
  }
}

async function revokePairing(): Promise<void> {
  if (!pairing.value?.has_credential || pairingPending.value) return
  const confirmed = await requestConfirmation({
    title: t('iosPairing.revokeTitle'),
    message: t('iosPairing.revokeMessage'),
    confirmLabel: t('iosPairing.revokeConfirm'),
    tone: 'danger'
  })
  if (!confirmed) return

  pairingPending.value = true
  pairingError.value = ''
  try {
    await gateway.revokeIOSPairing()
    pairing.value = {
      ...pairing.value,
      has_credential: false,
      credential_created_at: undefined
    }
    closeQR()
  } catch (cause) {
    pairingError.value = errorMessage(cause, t('iosPairing.revokeFailed'))
  } finally {
    pairingPending.value = false
  }
}

async function copyPairingPayload(): Promise<void> {
  if (!pairingPayloadJSON.value) return
  try {
    await navigator.clipboard.writeText(pairingPayloadJSON.value)
    copied.value = true
  } catch {
    pairingError.value = t('iosPairing.copyFailed')
  }
}

function closeQR(): void {
  qrDataURL.value = ''
  pairingPayloadJSON.value = ''
  copied.value = false
}

function onQRKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    closeQR()
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <section class="ios-settings" aria-labelledby="ios-settings-title">
    <div v-if="loading" class="ios-settings__state" role="status">
      <LoaderCircle class="spin" :size="18" />
      <span>{{ t('iosPairing.loading') }}</span>
    </div>

    <div
      v-else-if="loadError"
      class="ios-settings__state ios-settings__state--error"
      role="alert"
    >
      <span>{{ loadError }}</span>
      <button class="secondary-button" type="button" @click="load">
        <RefreshCw :size="15" />
        {{ t('common.retry') }}
      </button>
    </div>

    <template v-else-if="pairing">
      <header class="ios-summary">
        <span class="ios-summary__icon"><Smartphone :size="22" /></span>
        <div>
          <h3 id="ios-settings-title">{{ t('iosPairing.title') }}</h3>
          <p>{{ t('iosPairing.description') }}</p>
        </div>
      </header>

      <section
        v-if="isAdmin"
        class="ios-card"
        aria-labelledby="ios-tunnel-title"
      >
        <header>
          <span><ShieldCheck :size="19" /></span>
          <div>
            <h4 id="ios-tunnel-title">{{ t('iosPairing.tunnelTitle') }}</h4>
            <p>{{ t('iosPairing.tunnelDescription') }}</p>
          </div>
          <span
            class="ios-status"
            :class="{
              'is-active': pairing.cloudflare.connected,
              'is-blocked': !pairing.cloudflare.enabled
            }"
          >
            {{
              !pairing.cloudflare.enabled
                ? t('iosPairing.notInstalled')
                : pairing.cloudflare.connected
                  ? t('iosPairing.connected')
                  : t('iosPairing.disconnected')
            }}
          </span>
        </header>
        <dl v-if="pairing.cloudflare.enabled" class="ios-pairing-facts">
          <div>
            <dt>{{ t('iosPairing.publicURL') }}</dt>
            <dd><code>{{ pairing.cloudflare.public_url }}</code></dd>
          </div>
          <div>
            <dt>{{ t('iosPairing.origin') }}</dt>
            <dd>
              <code>http://modemdeck:7575 → http://api:8080</code>
            </dd>
          </div>
        </dl>
        <div v-if="!pairing.cloudflare.enabled" class="ios-notice">
          {{ t('iosPairing.cloudflareRequired') }}
        </div>
        <div v-else-if="!pairing.cloudflare.connected" class="ios-notice">
          {{ t('iosPairing.cloudflareUnavailable') }}
        </div>
        <p class="ios-pairing-note">{{ t('iosPairing.installManaged') }}</p>
      </section>

      <section class="ios-card" aria-labelledby="ios-pairing-title">
        <header>
          <span><KeyRound :size="19" /></span>
          <div>
            <h4 id="ios-pairing-title">{{ t('iosPairing.yourDevice') }}</h4>
            <p>{{ t('iosPairing.yourDeviceDescription') }}</p>
          </div>
          <span
            class="ios-status"
            :class="{
              'is-active':
                pairing.has_credential && pairing.cloudflare.connected,
              'is-blocked':
                !pairing.allowed || !pairing.cloudflare.enabled
            }"
          >
            {{
              !pairing.allowed
                ? t('iosPairing.notAllowed')
                : !pairing.cloudflare.enabled
                  ? t('iosPairing.notInstalled')
                  : !pairing.cloudflare.connected
                    ? t('iosPairing.disconnected')
                    : pairing.has_credential
                      ? t('iosPairing.paired')
                      : t('iosPairing.notPaired')
            }}
          </span>
        </header>

        <div v-if="!pairing.allowed" class="ios-notice">
          {{ t('iosPairing.permissionRequired') }}
        </div>
        <div v-else-if="!pairing.cloudflare.enabled" class="ios-notice">
          {{ t('iosPairing.cloudflareRequired') }}
        </div>
        <div v-else-if="!pairing.cloudflare.connected" class="ios-notice">
          {{ t('iosPairing.cloudflareUnavailable') }}
        </div>
        <template v-if="pairing.allowed">
          <dl class="ios-pairing-facts">
            <div v-if="pairing.cloudflare.public_url">
              <dt>{{ t('iosPairing.connection') }}</dt>
              <dd><code>{{ pairing.cloudflare.public_url }}</code></dd>
            </div>
            <div v-if="pairing.credential_created_at">
              <dt>{{ t('iosPairing.createdAt') }}</dt>
              <dd>{{ formatTimestamp(pairing.credential_created_at) }}</dd>
            </div>
          </dl>
          <p class="ios-pairing-note">{{ t('iosPairing.noSwitching') }}</p>
          <p class="ios-pairing-note">{{ t('iosPairing.noExpiry') }}</p>
          <div class="ios-pairing-actions">
            <button
              v-if="pairingReady"
              class="primary-button"
              type="button"
              :disabled="pairingPending || !pairingReady"
              @click="createPairing"
            >
              <LoaderCircle v-if="pairingPending" class="spin" :size="16" />
              <QrCode v-else :size="16" />
              {{
                pairing.has_credential
                  ? t('iosPairing.replaceQR')
                  : t('iosPairing.generateQR')
              }}
            </button>
            <button
              v-if="pairing.has_credential"
              class="danger-button"
              type="button"
              :disabled="pairingPending"
              @click="revokePairing"
            >
              <Trash2 :size="16" />
              {{ t('iosPairing.revoke') }}
            </button>
          </div>
        </template>
        <p v-if="pairingError" class="field-error" role="alert">
          {{ pairingError }}
        </p>
      </section>
    </template>

    <Teleport to="body">
      <div
        v-if="qrDataURL"
        class="ios-qr-backdrop"
        role="presentation"
        @mousedown.self="closeQR"
      >
        <section
          ref="qrDialog"
          class="ios-qr-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="ios-qr-title"
          tabindex="-1"
          @keydown="onQRKeydown"
        >
          <header>
            <div>
              <h2 id="ios-qr-title">{{ t('iosPairing.scanTitle') }}</h2>
              <p>{{ t('iosPairing.scanDescription') }}</p>
            </div>
            <button
              ref="qrCloseButton"
              class="icon-button"
              type="button"
              :aria-label="t('common.close')"
              @click="closeQR"
            >
              <X :size="19" />
            </button>
          </header>
          <div class="ios-qr-image">
            <img :src="qrDataURL" :alt="t('iosPairing.qrAlt')" />
          </div>
          <p class="ios-qr-warning">{{ t('iosPairing.showOnce') }}</p>
          <footer>
            <button
              class="secondary-button"
              type="button"
              @click="copyPairingPayload"
            >
              <Check v-if="copied" :size="16" />
              <Clipboard v-else :size="16" />
              {{
                copied
                  ? t('iosPairing.copied')
                  : t('iosPairing.copyPairingData')
              }}
            </button>
            <button class="primary-button" type="button" @click="closeQR">
              {{ t('common.done') }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.ios-settings {
  max-width: 760px;
}

.ios-settings__state {
  display: flex;
  min-height: 180px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
  font-size: 12px;
}

.ios-settings__state--error {
  flex-direction: column;
  color: var(--danger);
}

.ios-summary {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--border);
}

.ios-summary__icon,
.ios-card > header > span:first-child {
  display: grid;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 9px;
}

.ios-summary h3,
.ios-card h4 {
  font-size: 15px;
}

.ios-summary p,
.ios-card header p,
.ios-pairing-note {
  margin-top: 4px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
}

.ios-card {
  margin-top: 18px;
  padding: 18px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 10px;
}

.ios-card > header {
  display: flex;
  align-items: center;
  gap: 11px;
}

.ios-card > header > div {
  min-width: 0;
  flex: 1;
}

.ios-pairing-actions,
.ios-qr-dialog footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: 15px;
}

.ios-status {
  flex: 0 0 auto;
  padding: 5px 8px;
  color: var(--muted);
  font-size: 11px;
  font-weight: 700;
  background: var(--surface-hover);
  border-radius: 999px;
}

.ios-status.is-active {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.ios-status.is-blocked {
  color: var(--danger);
  background: var(--danger-soft);
}

.ios-notice {
  margin-top: 16px;
  padding: 12px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.ios-pairing-facts {
  display: grid;
  gap: 10px;
  margin: 16px 0 0;
}

.ios-pairing-facts div {
  min-width: 0;
}

.ios-pairing-facts dt {
  color: var(--muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.ios-pairing-facts dd {
  margin: 4px 0 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.ios-pairing-actions {
  justify-content: flex-start;
  padding-top: 4px;
}

.ios-card > .field-error {
  margin: 12px 0 0;
}

.ios-qr-backdrop {
  position: fixed;
  z-index: 175;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(16 24 40 / 52%);
}

.ios-qr-dialog {
  width: min(430px, 100%);
  padding: 20px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow);
}

.ios-qr-dialog > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.ios-qr-dialog h2 {
  font-size: 17px;
}

.ios-qr-dialog header p,
.ios-qr-warning {
  margin-top: 5px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
}

.ios-qr-image {
  display: grid;
  margin: 18px auto 0;
  place-items: center;
}

.ios-qr-image img {
  display: block;
  width: min(320px, 100%);
  height: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.ios-qr-warning {
  text-align: center;
}

@media (max-width: 520px) {
  .ios-card > header {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .ios-status {
    margin-left: 51px;
  }

  .ios-pairing-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .ios-pairing-actions button {
    width: 100%;
  }

  .ios-qr-backdrop {
    align-items: end;
    padding: 0 0 var(--mobile-nav-height);
  }

  .ios-qr-dialog {
    width: 100%;
    border-radius: 12px 12px 0 0;
  }
}
</style>
