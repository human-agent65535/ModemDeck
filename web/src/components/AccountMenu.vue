<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { LoaderCircle, LogOut, ShieldCheck, SlidersHorizontal } from '@lucide/vue'
import { fixtureMode } from '../api/client'
import { logout as logoutSession, sessionState } from '../state/session'
import { userErrorMessage } from '../utils/errorMessage'
import PopoverTransition from './PopoverTransition.vue'

// Identity and sign-out live at the foot of the rail, as in Linear, Vercel and GitHub,
// instead of at the bottom of the settings directory.
const { t } = useI18n()
const router = useRouter()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const pending = ref(false)
const error = ref('')
const menuId = `account-menu-${useId()}`

const username = computed(() => sessionState.username || t('settings.account'))
const roleLabel = computed(() =>
  sessionState.role === 'admin'
    ? t('settings.administratorAccount')
    : t('settings.memberAccount')
)
const initial = computed(() => username.value.trim().charAt(0).toLocaleUpperCase() || '?')

function items(): HTMLElement[] {
  return Array.from(
    menu.value?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') || []
  )
}

async function openMenu(): Promise<void> {
  open.value = true
  error.value = ''
  await nextTick()
  items()[0]?.focus()
}

function close(restoreFocus = false): void {
  open.value = false
  if (restoreFocus) void nextTick(() => trigger.value?.focus())
}

function toggle(): void {
  if (open.value) close()
  else void openMenu()
}

function go(section: string): void {
  close()
  void router.push({ name: 'settings', params: { section } })
}

async function logout(): Promise<void> {
  if (pending.value) return
  pending.value = true
  error.value = ''
  try {
    await logoutSession()
    await router.replace({ name: 'login' })
  } catch (cause) {
    error.value = userErrorMessage(cause, t('settings.logoutFailed'))
  } finally {
    pending.value = false
  }
}

function onMenuKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' || event.key === 'Tab') {
    event.preventDefault()
    close(true)
    return
  }
  const options = items()
  if (options.length === 0) return
  const current = options.indexOf(document.activeElement as HTMLElement)
  let next = -1
  if (event.key === 'ArrowDown') next = (current + 1) % options.length
  else if (event.key === 'ArrowUp') next = (current - 1 + options.length) % options.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = options.length - 1
  if (next < 0) return
  event.preventDefault()
  options[next]?.focus()
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value && !root.value.contains(event.target as Node)) close()
}

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointerDown))
</script>

<template>
  <div ref="root" class="account-menu">
    <button
      ref="trigger"
      class="account-menu__trigger"
      type="button"
      aria-haspopup="menu"
      :aria-controls="menuId"
      :aria-expanded="open"
      :aria-label="t('shell.accountMenu', { name: username })"
      :title="username"
      @click="toggle"
    >
      <span class="account-menu__avatar" aria-hidden="true">{{ initial }}</span>
    </button>

    <PopoverTransition>
      <section
        v-if="open"
        :id="menuId"
        ref="menu"
        class="account-menu__panel is-up"
        role="menu"
        :aria-label="t('shell.accountMenu', { name: username })"
        @keydown="onMenuKeydown"
      >
        <header class="account-menu__identity">
          <span class="account-menu__avatar" aria-hidden="true">{{ initial }}</span>
          <span>
            <strong>{{ username }}</strong>
            <small>{{ roleLabel }}</small>
          </span>
        </header>
        <button type="button" role="menuitem" @click="go('preferences')">
          <SlidersHorizontal :size="17" aria-hidden="true" />
          {{ t('settings.system') }}
        </button>
        <button type="button" role="menuitem" @click="go('security')">
          <ShieldCheck :size="17" aria-hidden="true" />
          {{ t('settings.account') }}
        </button>
        <hr />
        <button
          type="button"
          role="menuitem"
          :disabled="fixtureMode || pending"
          @click="logout"
        >
          <LoaderCircle v-if="pending" class="spin" :size="17" aria-hidden="true" />
          <LogOut v-else :size="17" aria-hidden="true" />
          {{ t('settings.logout') }}
        </button>
        <p v-if="error" class="account-menu__error" role="alert">{{ error }}</p>
      </section>
    </PopoverTransition>
  </div>
</template>

<style scoped>
.account-menu {
  position: relative;
  display: grid;
  place-items: center;
}

.account-menu__trigger {
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  background: transparent;
  border-radius: 50%;
}

.account-menu__avatar {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  color: var(--accent-strong);
  font-size: var(--text-sm);
  font-weight: var(--weight-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.account-menu__trigger:hover .account-menu__avatar,
.account-menu__trigger[aria-expanded="true"] .account-menu__avatar {
  background: var(--accent-border);
}

.account-menu__panel {
  position: absolute;
  z-index: var(--layer-popover);
  bottom: 0;
  left: calc(100% + var(--space-2));
  display: flex;
  width: 240px;
  flex-direction: column;
  padding: var(--space-2);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-lg);
}

.account-menu__identity {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-2) var(--space-3);
}

.account-menu__identity > span:last-child {
  display: grid;
  min-width: 0;
}

.account-menu__identity strong,
.account-menu__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-menu__identity strong {
  font-size: var(--text-base);
}

.account-menu__identity small {
  color: var(--muted);
  font-size: var(--text-sm);
}

.account-menu__panel button {
  display: flex;
  min-height: 40px;
  align-items: center;
  gap: var(--space-3);
  padding: 0 var(--space-2);
  color: var(--text);
  font-size: var(--text-base);
  text-align: left;
  background: transparent;
  border-radius: var(--radius-sm);
}

.account-menu__panel button:hover:not(:disabled),
.account-menu__panel button:focus-visible {
  background: var(--surface-hover);
}

.account-menu__panel button:disabled {
  color: var(--muted);
}

.account-menu__panel hr {
  width: 100%;
  margin: var(--space-1) 0;
  border: 0;
  border-top: 1px solid var(--border);
}

.account-menu__error {
  padding: var(--space-1) var(--space-2);
  color: var(--danger);
  font-size: var(--text-sm);
}
</style>
