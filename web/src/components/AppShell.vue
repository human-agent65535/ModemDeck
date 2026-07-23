<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import {
  AudioLines,
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
let dialerMediaQuery: MediaQueryList | undefined
const primaryNav = [
  { name: 'dashboard', label: '首页', icon: House },
  { name: 'contacts', label: '联系人', icon: UsersRound },
  { name: 'messages', label: '消息', icon: MessageSquareText },
  { name: 'calls', label: '通话', icon: Phone },
  { name: 'recordings', label: '录音', icon: AudioLines }
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
  initializeCallRuntime()
  void bootstrap()
  void loadContacts()
  dialerMediaQuery = window.matchMedia('(min-width: 1101px)')
  dialerMediaQuery.addEventListener('change', syncDialerMode)
  syncDialerMode()
})

onBeforeUnmount(() => {
  dialerMediaQuery?.removeEventListener('change', syncDialerMode)
  shutdownCallRuntime()
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
