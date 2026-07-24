<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  AlertTriangle,
  CheckCircle2,
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

const MAX_PEM_BYTES = 1024 * 1024
const PEM_ACCEPT =
  '.pem,.crt,.cer,.key,application/x-pem-file,application/pem-certificate-chain,text/plain'
const timestampFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit'
})

const settings = ref<TLSSettings | null>(null)
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const operationError = ref('')
const connectionNotice = ref('')

const certificateFile = ref<File | null>(null)
const privateKeyFile = ref<File | null>(null)
const certificateError = ref('')
const privateKeyError = ref('')
const certificateInput = ref<HTMLInputElement | null>(null)
const privateKeyInput = ref<HTMLInputElement | null>(null)

const modeLabel = computed(() =>
  settings.value?.mode === 'automatic' ? '自动证书' : '用户证书'
)
const renewalLabel = computed(() =>
  settings.value?.renews_automatically ? '自动续期' : '不自动续期'
)

function formatTimestamp(value: string): string {
  return timestampFormatter.format(new Date(value))
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  return `${Math.ceil(bytes / 1024)} KiB`
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 403) {
    return '当前账户无权修改 HTTPS 证书'
  }
  return error instanceof Error && error.message ? error.message : fallback
}

async function loadTLSSettings(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    settings.value = await gateway.getTLSSettings()
  } catch (error) {
    loadError.value = errorMessage(error, '无法载入 HTTPS 证书设置')
  } finally {
    loading.value = false
  }
}

function handleFileSelection(kind: 'certificate' | 'private-key', event: Event): void {
  const input = event.currentTarget as HTMLInputElement
  const file = input.files?.[0] || null
  const tooLarge = Boolean(file && file.size > MAX_PEM_BYTES)

  operationError.value = ''
  if (kind === 'certificate') {
    certificateFile.value = tooLarge ? null : file
    certificateError.value = tooLarge ? '证书文件不能超过 1 MiB' : ''
  } else {
    privateKeyFile.value = tooLarge ? null : file
    privateKeyError.value = tooLarge ? '私钥文件不能超过 1 MiB' : ''
  }
  if (tooLarge) input.value = ''
}

async function readPEM(file: File, kind: 'certificate' | 'private-key'): Promise<string> {
  if (file.size > MAX_PEM_BYTES) {
    throw new Error(kind === 'certificate' ? '证书文件不能超过 1 MiB' : '私钥文件不能超过 1 MiB')
  }

  let text: string
  try {
    text = await file.text()
  } catch {
    throw new Error(kind === 'certificate' ? '无法读取证书文件' : '无法读取私钥文件')
  }
  if (!text.trim() || text.includes('\0') || text.includes('\uFFFD')) {
    throw new Error(
      kind === 'certificate' ? '证书必须是文本 PEM 文件' : '私钥必须是文本 PEM 文件'
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
      kind === 'certificate' ? '证书文件缺少有效的 PEM 标记' : '私钥文件缺少有效的 PEM 标记'
    )
  }
  return text
}

function clearSelectedFiles(): void {
  certificateFile.value = null
  privateKeyFile.value = null
  certificateError.value = ''
  privateKeyError.value = ''
  if (certificateInput.value) certificateInput.value.value = ''
  if (privateKeyInput.value) privateKeyInput.value.value = ''
}

async function installUserCertificate(): Promise<void> {
  operationError.value = ''
  certificateError.value = certificateFile.value ? '' : '请选择证书 PEM 文件'
  privateKeyError.value = privateKeyFile.value ? '' : '请选择私钥 PEM 文件'
  if (!certificateFile.value || !privateKeyFile.value || saving.value) return

  saving.value = true
  try {
    let certificatePEM = ''
    let privateKeyPEM = ''
    try {
      certificatePEM = await readPEM(certificateFile.value, 'certificate')
    } catch (error) {
      certificateError.value = errorMessage(error, '无法读取证书文件')
    }
    try {
      privateKeyPEM = await readPEM(privateKeyFile.value, 'private-key')
    } catch (error) {
      privateKeyError.value = errorMessage(error, '无法读取私钥文件')
    }
    if (certificateError.value || privateKeyError.value) return

    settings.value = await gateway.updateTLSSettings({
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    })
    clearSelectedFiles()
    connectionNotice.value =
      '证书设置已保存。当前 HTTPS 连接可能仍使用旧证书，请按需刷新页面。'
  } catch (error) {
    operationError.value = errorMessage(error, '无法安装用户证书')
  } finally {
    saving.value = false
  }
}

