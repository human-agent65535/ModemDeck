<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  BellOff,
  Check,
  ChevronDown,
  LoaderCircle,
  PhoneIncoming
} from '@lucide/vue'
import {
  globalIncomingCallState,
  loadGlobalIncomingCallSettings,
  updateGlobalIncomingCallSettings
} from '../state/deviceConfiguration'

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const receiveOption = ref<HTMLButtonElement | null>(null)
const doNotDisturbOption = ref<HTMLButtonElement | null>(null)
const open = ref(false)
const menuId = `incoming-call-mode-${useId()}`
const receiveCalls = computed(() => globalIncomingCallState.data?.receive_calls)
const label = computed(() =>
  receiveCalls.value === undefined
    ? t('incomingCallMode.settings')
    : receiveCalls.value
      ? t('incomingCallMode.receive')
      : t('incomingCallMode.doNotDisturb')
)

function enabledOptions(): HTMLButtonElement[] {
  return [receiveOption.value, doNotDisturbOption.value].filter(
    (option): option is HTMLButtonElement => option !== null && !option.disabled
  )
}

async function openMenu(): Promise<void> {
  open.value = true
  if (globalIncomingCallState.status === 'idle') {
    void loadGlobalIncomingCallSettings()
  }
  await nextTick()
  const selected = receiveCalls.value === false ? doNotDisturbOption.value : receiveOption.value
  if (selected && !selected.disabled) {
    selected.focus()
    return
  }
  const first = enabledOptions()[0]
  if (first) first.focus()
  else menu.value?.focus()
}

function toggle(): void {
  if (open.value) close()
  else void openMenu()
}

function close(restoreFocus = false): void {
  open.value = false
  if (restoreFocus) void nextTick(() => trigger.value?.focus())
}

function leaveMenu(backwards: boolean): void {
  const current = trigger.value
  if (!current) {
    close()
    return
  }
  const focusable = Array.from(
    document.querySelectorAll<HTMLElement>(
      'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'
    )
  ).filter(element => element.offsetParent !== null && !menu.value?.contains(element))
  const currentIndex = focusable.indexOf(current)
  const offset = backwards ? -1 : 1
  const target =
    currentIndex >= 0 && focusable.length > 1
      ? focusable[(currentIndex + offset + focusable.length) % focusable.length]
      : undefined
  close()
  void nextTick(() => target?.focus())
}

async function choose(nextReceiveCalls: boolean): Promise<void> {
  if (globalIncomingCallState.saving) return
  if (globalIncomingCallState.data?.receive_calls === nextReceiveCalls) {
    close(true)
    return
  }
  if (await updateGlobalIncomingCallSettings(nextReceiveCalls)) close(true)
}

function onMenuKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close(true)
    return
  }
  if (event.key === 'Tab') {
    event.preventDefault()
    leaveMenu(event.shiftKey)
    return
  }

  const options = enabledOptions()
  if (options.length === 0) return
  const current = options.indexOf(document.activeElement as HTMLButtonElement)
  let next = -1
  if (event.key === 'ArrowDown') next = current < 0 ? 0 : (current + 1) % options.length
  else if (event.key === 'ArrowUp') next = current < 0 ? options.length - 1 : (current - 1 + options.length) % options.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = options.length - 1
  if (next < 0) return
  event.preventDefault()
  options[next]?.focus()
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) close()
}

