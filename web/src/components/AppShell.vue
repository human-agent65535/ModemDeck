<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterView, useRoute, useRouter } from 'vue-router'
import {
  AudioLines,
  ArrowLeft,
  BellOff,
  BellRing,
  ChartNoAxesCombined,
  FlaskConical,
  House,
  Menu,
  MessageSquareText,
  Phone,
  PhoneCall,
  Settings,
  UsersRound,
  X
} from '@lucide/vue'
import { fixtureCallMediaPreview, fixtureMode } from '../api/client'
import { callTestSession, initializeCallTestRuntime, shutdownCallTestRuntime } from '../state/callTestRuntime'
import {
  callState,
  initializeCallRuntime,
  occupiedLineIDs,
  shutdownCallRuntime
} from '../state/call'
import {
  initializeBrowserAudio,
  shutdownAudioDevices
} from '../state/audio'
import {
  browserNotificationState,
  initializeBrowserNotifications,
  shutdownBrowserNotifications,
  toggleBrowserNotifications
} from '../state/browserNotifications'
import {
  initializeBrowserSounds,
  shutdownBrowserSounds
} from '../state/browserSounds'
import {
  initializeMessageRuntime,
  shutdownMessageRuntime
} from '../state/messageRuntime'
import { shutdownDTMFAudio } from '../state/dtmfAudio'
import { initializeForegroundCommunicationRefresh } from '../state/foregroundCommunication'
import {
  initializeRuntimeEvents,
  shutdownRuntimeEvents
} from '../state/runtimeEvents'
import { sessionState } from '../state/session'
import {
  initializeApplicationVersionChecks
} from '../state/staleAssetRecovery'
import { openDialer, showCallSurface, uiState } from '../state/ui'
import { messageComposeRoute } from '../router/messageRoute'
import {
  bootstrapResource,
  capabilityReason,
  loadBootstrap,
  loadContacts
} from '../state/workspace'
import AccountMenu from './AccountMenu.vue'
import ActiveCallPill from './ActiveCallPill.vue'
import AudioSettingsMenu from './AudioSettingsMenu.vue'
import DialerPanel from './DialerPanel.vue'
import GlobalSearch from './GlobalSearch.vue'
import KeyboardShortcutsDialog from './KeyboardShortcutsDialog.vue'
import IncomingCallModeControl from './IncomingCallModeControl.vue'
import OverlayDialog from './OverlayDialog.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const permanentDialer = ref(false)
const nonModalDialer = ref(false)
const mobileMoreOpen = ref(false)
const occupiedLineCount = computed(() => occupiedLineIDs().size)
const activeCallPresent = computed(() =>
  Boolean(callTestSession.value || callState.session)
)
// The mobile call sheet is modal; the tab bar and dial button would compete with it.
const callSheetOpen = computed(() => activeCallPresent.value && !uiState.callMinimized)
const incomingCallRinging = computed(
  () =>
    callTestSession.value?.phase === 'ringing' ||
    !callState.owned &&
    callState.sessions.some(
      session =>
        session.direction === 'incoming' &&
        session.phase === 'ringing' &&
        session.control_state === 'available'
    )
)
const minimizedIncomingCall = computed(
  () => incomingCallRinging.value && uiState.callMinimized
)
const minimizedActiveCall = computed(
  () =>
    (callTestSession.value?.phase === 'active' || callState.sessions.some(
      session => session.control_state === 'owned' && session.phase === 'active'
    )) && uiState.callMinimized
)
const mobileCallTitle = computed(() => {
  if (incomingCallRinging.value) return t('shell.openIncomingCall')
  if (activeCallPresent.value) return t('shell.returnToCall')
  return t('shell.openDialer')
})
const browserNotificationTitle = computed(() => {
  if (!browserNotificationState.secureContext) return t('shell.notificationsRequireHTTPS')
  if (!browserNotificationState.supported) return t('shell.notificationsUnsupported')
  if (browserNotificationState.requesting) return t('shell.notificationsRequesting')
  if (browserNotificationState.preferenceEnabled) return t('shell.notificationsDisable')
  if (browserNotificationState.permission === 'denied') {
    return t('shell.notificationsDenied')
  }
  if (browserNotificationState.error) return browserNotificationState.error
  return t('shell.notificationsEnable')
})

