<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from '@lucide/vue'
import type { LineSummary, ProxyInstance, ProxyMode } from '../api/types'
import type { ProxyDraft } from '../state/network'
import { isIPAddress } from '../utils/ipAddress'
import { proxyCredentialError } from '../utils/proxyCredentials'
import LineSelector from './LineSelector.vue'
import OverlayDialog from './OverlayDialog.vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    open: boolean
    proxy?: ProxyInstance
    lines: LineSummary[]
    initialLineId?: string
    busy?: boolean
    error?: string
  }>(),
  {
    proxy: undefined,
    initialLineId: '',
    busy: false,
    error: ''
  }
)

const emit = defineEmits<{
  close: []
  save: [draft: ProxyDraft]
}>()

const form = reactive<ProxyDraft>({
  line_id: '',
  mode: 'socks5',
  listen_address: '127.0.0.1',
  listen_port: 1080,
  username: '',
  password: ''
})
const localError = reactive({ message: '' })

const title = computed(() => (props.proxy ? t('proxy.edit') : t('proxy.add')))
const visibleError = computed(() => localError.message || props.error)

function resetForm(): void {
  localError.message = ''
  if (props.proxy) {
    Object.assign(form, {
      line_id: props.proxy.line_id,
      mode: props.proxy.mode,
      listen_address: props.proxy.listen_address,
      listen_port: props.proxy.listen_port,
      username: props.proxy.username,
      password: ''
    })
    return
  }
  Object.assign(form, {
    line_id: props.initialLineId,
    mode: 'socks5' as ProxyMode,
    listen_address: '127.0.0.1',
    listen_port: 1080,
    username: '',
    password: ''
  })
}

function close(): void {
  if (!props.busy) emit('close')
}

function submit(): void {
  const username = form.username.trim()
  const password = form.password
  if (!form.line_id) {
    localError.message = t('proxy.selectLine')
    return
  }
  if (!form.listen_address.trim()) {
    localError.message = t('proxy.enterListenAddress')
    return
  }
  if (!isIPAddress(form.listen_address)) {
    localError.message = t('proxy.invalidListenAddress')
    return
  }
  if (
    !Number.isSafeInteger(form.listen_port) ||
    form.listen_port < 1024 ||
    form.listen_port > 65535
  ) {
    localError.message = t('proxy.invalidPort')
    return
  }
  const credentialError = proxyCredentialError(
    form.mode,
    username,
    password,
    props.proxy?.has_password,
    key => t(key)
  )
  if (credentialError) {
    localError.message = credentialError
    return
  }
  localError.message = ''
  emit('save', {
    line_id: form.line_id,
    mode: form.mode,
    listen_address: form.listen_address.trim(),
    listen_port: form.listen_port,
    username,
    password
  })
}

watch(
  () => props.open,
  open => {
    if (open) resetForm()
  },
  { immediate: true }
)

watch(
  () => [props.proxy?.id, props.initialLineId],
  () => {
    if (props.open) resetForm()
  }
)
</script>

