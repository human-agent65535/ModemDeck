<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { X } from '@lucide/vue'
import type { LineSummary, ProxyInstance, ProxyMode } from '../api/types'
import type { ProxyDraft } from '../state/network'
import { isIPAddress } from '../utils/ipAddress'
import { proxyCredentialError } from '../utils/proxyCredentials'
import LineSelector from './LineSelector.vue'

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
const dialog = ref<HTMLElement>()
let previousFocus: HTMLElement | null = null

const title = computed(() => (props.proxy ? '编辑代理' : '添加代理'))
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

function focusableElements(): HTMLElement[] {
  if (!dialog.value) return []
  return Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]), select:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'
    )
  )
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    close()
    return
  }
  if (event.key !== 'Tab') return

  const elements = focusableElements()
  if (!elements.length) {
    event.preventDefault()
    dialog.value?.focus()
    return
  }
  const first = elements[0]
  const last = elements[elements.length - 1]
  if (!first || !last) return
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

function submit(): void {
  const username = form.username.trim()
  const password = form.password
  if (!form.line_id) {
    localError.message = '请选择线路'
    return
  }
  if (!form.listen_address.trim()) {
    localError.message = '请输入监听地址'
    return
  }
  if (!isIPAddress(form.listen_address)) {
    localError.message = '监听地址必须是 IPv4 或 IPv6 地址'
    return
  }
  if (
    !Number.isSafeInteger(form.listen_port) ||
    form.listen_port < 1024 ||
    form.listen_port > 65535
  ) {
    localError.message = '端口范围为 1024–65535'
    return
  }
  const credentialError = proxyCredentialError(
    form.mode,
    username,
    password,
    props.proxy?.has_password
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
  async (open, wasOpen) => {
    if (open) {
      previousFocus =
        typeof document === 'undefined' ? null : document.activeElement as HTMLElement | null
      resetForm()
      await nextTick()
      dialog.value
        ?.querySelector<HTMLButtonElement>(
          'button[aria-haspopup="listbox"]:not([disabled])'
        )
        ?.focus()
    } else if (wasOpen) {
      await nextTick()
      previousFocus?.focus()
      previousFocus = null
    }
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
  <Teleport to="body">
    <div
      v-if="open"
      class="proxy-modal-backdrop"
      role="presentation"
      @mousedown.self="close"
    >
      <section
        ref="dialog"
        class="proxy-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        :aria-describedby="visibleError ? 'proxy-editor-error' : undefined"
        tabindex="-1"
        @keydown="onKeydown"
      >
        <header>
          <h2>{{ title }}</h2>
          <button
            class="icon-button"
            type="button"
            title="关闭"
            aria-label="关闭"
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
            label="线路"
            :disabled="busy"
          />

          <fieldset class="proxy-field">
            <legend>协议</legend>
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
              <span>监听地址</span>
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
              <span>端口</span>
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
              <span>用户名</span>
              <input
                v-model="form.username"
                type="text"
                autocomplete="off"
                :disabled="busy"
              />
            </label>
            <label class="proxy-field">
              <span>密码</span>
              <input
                v-model="form.password"
                type="password"
                autocomplete="new-password"
                :placeholder="proxy?.has_password ? '留空则保留' : ''"
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
              取消
            </button>
            <button
              class="primary-button"
              type="submit"
              :disabled="busy || lines.length === 0"
            >
              {{ busy ? '保存中' : '保存' }}
            </button>
          </footer>
        </form>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.proxy-modal-backdrop {
  position: fixed;
  z-index: 120;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(16 24 40 / 42%);
}

.proxy-modal {
  width: min(560px, 100%);
  max-height: calc(100dvh - 40px);
  overflow: auto;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
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

@media (max-width: 520px) {
  .proxy-modal-backdrop {
    align-items: end;
    padding: 0 0 var(--mobile-nav-height);
  }

  .proxy-modal {
    width: 100%;
    max-height: calc(100dvh - var(--mobile-nav-height) - 8px);
    border-radius: 8px 8px 0 0;
  }

  .proxy-fields-row,
  .proxy-fields-row.is-even {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