function openMobileCall(): void {
  if (activeCallPresent.value) showCallSurface()
  else openDialer()
}
let dialerMediaQuery: MediaQueryList | undefined
let nonModalDialerMediaQuery: MediaQueryList | undefined
let mountGeneration = 0
let stopApplicationVersionChecks: (() => void) | undefined
let stopForegroundCommunicationRefresh: (() => void) | undefined
const primaryNav = computed(() => [
  { name: 'dashboard', label: t('shell.home'), icon: House },
  { name: 'contacts', label: t('shell.contacts'), icon: UsersRound },
  { name: 'messages', label: t('shell.messages'), icon: MessageSquareText },
  { name: 'calls', label: t('shell.calls'), icon: Phone },
  { name: 'recordings', label: t('shell.recordings'), icon: AudioLines },
  { name: 'traffic', label: t('shell.traffic'), icon: ChartNoAxesCombined }
])
const mobileSettingsNavItem = computed(() => ({
  name: 'settings',
  label: t('shell.settings'),
  icon: Settings,
  to: { name: 'settings' }
}))
const mobileNavBeforeDial = computed(() =>
  primaryNav.value
    .filter(item => ['dashboard', 'contacts', 'messages'].includes(item.name))
    .map(item => ({ ...item, to: { name: item.name } }))
)
const mobileNavAfterDial = computed(() =>
  [
    ...primaryNav.value
      .filter(item => ['calls', 'recordings'].includes(item.name))
      .map(item => ({ ...item, to: { name: item.name } })),
    mobileSettingsNavItem.value
  ]
)
// Every destination the rail offers stays one tap away on phones: what the tab bar
// cannot hold goes into More, never into Settings.
const mobileSecondaryNav = computed(() => [
  ...primaryNav.value
    .filter(item => ['contacts', 'recordings', 'traffic'].includes(item.name))
    .map(item => ({ ...item, to: { name: item.name } })),
  mobileSettingsNavItem.value
])
const mobileMoreCurrent = computed(() =>
  ['contacts', 'recordings', 'traffic', 'settings'].includes(String(route.name))
)
const mobileSettingsSection = computed(() => {
  if (route.name !== 'settings') return ''
  const section = String(route.params.section || '')
  const labels: Record<string, string> = {
    preferences: t('settings.system'),
    security: t('settings.account'),
    users: t('settings.users'),
    contacts: t('settings.contactsSync'),
    audio: t('settings.audio'),
    pairing: t('settings.iosApp'),
    devices: t('settings.devices'),
    telegram: t('settings.telegram'),
    connectivity: t('settings.tls'),
    diagnostics: t('settings.diagnostics'),
    about: t('settings.about')
  }
  return labels[section] || ''
})
const settingsUserDetailOpen = computed(
  () =>
    route.name === 'settings' &&
    route.params.section === 'users' &&
    (typeof route.query.user === 'string' || route.query.newUser === '1')
)
const settingsTelegramDetailOpen = computed(
  () =>
    route.name === 'settings' &&
    route.params.section === 'telegram' &&
    (typeof route.query.bot === 'string' || route.query.newBot === '1')
)
const settingsDeviceDetailOpen = computed(
  () =>
    route.name === 'settings' &&
    route.params.section === 'devices' &&
    typeof route.query.device === 'string'
)
const mobileCommunicationDetailOpen = computed(() => {
  switch (route.name) {
    case 'dashboard':
      return typeof route.query.item === 'string'
    case 'contacts':
      return typeof route.params.contactId === 'string'
    case 'messages':
      return (
        typeof route.params.threadRef === 'string' ||
        Object.prototype.hasOwnProperty.call(route.query, 'compose')
      )
    case 'calls':
    case 'recordings':
      return typeof route.query.selected === 'string'
    default:
      return false
  }
})
const mobileShellBackVisible = computed(
  () =>
    Boolean(mobileSettingsSection.value) ||
    mobileCommunicationDetailOpen.value
)
const mobileDrilldownControlsHidden = computed(() => mobileShellBackVisible.value)
const mobileBackTitle = computed(() => {
  if (settingsUserDetailOpen.value) return t('users.backToUsers')
  switch (route.name) {
    case 'dashboard':
      return t('dashboard.backHome')
    case 'contacts':
      return t('contacts.back')
    case 'messages':
      return t('messages.back')
    case 'calls':
      return t('calls.back')
    case 'recordings':
      return t('recordings.back')
    default:
      return t('settings.back')
  }
})
// Tabs, history and bookmarks name the page, the way Gmail and Linear titles do.
const documentTitle = computed(() => {
  const page =
    route.name === 'settings'
      ? [mobileSettingsSection.value, t('shell.settings')].filter(Boolean).join(' · ')
      : primaryNav.value.find(item => item.name === route.name)?.label || ''
  return page ? `${page} · ModemDeck` : 'ModemDeck'
})

