<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  Circle,
  House,
  LoaderCircle,
  LogOut,
  Languages,
  RadioTower,
  Send,
  ShieldCheck,
  UserRound,
  Volume2
} from '@lucide/vue'
import AudioSettingsForm from '../components/AudioSettingsForm.vue'
import StatePanel from '../components/StatePanel.vue'
import DeviceConfigurationPanel from '../components/DeviceConfigurationPanel.vue'
import DiagnosticsPanel from '../components/DiagnosticsPanel.vue'
import RecordingSettingsForm from '../components/RecordingSettingsForm.vue'
import SystemSettingsForm from '../components/SystemSettingsForm.vue'
import TelegramSettingsForm from '../components/TelegramSettingsForm.vue'
import TLSSettingsForm from '../components/TLSSettingsForm.vue'
import { fixtureMode } from '../api/client'
import { logout as logoutSession, sessionState } from '../state/session'
import {
  bootstrapResource,
  callsResource,
  devicesResource,
  loadBootstrap,
  loadCalls,
  loadDevices,
  loadThreads,
  presentModuleLines,
  threadsResource
} from '../state/workspace'
import { isRegisteredNetwork } from '../utils/operatorNetwork'

type SettingsSection =
  | 'system'
  | 'audio'
  | 'devices'
  | 'recording'
  | 'telegram'
  | 'tls'
  | 'diagnostics'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const logoutPending = ref(false)
const logoutError = ref('')
const lines = computed(() => bootstrapResource.data?.lines || [])
const presentModules = computed(() =>
  presentModuleLines(lines.value, devicesResource.data)
)
const onlineModules = computed(
  () => presentModules.value.filter(line => isRegisteredNetwork(line)).length
)
const attentionCount = computed(
  () =>
    threadsResource.data.reduce((total, thread) => total + thread.unread_count, 0) +
    callsResource.data.filter(call => call.missed).length
)
const overviewSummary = computed(() => {
  const online = t('dashboard.modulesOnline', {
    online: onlineModules.value,
    total: presentModules.value.length
  })
  return attentionCount.value
    ? `${online} · ${t('dashboard.attention', { count: attentionCount.value })}`
    : online
})
const sections = computed<Array<{
  id: SettingsSection
  label: string
  description: string
  icon: typeof RadioTower
}>>(() => [
  {
    id: 'system',
    label: t('settings.system'),
    description: t('settings.systemDescription'),
    icon: Languages
  },
  {
    id: 'audio',
    label: t('settings.audio'),
    description: t('settings.audioDescription'),
    icon: Volume2
  },
  {
    id: 'devices',
    label: t('settings.devices'),
    description: t('settings.devicesDescription'),
    icon: RadioTower
  },
  {
    id: 'recording',
    label: t('settings.recording'),
    description: t('settings.recordingDescription'),
    icon: Circle
  },
  {
    id: 'telegram',
    label: t('settings.telegram'),
    description: t('settings.telegramDescription'),
    icon: Send
  },
  {
    id: 'tls',
    label: 'HTTPS',
    description: t('settings.tlsDescription'),
    icon: ShieldCheck
  },
  {
    id: 'diagnostics',
    label: t('settings.diagnostics'),
    description: t('settings.diagnosticsDescription'),
    icon: Activity
  }
])
const selectedSection = computed<SettingsSection | ''>(() => {
  const value = String(route.params.section || '')
  return sections.value.some(section => section.id === value) ? (value as SettingsSection) : ''
})
const currentTitle = computed(
  () =>
    sections.value.find(section => section.id === selectedSection.value)?.label ||
    t('settings.title')
)

watch(
  selectedSection,
  section => {
    if (section === 'devices') void loadBootstrap()
  },
  { immediate: true }
)

function openSection(section: SettingsSection): void {
  void router.push({ name: 'settings', params: { section } })
}

function backToSettings(): void {
  void router.push({ name: 'settings', params: { section: '' } })
}

function openDashboard(): void {
  void router.push({
    name: 'dashboard',
    query: { item: 'overview', from: 'settings' }
  })
}

async function logout(): Promise<void> {
  if (logoutPending.value) return
  logoutPending.value = true
  logoutError.value = ''
  try {
    await logoutSession()
    await router.replace({ name: 'login' })
  } catch (error) {
    logoutError.value =
      error instanceof Error ? error.message : t('settings.logoutFailed')
  } finally {
    logoutPending.value = false
  }
}

onMounted(() => {
  void Promise.all([loadBootstrap(), loadDevices(), loadThreads(), loadCalls()])
})
</script>

<template>
  <section class="workspace settings-workspace" :class="{ 'has-selection': selectedSection }">
    <aside class="list-pane settings-list-pane">
      <header class="pane-header"><h1>{{ t('settings.title') }}</h1></header>
      <div class="item-list settings-list">
        <button
          class="list-item settings-overview-link"
          type="button"
          @click="openDashboard"
        >
          <span class="settings-icon"><House :size="19" /></span>
          <span class="list-item__content">
            <strong>{{ t('dashboard.mobileOverview') }}</strong>
            <small>{{ overviewSummary }}</small>
          </span>
        </button>
        <button
          v-for="section in sections"
          :key="section.id"
          class="list-item"
          :class="{ 'is-selected': selectedSection === section.id }"
          type="button"
          @click="openSection(section.id)"
        >
          <span class="settings-icon"><component :is="section.icon" :size="19" /></span>
          <span class="list-item__content">
            <strong>{{ section.label }}</strong>
            <small>{{ section.description }}</small>
          </span>
        </button>
      </div>
      <footer v-if="!fixtureMode" class="settings-account">
        <span class="settings-account__icon"><UserRound :size="19" /></span>
        <span class="settings-account__identity">
          <strong>{{ sessionState.username || t('settings.administrator') }}</strong>
          <small>{{ t('settings.administratorAccount') }}</small>
          <small v-if="logoutError" class="settings-account__error" role="alert">
            {{ logoutError }}
          </small>
        </span>
        <button
          class="icon-button"
          type="button"
          :title="t('settings.logout')"
          :aria-label="t('settings.logout')"
          :disabled="logoutPending"
          @click="logout"
        >
          <LoaderCircle v-if="logoutPending" class="spin" :size="19" />
          <LogOut v-else :size="19" />
        </button>
      </footer>
    </aside>

    <article class="detail-pane settings-detail-pane">
      <template v-if="selectedSection">
        <header class="settings-detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            :title="t('settings.back')"
            @click="backToSettings"
          >
            <ArrowLeft :size="20" />
          </button>
          <h2>{{ currentTitle }}</h2>
        </header>

        <div v-if="selectedSection === 'system'" class="settings-content">
          <SystemSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'audio'" class="settings-content">
          <AudioSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'devices'" class="settings-content">
          <DeviceConfigurationPanel />
        </div>

        <div v-else-if="selectedSection === 'recording'" class="settings-content">
          <RecordingSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'telegram'" class="settings-content">
          <TelegramSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'tls'" class="settings-content">
          <TLSSettingsForm />
        </div>

        <div v-else class="settings-content">
          <DiagnosticsPanel />
        </div>
      </template>

      <StatePanel v-else state="empty" :title="t('settings.selectSetting')" />
    </article>
  </section>
</template>
