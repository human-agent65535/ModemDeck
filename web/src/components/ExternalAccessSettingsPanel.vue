<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Check,
  Clipboard,
  KeyRound,
  LoaderCircle,
  QrCode,
  RadioTower,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X
} from '@lucide/vue'
import QRCode from 'qrcode'
import { gateway } from '../api/client'
import type { ExternalAccessStatus, IOSPairingStatus } from '../api/types'
import { ApiError } from '../api/types'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { requestConfirmation } from '../state/confirmation'
import { sessionState } from '../state/session'
import OverlayDialog from './OverlayDialog.vue'
import SettingsLoadBoundary from './settings/SettingsLoadBoundary.vue'
import SettingsModuleCard from './settings/SettingsModuleCard.vue'

const STATUS_REFRESH_INTERVAL_MS = 15_000
const PAIRING_CONFIRMATION_INTERVAL_MS = 2_000

const { t, locale } = useI18n()
const isAdmin = computed(() => sessionState.role === 'admin')
const loading = ref(true)
const loadError = ref('')
const pairing = ref<IOSPairingStatus | null>(null)
const externalAccess = ref<ExternalAccessStatus | null>(null)
const pairingPending = ref(false)
const pairingError = ref('')
const qrDataURL = ref('')
const pairingCode = ref('')
const pairingCodeInput = ref<HTMLTextAreaElement | null>(null)
const copied = ref(false)
const selectedServerURL = ref('')
let statusRefreshTimer: number | undefined
let statusLoadPending = false
let pairingConfirmationTimer: number | undefined
let pairingConfirmationLoadPending = false

const pairingReady = computed(
  () =>
    pairing.value?.allowed &&
    pairing.value.availability === 'ready' &&
    pairing.value.server_urls.length > 0
)
const refreshMutation = useSettingsMutation({
  errorMessage: cause =>
    errorMessage(cause, t('iosPairing.refreshFailed')),
  successMessage: () => t('iosPairing.refreshed'),
  toast: 'both'
})
const pairingStatusLabel = computed(() => {
  switch (pairing.value?.availability) {
    case 'permission_required':
      return t('iosPairing.notAllowed')
    case 'cloudflare_required':
      return t('iosPairing.notInstalled')
    case 'connector_unavailable':
      return t('iosPairing.disconnected')
    case 'route_unavailable':
      return t('iosPairing.routeUnavailable')
    case 'ready':
      if (pairing.value.paired) return t('iosPairing.paired')
      if (pairing.value.has_credential) return t('iosPairing.waiting')
      return t('iosPairing.notPaired')
    default:
      return ''
  }
})
const pairingNotice = computed(() => {
  switch (pairing.value?.availability) {
    case 'permission_required':
      return t('iosPairing.permissionRequired')
    case 'cloudflare_required':
      return t('iosPairing.cloudflareRequired')
    case 'connector_unavailable':
    case 'route_unavailable':
      return t('iosPairing.cloudflareUnavailable')
    default:
      return ''
  }
})

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
    if (
      cause.code === 'server_url_required' ||
      cause.code === 'invalid_server_url'
    ) {
      return t('iosPairing.selectServer')
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

function applyPairingStatus(status: IOSPairingStatus): void {
  const wasPaired = pairing.value?.paired ?? false
  pairing.value = status
  if (!status.server_urls.includes(selectedServerURL.value)) {
    selectedServerURL.value = status.server_urls[0] || ''
  }
  if (!wasPaired && status.paired && qrDataURL.value) {
    closeQR()
  }
  syncPairingConfirmationPolling()
}

function clearPairingConfirmationPolling(): void {
  if (pairingConfirmationTimer === undefined) return
  window.clearInterval(pairingConfirmationTimer)
  pairingConfirmationTimer = undefined
}

function syncPairingConfirmationPolling(): void {
  const waiting = Boolean(
    pairing.value?.has_credential && !pairing.value.paired
  )
  if (!waiting) {
    clearPairingConfirmationPolling()
    return
  }
  if (pairingConfirmationTimer !== undefined) return
  pairingConfirmationTimer = window.setInterval(() => {
    void refreshPairingConfirmation()
  }, PAIRING_CONFIRMATION_INTERVAL_MS)
}

async function refreshPairingConfirmation(): Promise<void> {
  if (
    pairingConfirmationLoadPending ||
    statusLoadPending ||
    !pairing.value?.has_credential ||
    pairing.value.paired
  ) {
    return
  }
  pairingConfirmationLoadPending = true
  try {
    const result = await gateway.getIOSPairing()
    applyPairingStatus(result.pairing)
  } catch {
    // The regular status refresh reports persistent connectivity failures.
  } finally {
    pairingConfirmationLoadPending = false
  }
}

async function refreshStatus(background: boolean): Promise<void> {
  if (statusLoadPending) return
  statusLoadPending = true
  if (!background) {
    loading.value = true
    loadError.value = ''
  }
  try {
    const [pairingResult, status] = await Promise.all([
      gateway.getIOSPairing(),
      isAdmin.value ? gateway.getExternalAccessStatus() : Promise.resolve(null)
    ])
    applyPairingStatus(pairingResult.pairing)
    externalAccess.value = status
    loadError.value = ''
  } catch (cause) {
    if (!background || !pairing.value) {
      loadError.value = errorMessage(cause, t('iosPairing.loadFailed'))
    }
  } finally {
    statusLoadPending = false
    if (!background) loading.value = false
  }
}

async function refreshExternalAccess(): Promise<void> {
  if (!isAdmin.value || refreshMutation.saving.value) return
  const result = await refreshMutation.run(async () => {
    const status = await gateway.refreshExternalAccess()
    const pairingResult = await gateway.getIOSPairing()
    return { status, pairing: pairingResult.pairing }
  })
  if (!result.ok) return
  externalAccess.value = result.value.status
  applyPairingStatus(result.value.pairing)
}

function load(): Promise<void> {
  return refreshStatus(false)
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
    const result = await gateway.createIOSPairing(
      selectedServerURL.value || undefined
    )
    if (!result.payload) throw new Error(t('iosPairing.invalidPayload'))
    applyPairingStatus(result.pairing)
    pairingCode.value = JSON.stringify(result.payload)
    qrDataURL.value = await QRCode.toDataURL(pairingCode.value, {
      width: 320,
      margin: 2,
      errorCorrectionLevel: 'M',
      color: {
        dark: '#16211f',
        light: '#ffffff'
      }
    })
    copied.value = false
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
    applyPairingStatus({
      ...pairing.value,
      has_credential: false,
      credential_created_at: undefined,
      paired: false,
      paired_at: undefined
    })
    closeQR()
  } catch (cause) {
    pairingError.value = errorMessage(cause, t('iosPairing.revokeFailed'))
  } finally {
    pairingPending.value = false
  }
}

