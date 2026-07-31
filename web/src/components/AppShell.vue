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
  House,
  MessageSquareText,
  Phone,
  PhoneCall,
  Settings,
  TestTube2,
  UsersRound
} from '@lucide/vue'
import { fixtureMode } from '../api/client'
import {
  callState,
  initializeCallRuntime,
  isLiveCallSession,
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
import {
  initializeRuntimeEvents,
  shutdownRuntimeEvents
} from '../state/runtimeEvents'
import { sessionState } from '../state/session'
import {
  initializeApplicationVersionChecks,
  requestApplicationVersionCheck
} from '../state/staleAssetRecovery'
import { openDialer, showCallSurface, uiState } from '../state/ui'
import {
  bootstrapResource,
  loadBootstrap,
  loadContacts
} from '../state/workspace'
import AudioSettingsMenu from './AudioSettingsMenu.vue'
import DialerPanel from './DialerPanel.vue'
import GlobalSearch from './GlobalSearch.vue'
import IncomingCallModeControl from './IncomingCallModeControl.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const settingsLanding = computed(() => 'account')
const permanentDialer = ref(false)
const occupiedLineCount = computed(() => occupiedLineIDs().size)
const activeCallPresent = computed(() =>
  callState.sessions.some(isLiveCallSession)
)
const incomingCallRinging = computed(
  () =>
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
    callState.sessions.some(
      session => session.control_state === 'owned' && session.phase === 'active'
    ) && uiState.callMinimized
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
let mountGeneration = 0
let stopApplicationVersionChecks: (() => void) | undefined
const primaryNav = computed(() => [
  { name: 'dashboard', label: t('shell.home'), icon: House },
  { name: 'contacts', label: t('shell.contacts'), icon: UsersRound },
  { name: 'messages', label: t('shell.messages'), icon: MessageSquareText },
  { name: 'calls', label: t('shell.calls'), icon: Phone },
  { name: 'recordings', label: t('shell.recordings'), icon: AudioLines },
  { name: 'traffic', label: t('shell.traffic'), icon: ChartNoAxesCombined }
])
const mobileNavBeforeDial = computed(() =>
  primaryNav.value.filter(item => ['contacts', 'messages', 'calls'].includes(item.name))
)
const mobileNavAfterDial = computed(() =>
  primaryNav.value.filter(item => ['recordings', 'traffic'].includes(item.name))
)
const mobileSettingsSection = computed(() => {
  if (route.name !== 'settings') return ''
  const section = String(route.params.section || '')
  const labels: Record<string, string> = {
    account:
      sessionState.role === 'admin'
        ? t('settings.accountManagement')
        : t('settings.account'),
    contacts: t('settings.contactsSync'),
    audio: t('settings.audio'),
    devices: t('settings.devices'),
    telegram: t('settings.telegram'),
    tls: 'HTTPS',
    diagnostics: t('settings.diagnostics')
  }
  return labels[section] || ''
})
const mobileOverviewFromSettings = computed(
  () => route.name === 'dashboard' && route.query.from === 'settings'
)
const settingsUserDetailOpen = computed(
  () =>
    route.name === 'settings' &&
    route.params.section === 'account' &&
    (typeof route.query.user === 'string' || route.query.newUser === '1')
)
const mobileShellBackVisible = computed(
  () => Boolean(mobileSettingsSection.value) || mobileOverviewFromSettings.value
)
const mobileBackTitle = computed(() =>
  settingsUserDetailOpen.value ? t('users.backToUsers') : t('settings.back')
)
const mobilePageTitle = computed(() => {
  if (route.name === 'dashboard') return t('dashboard.mobileOverview')
  if (route.name === 'settings') return mobileSettingsSection.value || t('shell.settings')
  return primaryNav.value.find(item => item.name === route.name)?.label || ''
})

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

watch(activeCallPresent, active => {
  if (!active) requestApplicationVersionCheck()
})

async function bootstrap(): Promise<void> {
  await loadBootstrap(true)
}

async function initializeWorkspaceRuntime(currentGeneration: number): Promise<void> {
  await bootstrap()
  if (currentGeneration !== mountGeneration) return
  if (!fixtureMode && !import.meta.env.DEV) {
    stopApplicationVersionChecks = initializeApplicationVersionChecks(
      window,
      document,
      () => !activeCallPresent.value
    )
  }
  initializeRuntimeEvents()
}

function syncDialerMode(): void {
  permanentDialer.value = dialerMediaQuery?.matches ?? false
}

function backToSettingsMenu(): void {
  if (settingsUserDetailOpen.value) {
    void router.push({
      name: 'settings',
      params: { section: 'account' }
    })
    return
  }
  void router.push({ name: 'settings' })
}

onMounted(() => {
  mountGeneration += 1
  const currentGeneration = mountGeneration
  void initializeBrowserAudio()
  initializeBrowserNotifications()
  initializeBrowserSounds()
  initializeCallRuntime(router)
  initializeMessageRuntime(router)
  void initializeWorkspaceRuntime(currentGeneration)
  void loadContacts()
  dialerMediaQuery = window.matchMedia('(min-width: 1101px)')
  dialerMediaQuery.addEventListener('change', syncDialerMode)
  syncDialerMode()
})

onBeforeUnmount(() => {
  mountGeneration += 1
  dialerMediaQuery?.removeEventListener('change', syncDialerMode)
  stopApplicationVersionChecks?.()
  stopApplicationVersionChecks = undefined
  shutdownRuntimeEvents()
  shutdownMessageRuntime()
  shutdownCallRuntime()
  shutdownBrowserSounds()
  shutdownDTMFAudio()
  shutdownBrowserNotifications()
  shutdownAudioDevices()
})
</script>

<template>
  <div class="app-shell">
    <aside class="rail" :aria-label="t('shell.primaryNavigation')">
      <RouterLink
        class="brand-mark"
        :to="{ name: 'dashboard' }"
        :aria-label="`ModemDeck ${t('shell.home')}`"
      >
        <span>M</span>
        <strong>Modem<br />Deck</strong>
      </RouterLink>

      <nav class="rail__primary">
        <RouterLink
          v-for="item in primaryNav"
          :key="item.name"
          class="rail-link"
          :class="{ 'is-current': route.name === item.name }"
          :to="{ name: item.name }"
          :title="item.label"
        >
          <component :is="item.icon" :size="22" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>

      <nav class="rail__secondary">
        <RouterLink
          class="rail-link"
          :class="{ 'is-current': route.name === 'settings' }"
          :to="{ name: 'settings', params: { section: settingsLanding } }"
          :title="t('shell.settings')"
        >
          <Settings :size="22" />
          <span>{{ t('shell.settings') }}</span>
        </RouterLink>
      </nav>
    </aside>

    <main class="shell-main">
      <header class="shell-header">
        <button
          v-if="mobileShellBackVisible"
          class="icon-button mobile-shell-back"
          type="button"
          :title="mobileBackTitle"
          :aria-label="mobileBackTitle"
          @click="backToSettingsMenu"
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
          <TestTube2 :size="15" />
          {{ t('shell.fixtureData') }}
        </div>
        <div class="shell-header__controls">
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
          <button
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

      <div class="route-stage">
        <RouterView />
      </div>
    </main>

    <nav class="mobile-nav" :aria-label="t('shell.mobileNavigation')">
      <RouterLink
        v-for="item in mobileNavBeforeDial"
        :key="item.name"
        :class="{ 'is-current': route.name === item.name }"
        :to="{ name: item.name }"
        :title="item.label"
        :aria-label="item.label"
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
        :class="{ 'is-current': route.name === item.name }"
        :to="{ name: item.name }"
        :title="item.label"
        :aria-label="item.label"
      >
        <component :is="item.icon" :size="21" />
        <span class="mobile-nav__label">{{ item.label }}</span>
      </RouterLink>
      <RouterLink
        :class="{ 'is-current': route.name === 'settings' }"
        :to="{ name: 'settings', params: { section: settingsLanding } }"
        :title="t('shell.settings')"
        :aria-label="t('shell.settings')"
      >
        <Settings :size="21" />
        <span class="mobile-nav__label">{{ t('shell.settings') }}</span>
      </RouterLink>
    </nav>

    <DialerPanel :permanent="permanentDialer" />
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
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  line-height: 1;
  background: var(--danger);
  border: 2px solid var(--surface);
  border-radius: 9px;
}

@media (max-width: 860px) {
  .mobile-nav {
    grid-template-columns: repeat(3, minmax(0, 1fr)) 58px repeat(3, minmax(0, 1fr));
  }
}
</style>
