<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import {
  Grid3X3,
  MessageSquareText,
  Phone,
  Settings,
  TestTube2,
  UsersRound
} from '@lucide/vue'
import { fixtureMode } from '../api/client'
import { initializeCallRuntime } from '../state/call'
import { sessionState } from '../state/session'
import { openDialer } from '../state/ui'
import {
  bootstrapResource,
  loadBootstrap,
  loadContacts
} from '../state/workspace'
import CallSurface from './CallSurface.vue'
import DialerPanel from './DialerPanel.vue'
import GlobalSearch from './GlobalSearch.vue'

const route = useRoute()
const router = useRouter()
const primaryNav = [
  { name: 'contacts', label: '联系人', icon: UsersRound },
  { name: 'messages', label: '消息', icon: MessageSquareText },
  { name: 'calls', label: '通话', icon: Phone }
]

const communicationView = computed(() => Boolean(route.meta.communication))
const messageComposerVisible = computed(
  () =>
    route.name === 'messages' &&
    (typeof route.params.threadKey === 'string' || route.query.compose !== undefined)
)

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
  const result = await loadBootstrap(true)
  if (result) initializeCallRuntime()
}

onMounted(() => {
  void bootstrap()
  void loadContacts()
})
</script>

<template>
  <div class="app-shell">
    <aside class="rail" aria-label="主导航">
      <RouterLink class="brand-mark" to="/contacts" aria-label="ModemDeck">
        <span>M</span>
        <strong>Modem<br />Deck</strong>
      </RouterLink>

      <nav class="rail__primary">
        <RouterLink
          v-for="item in primaryNav"
          :key="item.name"
          class="rail-link"
          :to="{ name: item.name }"
        >
          <component :is="item.icon" :size="22" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>

      <nav class="rail__secondary">
        <RouterLink class="rail-link" :to="{ name: 'settings', params: { section: 'devices' } }">
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
      </header>

      <div v-if="bootstrapResource.status === 'error'" class="bootstrap-alert" role="alert">
        <span>{{ bootstrapResource.error }}</span>
        <button type="button" @click="bootstrap">重试</button>
      </div>

      <div class="route-stage">
        <RouterView />
      </div>
    </main>

    <button
      v-if="communicationView"
      class="dialer-fab"
      :class="{ 'dialer-fab--above-composer': messageComposerVisible }"
      type="button"
      title="打开拨号盘"
      aria-label="打开拨号盘"
      @click="openDialer()"
    >
      <Grid3X3 :size="24" />
    </button>

    <nav class="mobile-nav" aria-label="移动导航">
      <RouterLink
        v-for="item in primaryNav"
        :key="item.name"
        :to="{ name: item.name }"
      >
        <component :is="item.icon" :size="21" />
        <span>{{ item.label }}</span>
      </RouterLink>
      <RouterLink :to="{ name: 'settings', params: { section: 'devices' } }">
        <Settings :size="21" />
        <span>设置</span>
      </RouterLink>
    </nav>

    <DialerPanel />
    <CallSurface />
  </div>
</template>