function selectPairingCode(): void {
  pairingCodeInput.value?.select()
}

function copySelectedPairingCode(): boolean {
  selectPairingCode()
  return document.execCommand('copy')
}

async function copyPairingCode(): Promise<void> {
  if (!pairingCode.value) return
  pairingError.value = ''
  try {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(pairingCode.value)
      } catch {
        if (!copySelectedPairingCode()) throw new Error('copy failed')
      }
    } else {
      if (!copySelectedPairingCode()) throw new Error('copy failed')
    }
    copied.value = true
  } catch {
    selectPairingCode()
    pairingError.value = t('iosPairing.copyFailed')
  }
}

function closeQR(): void {
  qrDataURL.value = ''
  pairingCode.value = ''
  pairingError.value = ''
  copied.value = false
}

onMounted(() => {
  void load()
  statusRefreshTimer = window.setInterval(() => {
    void refreshStatus(true)
  }, STATUS_REFRESH_INTERVAL_MS)
})

onBeforeUnmount(() => {
  if (statusRefreshTimer !== undefined) {
    window.clearInterval(statusRefreshTimer)
  }
  clearPairingConfirmationPolling()
})
</script>

<template>
  <section class="ios-settings" :aria-label="t('iosPairing.title')">
    <SettingsLoadBoundary
      :loading="loading"
      :error="Boolean(loadError)"
      :loading-title="t('iosPairing.loading')"
      :error-title="t('iosPairing.loadFailed')"
      :detail="loadError"
      retryable
      @retry="load"
    >
    <template v-if="pairing">
      <SettingsModuleCard
        v-if="isAdmin && externalAccess"
        class="ios-card"
        :title="t('iosPairing.tunnelTitle')"
        title-id="ios-tunnel-title"
        surface="subtle"
        :has-body="externalAccess.cloudflare.enabled"
      >
        <template #icon>
          <ShieldCheck :size="19" />
        </template>
        <template #status>
          <div class="ios-tunnel-status">
            <button
              v-if="externalAccess.cloudflare.enabled"
              class="icon-button ios-refresh-button"
              type="button"
              :disabled="refreshMutation.saving.value"
              :aria-label="t('iosPairing.refresh')"
              :title="t('iosPairing.refresh')"
              @click="refreshExternalAccess"
            >
              <RefreshCw
                :class="{ spin: refreshMutation.saving.value }"
                :size="16"
              />
            </button>
            <span
              class="ios-status"
              :class="{
                'is-active': externalAccess.cloudflare.connected,
                'is-blocked': !externalAccess.cloudflare.connected
              }"
            >
              {{
                !externalAccess.cloudflare.enabled
                  ? t('iosPairing.notInstalled')
                  : externalAccess.cloudflare.connected
                    ? t('iosPairing.connected')
                    : externalAccess.cloudflare.connector_connected
                      ? t('iosPairing.routeUnavailable')
                      : t('iosPairing.disconnected')
              }}
            </span>
          </div>
        </template>
        <dl v-if="externalAccess.cloudflare.enabled" class="ios-pairing-facts">
          <div v-if="externalAccess.cloudflare.api_urls.length">
            <dt>API</dt>
            <dd
              v-for="url in externalAccess.cloudflare.api_urls"
              :key="url"
              class="ios-route"
            >
              <Check
                v-if="externalAccess.cloudflare.verified_api_urls.includes(url)"
                :size="14"
                aria-hidden="true"
              />
              <X
                v-else
                class="ios-route__unverified"
                :size="14"
                aria-hidden="true"
              />
              <code>{{ url }}</code>
              <span class="sr-only">
                {{
                  externalAccess.cloudflare.verified_api_urls.includes(url)
                    ? t('iosPairing.routeVerified')
                    : t('iosPairing.routeUnverified')
                }}
              </span>
            </dd>
          </div>
          <div v-if="externalAccess.cloudflare.web_urls.length">
            <dt>Web</dt>
            <dd v-for="url in externalAccess.cloudflare.web_urls" :key="url">
              <code>{{ url }}</code>
            </dd>
          </div>
        </dl>
      </SettingsModuleCard>

      <SettingsModuleCard
        v-if="isAdmin && externalAccess"
        class="ios-card"
        :title="t('iosPairing.turnTitle')"
        title-id="external-turn-title"
        surface="subtle"
        :has-body="!externalAccess.turn.available"
      >
        <template #icon>
          <RadioTower :size="19" />
        </template>
        <template #status>
          <span
            class="ios-status"
            :class="{
              'is-active': externalAccess.turn.available,
              'is-blocked': !externalAccess.turn.available
            }"
          >
            {{
              !externalAccess.turn.configured
                ? t('iosPairing.turnNotConfigured')
                : externalAccess.turn.available
                  ? t('iosPairing.turnAvailable')
                  : t('iosPairing.turnUnavailable')
            }}
          </span>
        </template>
        <div
          v-if="!externalAccess.turn.available"
          class="ios-notice ios-notice--danger"
        >
          {{ t('iosPairing.turnCallUnavailable') }}
        </div>
      </SettingsModuleCard>

      <SettingsModuleCard
        class="ios-card"
        :title="t('iosPairing.yourDevice')"
        title-id="ios-pairing-title"
        :description="t('iosPairing.yourDeviceDescription')"
        surface="subtle"
      >
        <template #icon>
          <KeyRound :size="19" />
        </template>
        <template #status>
          <span
            class="ios-status"
            :class="{
              'is-active': pairing.paired && pairing.availability === 'ready',
              'is-blocked': pairing.availability !== 'ready'
            }"
          >
            {{ pairingStatusLabel }}
          </span>
        </template>

        <div v-if="pairingNotice" class="ios-notice">
          {{ pairingNotice }}
        </div>
        <template v-if="pairing.allowed">
          <dl class="ios-pairing-facts">
            <div v-if="pairing.credential_created_at">
              <dt>{{ t('iosPairing.createdAt') }}</dt>
              <dd>{{ formatTimestamp(pairing.credential_created_at) }}</dd>
            </div>
          </dl>
          <label
            v-if="pairing.server_urls.length > 1"
            class="field ios-server-select"
          >
            <span>{{ t('iosPairing.pairingRoute') }}</span>
            <select
              v-model="selectedServerURL"
              :disabled="pairingPending"
            >
              <option
                v-for="url in pairing.server_urls"
                :key="url"
                :value="url"
              >
                {{ url }}
              </option>
            </select>
          </label>
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
      </SettingsModuleCard>
    </template>
    </SettingsLoadBoundary>

    <OverlayDialog
      :open="Boolean(qrDataURL)"
      size="small"
      labelledby="ios-qr-title"
      initial-focus="[data-overlay-initial]"
      @close="closeQR"
    >
      <div class="ios-qr-dialog">
          <header>
            <div>
              <h2 id="ios-qr-title">{{ t('iosPairing.scanTitle') }}</h2>
              <p>{{ t('iosPairing.scanDescription') }}</p>
              <p>{{ t('iosPairing.scanWaiting') }}</p>
            </div>
            <button
              class="icon-button"
              type="button"
              data-overlay-initial
              :aria-label="t('common.close')"
              @click="closeQR"
            >
              <X :size="19" />
            </button>
          </header>
          <section class="ios-pairing-code">
            <div>
              <strong>{{ t('iosPairing.pairingCode') }}</strong>
              <p>{{ t('iosPairing.sameDeviceHint') }}</p>
            </div>
            <textarea
              ref="pairingCodeInput"
              :value="pairingCode"
              rows="3"
              readonly
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              :aria-label="t('iosPairing.pairingCode')"
              @focus="selectPairingCode"
            />
            <button
              class="secondary-button"
              type="button"
              @click="copyPairingCode"
            >
              <Check v-if="copied" :size="16" />
              <Clipboard v-else :size="16" />
              {{
                copied
                  ? t('iosPairing.copied')
                  : t('iosPairing.copyPairingData')
              }}
            </button>
            <p v-if="pairingError" class="field-error" role="alert">
              {{ pairingError }}
            </p>
          </section>
          <div class="ios-qr-image">
            <img :src="qrDataURL" :alt="t('iosPairing.qrAlt')" />
          </div>
          <p class="ios-qr-warning">{{ t('iosPairing.showOnce') }}</p>
          <footer>
            <button class="primary-button" type="button" @click="closeQR">
              {{ t('common.done') }}
            </button>
          </footer>
      </div>
    </OverlayDialog>
  </section>
