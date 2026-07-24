<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import {
  AudioLines,
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
import { initializeCallRuntime, shutdownCallRuntime } from '../state/call'
import {
  browserNotificationState,
  initializeBrowserNotifications,
  shutdownBrowserNotifications,
  toggleBrowserNotifications
} from '../state/browserNotifications'
import {
  initializeMessageRuntime,
  shutdownMessageRuntime
} from '../state/messageRuntime'
import {
  initializeRuntimeEvents,
  shutdownRuntimeEvents
} from '../state/runtimeEvents'
import { sessionState } from '../state/session'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  loadBootstrap,
  loadContacts
} from '../state/workspace'
import AudioSettingsMenu from './AudioSettingsMenu.vue'
import CallSurface from './CallSurface.vue'
import DialerPanel from './DialerPanel.vue'
import GlobalSearch from './GlobalSearch.vue'
import IncomingCallModeControl from './IncomingCallModeControl.vue'

const route = useRoute()
const router = useRouter()
const permanentDialer = ref(false)
const browserNotificationTitle = computed(() => {
  if (!browserNotificationState.secureContext) return '浏览器通知需要 HTTPS'
  if (!browserNotificationState.supported) return '当前浏览器不支持通知'
  if (browserNotificationState.requesting) return '正在请求浏览器通知权限'
  if (browserNotificationState.preferenceEnabled) return '关闭短信与来电通知'
  if (browserNotificationState.permission === 'denied') {
    return '通知已被浏览器阻止，请在浏览器设置中允许'
  }
  if (browserNotificationState.error) return browserNotificationState.error
  return '启用短信与来电通知'
})
let dialerMediaQuery: MediaQueryList | undefined
const primaryNav = [
  { name: 'dashboard', label: '首页', icon: House },
  { name: 'contacts', label: '联系人', icon: UsersRound },
  { name: 'messages', label: '消息', icon: MessageSquareText },
  { name: 'calls', label: '通话', icon: Phone },
  { name: 'recordings', label: '录音', icon: AudioLines },
  { name: 'traffic', label: '流量', icon: ChartNoAxesCombined }
]

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

onMounted(() => {
  initializeBrowserNotifications()
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
  shutdownBrowserNotifications()
})
</script>

<template>
  <div class="app-shell">
    <aside class="rail" aria-label="主导航">
      <RouterLink class="brand-mark" :to="{ name: 'dashboard' }" aria-label="ModemDeck 首页">
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
          :to="{ name: 'settings', params: { section: 'devices' } }"
          title="设置"
        >
          <Settings :size="22" />
          <span>设置</span>
        </RouterLink>
      </nav>
    </aside>

    <main class="shell-main">
      <header class="shell-header">
        <div class="mobile-brand">ModemDeck</div>
        <GlobalSearch />
        <div v-if="fixtureMode" class="fixture-badge" title="仅在显式开发模式下启用">
          <TestTube2 :size="15" />
          开发数据
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
            title="打开拨号栏"
            aria-label="打开拨号栏"
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
          重试
        </button>
      </div>

      <div class="route-stage">
        <RouterView />
      </div>
    </main>

    <nav class="mobile-nav" aria-label="移动导航">
      <RouterLink
        v-for="item in primaryNav"
        :key="item.name"
        :class="{ 'is-current': route.name === item.name }"
        :to="{ name: item.name }"
      >
        <component :is="item.icon" :size="21" />
        <span>{{ item.label }}</span>
      </RouterLink>
      <RouterLink
        :class="{ 'is-current': route.name === 'settings' }"
        :to="{ name: 'settings', params: { section: 'devices' } }"
      >
        <Settings :size="21" />
        <span>设置</span>
      </RouterLink>
    </nav>

    <DialerPanel :permanent="permanentDialer" />
    <CallSurface />
  </div>
</template>

<style scoped>
@media (max-width: 860px) {
  .mobile-nav {
    grid-template-columns: repeat(7, minmax(0, 1fr));
  }
}
</style>