async function useAutomaticCertificate(): Promise<void> {
  if (saving.value) return
  const confirmed = await requestConfirmation({
    title: '使用自动证书？',
    message: '将停止使用当前用户证书。当前 HTTPS 连接可能需要刷新。',
    confirmLabel: '使用自动证书'
  })
  if (!confirmed) return

  saving.value = true
  operationError.value = ''
  try {
    settings.value = await gateway.updateTLSSettings({ operation: 'use_automatic' })
    connectionNotice.value =
      '证书设置已保存。当前 HTTPS 连接可能仍使用旧证书，请按需刷新页面。'
  } catch (error) {
    operationError.value = errorMessage(error, '无法切换到自动证书')
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
    <div v-if="loading" class="tls-settings__state" role="status">
      <LoaderCircle class="spin" :size="18" />
      <span>正在载入 HTTPS 证书</span>
    </div>

    <div v-else-if="loadError" class="tls-settings__state tls-settings__state--error" role="alert">
      <span>{{ loadError }}</span>
      <button class="secondary-button" type="button" @click="loadTLSSettings">
        <RefreshCw :size="15" />
        <span>重试</span>
      </button>
    </div>

    <template v-else-if="settings">
      <header class="tls-summary">
        <span class="tls-summary__icon"><ShieldCheck :size="21" /></span>
        <div class="tls-summary__heading">
          <h3 id="tls-settings-title">当前证书</h3>
          <p>{{ modeLabel }} · {{ renewalLabel }}</p>
        </div>
        <div class="tls-summary__status" aria-label="证书状态">
          <span class="status-label" :class="{ 'status-label--danger': settings.expired }">
            {{ settings.expired ? '已过期' : '有效' }}
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
        <span>用户证书已过期，不会自动替换。</span>
      </p>

      <dl class="tls-facts">
        <div>
          <dt>生效时间</dt>
          <dd>{{ formatTimestamp(settings.not_before) }}</dd>
        </div>
        <div>
          <dt>到期时间</dt>
          <dd>{{ formatTimestamp(settings.not_after) }}</dd>
        </div>
        <div class="tls-facts__wide">
          <dt>主题</dt>
          <dd>{{ settings.subject || '未提供' }}</dd>
        </div>
        <div class="tls-facts__wide">
          <dt>签发者</dt>
          <dd>{{ settings.issuer || '未提供' }}</dd>
        </div>
        <div class="tls-facts__wide">
          <dt>DNS SAN</dt>
          <dd>{{ settings.dns_names.join('、') || '无' }}</dd>
        </div>
        <div class="tls-facts__wide">
          <dt>IP SAN</dt>
          <dd>{{ settings.ip_addresses.join('、') || '无' }}</dd>
        </div>
        <div class="tls-facts__wide">
          <dt>SHA-256 指纹</dt>
          <dd><code>{{ settings.fingerprint_sha256 }}</code></dd>
        </div>
      </dl>

      <div v-if="settings.mode === 'automatic'" class="tls-ca-download">
        <a class="secondary-button" :href="tlsCAPath">
          <Download :size="16" />
          <span>下载根证书</span>
        </a>
        <p>导入系统信任库后，浏览器音频与通知才能正常使用</p>
      </div>

      <section class="tls-install" aria-labelledby="tls-install-title">
        <header>
          <div>
            <h3 id="tls-install-title">安装用户证书</h3>
            <p>证书和私钥需为 PEM 文本，每个文件最大 1 MiB。</p>
          </div>
          <button
            v-if="settings.mode === 'user'"
            class="secondary-button"
            type="button"
            :disabled="saving"
            @click="useAutomaticCertificate"
          >
            <RefreshCw :size="16" />
            <span>使用自动证书</span>
          </button>
        </header>

        <form @submit.prevent="installUserCertificate">
          <div class="tls-file-grid">
            <div class="tls-file-field" :class="{ 'has-error': certificateError }">
              <span class="tls-file-field__label">证书 PEM</span>
              <label
                class="tls-file-picker"
                :class="{ 'is-disabled': saving }"
                for="tls-certificate-file"
              >
                <FileText :size="18" />
                <span>
                  <strong>{{ certificateFile?.name || '选择证书文件' }}</strong>
                  <small>
                    {{
                      certificateFile
                        ? formatFileSize(certificateFile.size)
                        : '证书或证书链'
                    }}
                  </small>
                </span>
              </label>
              <input
                id="tls-certificate-file"
                ref="certificateInput"
                class="sr-only"
                type="file"
                :accept="PEM_ACCEPT"
                :disabled="saving"
                :aria-describedby="certificateError ? 'tls-certificate-error' : undefined"
                @change="handleFileSelection('certificate', $event)"
              />
              <p
                v-if="certificateError"
                id="tls-certificate-error"
                class="field-error"
                role="alert"
              >
                {{ certificateError }}
              </p>
            </div>

            <div class="tls-file-field" :class="{ 'has-error': privateKeyError }">
              <span class="tls-file-field__label">私钥 PEM</span>
              <label
                class="tls-file-picker"
                :class="{ 'is-disabled': saving }"
                for="tls-private-key-file"
              >
                <KeyRound :size="18" />
                <span>
                  <strong>{{ privateKeyFile?.name || '选择私钥文件' }}</strong>
                  <small>
                    {{ privateKeyFile ? formatFileSize(privateKeyFile.size) : '匹配证书的私钥' }}
                  </small>
                </span>
              </label>
              <input
                id="tls-private-key-file"
                ref="privateKeyInput"
                class="sr-only"
                type="file"
                :accept="PEM_ACCEPT"
                :disabled="saving"
                :aria-describedby="privateKeyError ? 'tls-private-key-error' : undefined"
                @change="handleFileSelection('private-key', $event)"
              />
              <p
                v-if="privateKeyError"
                id="tls-private-key-error"
                class="field-error"
                role="alert"
              >
                {{ privateKeyError }}
              </p>
            </div>
          </div>

          <footer class="tls-install__actions">
            <div class="tls-install__feedback">
              <p v-if="operationError" class="field-error" role="alert">
                {{ operationError }}
              </p>
              <p v-else-if="connectionNotice" class="tls-connection-notice" role="status">
                <CheckCircle2 :size="16" />
                <span>{{ connectionNotice }}</span>
              </p>
            </div>
            <button class="primary-button" type="submit" :disabled="saving">
              <LoaderCircle v-if="saving" class="spin" :size="17" />
              <Upload v-else :size="17" />
              <span>安装证书</span>
            </button>
          </footer>
        </form>
      </section>
    </template>
  </section>
</template>