</template>

<style scoped>
.ios-settings {
  display: grid;
  width: 100%;
  gap: 18px;
}

.ios-pairing-note {
  margin-top: 4px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
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

.ios-tunnel-status {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
}

.ios-refresh-button {
  width: 30px;
  height: 30px;
}

.ios-notice {
  margin: 0;
  padding: 12px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.ios-notice--danger {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: color-mix(in srgb, var(--danger) 28%, var(--border));
}

.ios-pairing-facts {
  display: grid;
  gap: 10px;
  margin: 0;
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

.ios-route {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ios-route > svg:first-child {
  flex: 0 0 auto;
  color: var(--accent-strong);
}

.ios-route > svg.ios-route__unverified {
  color: var(--danger);
}

.ios-server-select {
  max-width: 520px;
  margin-top: 14px;
}

.ios-pairing-actions {
  justify-content: flex-start;
  padding-top: 4px;
}

.ios-card .field-error {
  margin: 12px 0 0;
}

.ios-qr-dialog {
  max-height: inherit;
  overflow-y: auto;
  padding: 20px;
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
.ios-pairing-code p,
.ios-qr-warning {
  margin-top: 5px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.5;
}

.ios-pairing-code {
  display: grid;
  gap: 9px;
  margin-top: 16px;
}

.ios-pairing-code > div {
  display: grid;
  gap: 3px;
}

.ios-pairing-code > div p {
  margin: 0;
}

.ios-pairing-code textarea {
  width: 100%;
  min-height: 70px;
  padding: 9px 10px;
  resize: none;
  color: var(--text);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  line-height: 1.45;
  overflow-wrap: anywhere;
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.ios-pairing-code .secondary-button {
  justify-self: start;
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
  .ios-status {
    margin-left: 51px;
  }

  .ios-tunnel-status .ios-status {
    margin-left: 0;
  }

  .ios-pairing-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .ios-pairing-actions button {
    width: 100%;
  }

  .ios-qr-image img {
    width: min(260px, 70vw);
  }
}
</style>