<template>
  <OverlayDialog
    :open="open"
    size="medium"
    :label="title"
    :describedby="visibleError ? 'proxy-editor-error' : undefined"
    initial-focus='button[aria-haspopup="listbox"]:not([disabled])'
    @close="close"
  >
    <div class="proxy-modal">
      <header>
        <h2>{{ title }}</h2>
        <button
          class="icon-button"
          type="button"
          :title="t('common.close')"
          :aria-label="t('common.close')"
          :disabled="busy"
          @click="close"
        >
          <X :size="19" />
        </button>
      </header>

      <form @submit.prevent="submit">
      <LineSelector
        v-model="form.line_id"
        class="proxy-field"
        :lines="lines"
        :label="t('proxy.line')"
        :disabled="busy"
      />

      <fieldset class="proxy-field">
        <legend>{{ t('common.protocol') }}</legend>
        <div class="proxy-segmented">
          <button
            type="button"
            :class="{ 'is-selected': form.mode === 'socks5' }"
            :aria-pressed="form.mode === 'socks5'"
            :disabled="busy"
            @click="form.mode = 'socks5'"
          >
            SOCKS5
          </button>
          <button
            type="button"
            :class="{ 'is-selected': form.mode === 'http' }"
            :aria-pressed="form.mode === 'http'"
            :disabled="busy"
            @click="form.mode = 'http'"
          >
            HTTP CONNECT
          </button>
        </div>
      </fieldset>

      <div class="proxy-fields-row">
        <label class="proxy-field">
          <span>{{ t('proxy.listenAddress') }}</span>
          <input
            v-model="form.listen_address"
            type="text"
            inputmode="url"
            placeholder="127.0.0.1"
            spellcheck="false"
            :disabled="busy"
          />
        </label>
        <label class="proxy-field is-port">
          <span>{{ t('common.port') }}</span>
          <input
            v-model.number="form.listen_port"
            type="number"
            min="1024"
            max="65535"
            inputmode="numeric"
            :disabled="busy"
          />
        </label>
      </div>

      <div class="proxy-fields-row is-even">
        <label class="proxy-field">
          <span>{{ t('common.username') }}</span>
          <input
            v-model="form.username"
            type="text"
            autocomplete="off"
            :disabled="busy"
          />
        </label>
        <label class="proxy-field">
          <span>{{ t('common.password') }}</span>
          <input
            v-model="form.password"
            type="password"
            autocomplete="new-password"
            :placeholder="proxy?.has_password ? t('proxy.keepPassword') : ''"
            :disabled="busy"
          />
        </label>
      </div>

      <p
        v-if="visibleError"
        id="proxy-editor-error"
        class="proxy-modal__error"
        role="alert"
      >
        {{ visibleError }}
      </p>

      <footer>
        <button class="secondary-button" type="button" :disabled="busy" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          class="primary-button"
          type="submit"
          :disabled="busy || lines.length === 0"
        >
          {{ busy ? t('common.saving') : t('common.save') }}
        </button>
      </footer>
      </form>
    </div>
  </OverlayDialog>
</template>

<style scoped>
.proxy-modal {
  max-height: inherit;
  overflow: auto;
}

.proxy-modal > header {
  display: flex;
  min-height: 60px;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px 8px 20px;
  border-bottom: 1px solid var(--border);
}

.proxy-modal h2 {
  font-size: 17px;
}

.proxy-modal form {
  display: grid;
  gap: 18px;
  padding: 20px;
}

.proxy-field {
  display: grid;
  min-width: 0;
  gap: 7px;
  padding: 0;
  margin: 0;
  border: 0;
}

.proxy-field > span,
.proxy-field legend {
  padding: 0;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}

.proxy-field input,
.proxy-field select {
  width: 100%;
  height: 42px;
  min-width: 0;
  padding: 0 11px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}

.proxy-field input:focus,
.proxy-field select:focus {
  border-color: var(--accent);
}

.proxy-fields-row {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) 120px;
  gap: 12px;
}

.proxy-fields-row.is-even {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.proxy-segmented {
  display: grid;
  width: fit-content;
  grid-template-columns: repeat(2, minmax(110px, auto));
  padding: 3px;
  background: var(--surface-hover);
  border-radius: 7px;
}

.proxy-segmented button {
  min-height: 34px;
  padding: 0 12px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  background: transparent;
  border-radius: 5px;
}

.proxy-segmented button.is-selected {
  color: var(--accent-strong);
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(16 24 40 / 12%);
}

.proxy-modal__error {
  color: var(--danger);
  font-size: 12px;
}

.proxy-modal form > footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 4px;
}

@media (max-width: 560px) {
  .proxy-modal {
    max-height: calc(100dvh - var(--mobile-nav-height) - 8px);
  }

  .proxy-fields-row,
  .proxy-fields-row.is-even {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
