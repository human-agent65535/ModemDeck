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
import { callState, initializeCallRuntime, shutdownCallRuntime } from '../state/call'
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
import { openDialer, uiState } from '../state/ui'
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
const permanentDialer = ref(false)
const activeCallPresent = computed(() => {
  const phase = callState.session?.phase
  return Boolean(phase && phase !== 'ended' && phase !== 'failed')
})
const incomingCallRinging = computed(
  () => callState.session?.direction === 'incoming' && callState.session.phase === 'ringing'
)
const minimizedIncomingCall = computed(
  () => incomingCallRinging.value && uiState.callMinimized
)
const minimizedActiveCall = computed(
  () => callState.session?.phase === 'active' && uiState.callMinimized
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
let dialerMediaQuery: MediaQueryList | undefined
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
    system: t('settings.system'),
    audio: t('settings.audio'),
    devices: t('settings.devices'),
    recording: t('settings.recording'),
    telegram: t('settings.telegram'),
    tls: 'HTTPS',
    diagnostics: t('settings.diagnostics')
  }
  return labels[section] || ''
})
const mobileOverviewFromSettings = computed(
  () => route.name === 'dashboard' && route.query.from === 'settings'
)
const mobileShellBackVisible = computed(
  () => Boolean(mobileSettingsSection.value) || mobileOverviewFromSettings.value
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

async function bootstrap(): Promise<void> {
  await loadBootstrap(true)
}

function syncDialerMode(): void {
  permanentDialer.value = dialerMediaQuery?.matches ?? false
}

function backToSettingsMenu(): void {
  void router.push({ name: 'settings' })
}

onMounted(() => {
  initializeBrowserNotifications()
  initializeBrowserSounds()
  initializeCallRuntime(router)
  initializeMessageRuntime(router)
  initializeRuntimeEvents()
  void bootstrap()
  void loadContacts()
  dialerMediaQuery = window.matchMedia('(min-width: 1101px)')
  dialerMediaQuery.addEventListener('change', syncDialerMode)
  syncDialerMode()
})

onBeforeUnmount(() => {
  dialerMediaQuery?.removeEventListener('change', syncDialerMode)
  shutdownRuntimeEvents()
  shutdownMessageRuntime()
  shutdownCallRuntime()
  shutdownBrowserSounds()
  shutdownDTMFAudio()
  shutdownBrowserNotifications()
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
          :to="{ name: 'settings', params: { section: 'system' } }"
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
          :title="t('settings.back')"
          :aria-label="t('settings.back')"
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
          <IncomingCallModeControl />
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
      >
        <component :is="item.icon" :size="21" />
        <span>{{ item.label }}</span>
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
        @click="openDialer()"
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
        </span>
        <span>{{ t('shell.mobileCall') }}</span>
      </button>
      <RouterLink
        v-for="item in mobileNavAfterDial"
        :key="item.name"
        :class="{ 'is-current': route.name === item.name }"
        :to="{ name: item.name }"
      >
        <component :is="item.icon" :size="21" />
        <span>{{ item.label }}</span>
      </RouterLink>
      <RouterLink
        :class="{ 'is-current': route.name === 'settings' }"
        :to="{ name: 'settings' }"
      >
        <Settings :size="21" />
        <span>{{ t('shell.settings') }}</span>
      </RouterLink>
    </nav>

    <DialerPanel :permanent="permanentDialer" />
  </div>
</template>

<style scoped>
@media (max-width: 860px) {
  .mobile-nav {
    grid-template-columns: repeat(3, minmax(0, 1fr)) 58px repeat(3, minmax(0, 1fr));
  }
}
</style>