watch(documentTitle, title => { document.title = title }, { immediate: true })

const mainContent = ref<HTMLElement | null>(null)

function skipToContent(): void {
  mainContent.value?.focus()
}

const shortcutsOpen = ref(false)

function editableTarget(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null
  return Boolean(
    element?.isContentEditable ||
    element?.closest?.('input, textarea, select, [contenteditable="true"]')
  )
}

// Arrow keys walk the rows of whichever list has focus, as in Mail and Gmail;
// Enter and Space already open a row.
function moveInList(event: KeyboardEvent): boolean {
  const row = (event.target as HTMLElement | null)?.closest?.<HTMLElement>('.list-item')
  const list = row?.closest<HTMLElement>('.item-list')
  if (!row || !list) return false
  const rows = Array.from(list.querySelectorAll<HTMLElement>('.list-item')).filter(
    item => item.offsetParent !== null && (item.tabIndex >= 0 || item.tagName === 'BUTTON')
  )
  const index = rows.indexOf(row)
  let next = -1
  if (event.key === 'ArrowDown') next = Math.min(rows.length - 1, index + 1)
  else if (event.key === 'ArrowUp') next = Math.max(0, index - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = rows.length - 1
  if (next < 0 || index < 0) return false
  rows[next]?.focus()
  rows[next]?.scrollIntoView({ block: 'nearest' })
  return true
}

function onShellKeydown(event: KeyboardEvent): void {
  if (event.defaultPrevented || event.isComposing || event.metaKey || event.ctrlKey || event.altKey) return
  if (moveInList(event)) {
    event.preventDefault()
    return
  }
  if (editableTarget(event.target) || document.querySelector('[aria-modal="true"]')) return
  if (event.key === '?') {
    shortcutsOpen.value = true
  } else if (event.key.toLowerCase() === 'c' && !event.shiftKey && !capabilityReason('message')) {
    void router.push(messageComposeRoute({ recipient: '', name: '', lineKey: '' }))
  } else if (event.key.toLowerCase() === 'd' && !event.shiftKey && !capabilityReason('dial')) {
    openDialer()
  } else {
    return
  }
  event.preventDefault()
}

const mobilePageTitle = computed(() => {
  if (route.name === 'settings') return mobileSettingsSection.value || t('shell.settings')
  return primaryNav.value.find(item => item.name === route.name)?.label || ''
})

function mobileNavItemCurrent(name: string): boolean {
  return route.name === name
}

function toggleMobileMore(): void {
  if (mobileMoreOpen.value) {
    closeMobileMore()
    return
  }
  mobileMoreOpen.value = true
}

function closeMobileMore(): void {
  mobileMoreOpen.value = false
}

watch(
  () => sessionState.status,
  status => {
    if (status !== 'anonymous') return
    void router.replace({
      name: 'login',
      query: { redirect: route.fullPath }
    })
  }
)

watch(
  () => route.fullPath,
  () => {
    mobileMoreOpen.value = false
  }
)

async function bootstrap(): Promise<void> {
  await loadBootstrap(true)
}

async function initializeWorkspaceRuntime(currentGeneration: number): Promise<void> {
  await bootstrap()
  if (currentGeneration !== mountGeneration) return
  if (!fixtureMode && !import.meta.env.DEV) {
    stopApplicationVersionChecks = initializeApplicationVersionChecks(
      window,
      document
    )
  }
  initializeRuntimeEvents()
}

function syncDialerMode(): void {
  permanentDialer.value = dialerMediaQuery?.matches ?? false
  nonModalDialer.value = nonModalDialerMediaQuery?.matches ?? false
}

function handleMobileBack(): void {
  if (settingsUserDetailOpen.value) {
    void router.push({
      name: 'settings',
      params: { section: 'users' }
    })
    return
  }
  if (settingsTelegramDetailOpen.value) {
    void router.push({
      name: 'settings',
      params: { section: 'telegram' }
    })
    return
  }
  if (settingsDeviceDetailOpen.value) {
    void router.push({
      name: 'settings',
      params: { section: 'devices' }
    })
    return
  }
  const query = { ...route.query }
  switch (route.name) {
    case 'dashboard':
      delete query.item
      void router.push({ name: 'dashboard', query })
      return
    case 'contacts':
      void router.push({ name: 'contacts' })
      return
    case 'messages':
      delete query.compose
      void router.push({ name: 'messages', query })
      return
    case 'calls':
      delete query.selected
      void router.push({ name: 'calls', query })
      return
    case 'recordings':
      delete query.selected
      void router.push({ name: 'recordings', query })
      return
    default:
      void router.push({ name: 'settings' })
  }
}

onMounted(() => {
  mountGeneration += 1
  const currentGeneration = mountGeneration
  if (!fixtureCallMediaPreview) void initializeBrowserAudio()
  initializeBrowserNotifications()
  initializeBrowserSounds()
  initializeCallRuntime(router)
  initializeCallTestRuntime()
  initializeMessageRuntime(router)
  stopForegroundCommunicationRefresh = initializeForegroundCommunicationRefresh()
  void initializeWorkspaceRuntime(currentGeneration)
  void loadContacts()
  document.addEventListener('keydown', onShellKeydown)
  dialerMediaQuery = window.matchMedia('(min-width: 1480px)')
  nonModalDialerMediaQuery = window.matchMedia('(min-width: 641px)')
  dialerMediaQuery.addEventListener('change', syncDialerMode)
  nonModalDialerMediaQuery.addEventListener('change', syncDialerMode)
  syncDialerMode()
})

onBeforeUnmount(() => {
  mountGeneration += 1
  document.removeEventListener('keydown', onShellKeydown)
  dialerMediaQuery?.removeEventListener('change', syncDialerMode)
  nonModalDialerMediaQuery?.removeEventListener('change', syncDialerMode)
  stopApplicationVersionChecks?.()
  stopApplicationVersionChecks = undefined
  stopForegroundCommunicationRefresh?.()
  stopForegroundCommunicationRefresh = undefined
  shutdownRuntimeEvents()
  shutdownMessageRuntime()
  shutdownCallRuntime()
  shutdownCallTestRuntime()
  shutdownBrowserSounds()
  shutdownDTMFAudio()
  shutdownBrowserNotifications()
  shutdownAudioDevices()
  document.title = 'ModemDeck'
})
</script>

<template>
  <div
    class="app-shell"
    :class="{
      'has-call-sheet': callSheetOpen,
      'is-communication-detail': mobileCommunicationDetailOpen
    }"
  >
    <a class="skip-link" href="#main-content" @click.prevent="skipToContent">
      {{ t('shell.skipToContent') }}
    </a>
    <aside class="rail">
      <RouterLink
        class="brand-mark"
        :to="{ name: 'dashboard' }"
        :aria-label="`ModemDeck ${t('shell.home')}`"
      >
        <span>M</span>
        <strong>Modem<br />Deck</strong>
      </RouterLink>

      <nav class="rail__primary" :aria-label="t('shell.primaryNavigation')">
        <RouterLink
          v-for="item in primaryNav"
          :key="item.name"
          class="rail-link"
          :class="{ 'is-current': route.name === item.name }"
          :to="{ name: item.name }"
          :title="item.label"
          :aria-current="route.name === item.name ? 'page' : undefined"
        >
          <component :is="item.icon" :size="22" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>

      <nav class="rail__secondary" :aria-label="t('shell.secondaryNavigation')">
        <RouterLink
          class="rail-link"
          :class="{ 'is-current': route.name === 'settings' }"
          :to="{ name: 'settings' }"
          :title="t('shell.settings')"
          :aria-current="route.name === 'settings' ? 'page' : undefined"
        >
          <Settings :size="22" />
          <span>{{ t('shell.settings') }}</span>
        </RouterLink>
      </nav>
      <AccountMenu class="rail__account" />
    </aside>

    <main class="shell-main">
      <header class="shell-header">
        <button
          v-if="mobileShellBackVisible"
          class="icon-button mobile-shell-back"
          type="button"
          :title="mobileBackTitle"
          :aria-label="mobileBackTitle"
          @click="handleMobileBack"
        >
          <ArrowLeft :size="20" />
        </button>
        <div class="mobile-brand">{{ mobilePageTitle }}</div>
        <GlobalSearch />
        <div
          v-if="fixtureMode"
          class="fixture-badge"
          :title="t('shell.fixtureDataHint')"
        >
          <FlaskConical :size="15" aria-hidden="true" />
          <span class="fixture-badge__label">{{ t('shell.fixtureData') }}</span>
        </div>
        <div
          class="shell-header__controls"
          :class="{ 'is-hidden-on-mobile': mobileDrilldownControlsHidden }"
        >
          <IncomingCallModeControl v-if="sessionState.role === 'admin'" />
          <button
            class="icon-button"
            :class="{ 'is-active': browserNotificationState.active }"
            type="button"
            :disabled="
              browserNotificationState.requesting ||
              !browserNotificationState.secureContext ||
              !browserNotificationState.supported ||
              (browserNotificationState.permission === 'denied' &&
                !browserNotificationState.preferenceEnabled)
            "
            :title="browserNotificationTitle"
            :aria-label="browserNotificationTitle"
            :aria-pressed="browserNotificationState.active"
            @click="toggleBrowserNotifications"
          >
            <BellRing v-if="browserNotificationState.active" :size="19" />
            <BellOff v-else :size="19" />
          </button>
          <AudioSettingsMenu />
          <ActiveCallPill
            v-if="activeCallPresent && uiState.callMinimized"
            :ringing="incomingCallRinging"
          />
          <button
            v-else
            class="icon-button shell-dialer-toggle"
            type="button"
            :title="t('shell.openDialer')"
            :aria-label="t('shell.openDialer')"
            @click="openDialer()"
          >
            <PhoneCall :size="19" />
          </button>
        </div>
      </header>

      <div
        v-if="bootstrapResource.status === 'error' || bootstrapResource.status === 'forbidden'"
        class="bootstrap-alert"
        role="alert"
      >
        <span>{{ bootstrapResource.error }}</span>
        <button v-if="bootstrapResource.status === 'error'" type="button" @click="bootstrap">
          {{ t('common.retry') }}
        </button>
      </div>

      <div
        id="main-content"
        ref="mainContent"
        class="route-stage"
        tabindex="-1"
      >
        <RouterView v-slot="{ Component }">
          <Transition name="route-view">
            <div :key="String(route.name || route.path)" class="route-view">
              <component :is="Component" />
            </div>
          </Transition>
        </RouterView>
      </div>
    </main>

    <OverlayDialog
      :open="mobileMoreOpen"
      size="large"
      labelledby="mobile-more-title"
      initial-focus=".mobile-more-links a"
      @close="closeMobileMore"
    >
      <div id="mobile-more-menu" class="mobile-more-sheet">
          <header class="mobile-more-sheet__header">
            <h2 id="mobile-more-title">{{ t('common.more') }}</h2>
            <button
              class="icon-button"
              type="button"
              :title="t('common.close')"
              :aria-label="t('common.close')"
              @click="closeMobileMore"
            >
              <X :size="20" />
            </button>
          </header>
          <nav class="mobile-more-links" :aria-label="t('common.more')">
            <RouterLink
              v-for="item in mobileSecondaryNav"
              :key="item.name"
              :class="{ 'is-current': mobileNavItemCurrent(item.name) }"
              :to="item.to"
              @click="closeMobileMore"
            >
              <component :is="item.icon" :size="21" />
              <span>{{ item.label }}</span>
            </RouterLink>
          </nav>
      </div>
    </OverlayDialog>

    <nav class="mobile-nav" :aria-label="t('shell.mobileNavigation')">
      <RouterLink
        v-for="item in mobileNavBeforeDial"
        :key="item.name"
        :class="{
          'is-current': mobileNavItemCurrent(item.name),
          'mobile-nav__overflow': item.name === 'contacts'
        }"
        :to="item.to"
        :title="item.label"
        :aria-label="item.label"
        :aria-current="mobileNavItemCurrent(item.name) ? 'page' : undefined"
      >
        <component :is="item.icon" :size="21" />
        <span class="mobile-nav__label">{{ item.label }}</span>
      </RouterLink>
      <button
        class="mobile-nav__dial"
        :class="{
          'is-current': uiState.dialerOpen || activeCallPresent,
          'is-ringing': minimizedIncomingCall,
          'is-active-call': minimizedActiveCall
        }"
        type="button"
        :title="mobileCallTitle"
        :aria-label="mobileCallTitle"
        :aria-pressed="uiState.dialerOpen || activeCallPresent"
        @click="openMobileCall"
      >
        <span class="mobile-nav__dial-icon">
          <span
            v-if="minimizedIncomingCall"
            class="mobile-nav__ring-wave mobile-nav__ring-wave--inner"
            aria-hidden="true"
          />
          <span
            v-if="minimizedIncomingCall"
            class="mobile-nav__ring-wave mobile-nav__ring-wave--outer"
            aria-hidden="true"
          />
          <PhoneCall :size="23" />
          <span
            v-if="occupiedLineCount > 0"
            class="mobile-nav__call-count"
            aria-hidden="true"
          >
            {{ occupiedLineCount }}
          </span>
        </span>
        <span class="mobile-nav__label">{{ t('shell.mobileCall') }}</span>
      </button>
      <RouterLink
        v-for="item in mobileNavAfterDial"
        :key="item.name"
        :class="{
          'is-current': mobileNavItemCurrent(item.name),
          'mobile-nav__overflow': item.name !== 'calls'
        }"
        :to="item.to"
        :title="item.label"
        :aria-label="item.label"
        :aria-current="mobileNavItemCurrent(item.name) ? 'page' : undefined"
      >
        <component :is="item.icon" :size="21" />
        <span class="mobile-nav__label">{{ item.label }}</span>
      </RouterLink>
      <button
        class="mobile-nav__more"
        :class="{ 'is-current': mobileMoreCurrent }"
        type="button"
        :title="t('common.more')"
        :aria-label="t('common.more')"
        aria-controls="mobile-more-menu"
        :aria-expanded="mobileMoreOpen"
        @click="toggleMobileMore"
      >
        <Menu :size="21" />
        <span class="mobile-nav__label">{{ t('common.more') }}</span>
      </button>
    </nav>

    <KeyboardShortcutsDialog :open="shortcutsOpen" @close="shortcutsOpen = false" />

    <DialerPanel :permanent="permanentDialer"
      :non-modal="nonModalDialer"
    />
  </div>