onMounted(() => {
  void loadGlobalIncomingCallSettings()
  document.addEventListener('pointerdown', onDocumentPointerDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
})
</script>

<template>
  <div ref="root" class="incoming-call-mode">
    <button
      ref="trigger"
      class="incoming-call-mode__trigger"
      type="button"
      aria-haspopup="menu"
      :aria-controls="menuId"
      :aria-expanded="open"
      :disabled="globalIncomingCallState.status === 'forbidden'"
      @click="toggle"
    >
      <LoaderCircle
        v-if="globalIncomingCallState.status === 'loading'"
        class="spin"
        :size="17"
      />
      <PhoneIncoming v-else-if="receiveCalls !== false" :size="17" />
      <BellOff v-else :size="17" />
      <span>{{ label }}</span>
      <ChevronDown :size="15" />
    </button>

    <section
      v-if="open"
      :id="menuId"
      ref="menu"
      class="incoming-call-mode__menu"
      role="menu"
      tabindex="-1"
      :aria-label="t('incomingCallMode.globalPolicy')"
      @keydown="onMenuKeydown"
    >
      <header>
        <strong>{{ t('incomingCallMode.globalPolicy') }}</strong>
        <small>{{ t('incomingCallMode.globalPolicyDescription') }}</small>
      </header>

      <button
        ref="receiveOption"
        type="button"
        role="menuitemradio"
        :aria-checked="receiveCalls === true"
        :disabled="globalIncomingCallState.saving || !globalIncomingCallState.data"
        @click="choose(true)"
      >
        <span class="incoming-call-mode__option-icon"><PhoneIncoming :size="17" /></span>
        <span>
          <strong>{{ t('incomingCallMode.receive') }}</strong>
          <small>{{ t('incomingCallMode.receiveDescription') }}</small>
        </span>
        <LoaderCircle
          v-if="globalIncomingCallState.saving && receiveCalls === false"
          class="spin"
          :size="16"
        />
        <Check v-else-if="receiveCalls === true" :size="17" />
      </button>

      <button
        ref="doNotDisturbOption"
        type="button"
        role="menuitemradio"
        :aria-checked="receiveCalls === false"
        :disabled="globalIncomingCallState.saving || !globalIncomingCallState.data"
        @click="choose(false)"
      >
        <span class="incoming-call-mode__option-icon"><BellOff :size="17" /></span>
        <span>
          <strong>{{ t('incomingCallMode.doNotDisturb') }}</strong>
          <small>{{ t('incomingCallMode.doNotDisturbDescription') }}</small>
        </span>
        <LoaderCircle
          v-if="globalIncomingCallState.saving && receiveCalls === true"
          class="spin"
          :size="16"
        />
        <Check v-else-if="receiveCalls === false" :size="17" />
      </button>

      <p class="incoming-call-mode__notice">
        {{ t('incomingCallMode.deviceCapabilityNotice') }}
      </p>
      <p v-if="globalIncomingCallState.error" class="incoming-call-mode__error" role="alert">
        <AlertCircle :size="15" />
        <span>{{ globalIncomingCallState.error }}</span>
        <button type="button" @click="loadGlobalIncomingCallSettings(true)">
          {{ t('incomingCallMode.reload') }}
        </button>
      </p>
    </section>
  </div>
</template>

<style scoped>
.incoming-call-mode {
  position: relative;
}

.incoming-call-mode__trigger {
  display: inline-flex;
  min-height: 34px;
  align-items: center;
  gap: 7px;
  padding: 0 10px;
  color: var(--text);
  font-size: 12px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.incoming-call-mode__trigger:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.incoming-call-mode__menu {
  position: absolute;
  z-index: 80;
  top: calc(100% + 8px);
  right: 0;
  width: min(340px, calc(100vw - 24px));
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 12px 32px rgb(16 24 40 / 16%);
}

.incoming-call-mode__menu > header {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 14px 15px 11px;
  border-bottom: 1px solid var(--border);
}

.incoming-call-mode__menu > header strong {
  font-size: 13px;
}

.incoming-call-mode__menu small {
  color: var(--muted);
  font-size: 11px;
  line-height: 1.45;
}

.incoming-call-mode__menu > button {
  display: grid;
  width: 100%;
  min-height: 64px;
  grid-template-columns: 34px minmax(0, 1fr) 18px;
  align-items: center;
  gap: 9px;
  padding: 8px 14px;
  color: var(--text);
  text-align: left;
  background: transparent;
  border-bottom: 1px solid var(--border);
}

.incoming-call-mode__menu > button:hover:not(:disabled) {
  background: var(--surface-hover);
}

.incoming-call-mode__menu > button:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.incoming-call-mode__menu > button > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.incoming-call-mode__option-icon {
  display: inline-grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: 50%;
}

.incoming-call-mode__notice,
.incoming-call-mode__error {
  margin: 0;
  padding: 10px 14px;
  font-size: 11px;
  line-height: 1.45;
}

.incoming-call-mode__notice {
  display: flex;
  flex-direction: column;
  gap: 2px;
  color: var(--muted);
}

.incoming-call-mode__error {
  display: grid;
  grid-template-columns: 16px minmax(0, 1fr);
  gap: 7px;
  color: var(--danger);
  background: var(--danger-soft);
}

.incoming-call-mode__error button {
  grid-column: 2;
  justify-self: start;
  color: inherit;
  font-weight: 650;
  background: transparent;
}
</style>