</template>

<style scoped>
.mobile-nav__dial-icon {
  position: relative;
}

.mobile-nav__call-count {
  position: absolute;
  top: -7px;
  right: -10px;
  display: inline-flex;
  min-width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  padding: 0 4px;
  color: var(--on-accent);
  font-size: var(--text-2xs);
  font-weight: 800;
  line-height: 1;
  background: var(--danger);
  border: 2px solid var(--surface);
  border-radius: 9px;
}

@media (max-width: 640px) {
  .mobile-more-sheet {
    width: 100%;
    max-height: min(480px, calc(100dvh - var(--mobile-nav-height) - 20px));
    overflow-y: auto;
  }

  .mobile-more-sheet__header {
    display: flex;
    min-height: 58px;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px 8px 18px;
    border-bottom: 1px solid var(--border);
  }

  .mobile-more-sheet__header h2 {
    margin: 0;
    font-size: 17px;
  }

  .mobile-more-links {
    display: grid;
    padding: 6px 10px 14px;
  }

  .mobile-more-links a {
    display: flex;
    min-height: 52px;
    align-items: center;
    gap: 13px;
    padding: 0 12px;
    color: var(--text);
    font-size: 14px;
    font-weight: 650;
    border-radius: 10px;
    transition:
      color var(--motion-fast) var(--ease-standard),
      background var(--motion-fast) var(--ease-standard),
      transform var(--motion-fast) var(--ease-standard);
  }

  .mobile-more-links a:hover,
  .mobile-more-links a:focus-visible,
  .mobile-more-links a.is-current {
    color: var(--accent-strong);
    background: var(--accent-soft);
  }

  .mobile-more-links a:active {
    transform: scale(0.985);
  }
}
</style>
