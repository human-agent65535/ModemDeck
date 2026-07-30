<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  ContactRound,
  House,
  Info,
  LoaderCircle,
  LogOut,
  RadioTower,
  Send,
  ShieldCheck,
  UserRound,
  Volume2
} from '@lucide/vue'
import AccountSettingsPanel from '../components/AccountSettingsPanel.vue'
import AudioSettingsForm from '../components/AudioSettingsForm.vue'
import AboutSettingsPanel from '../components/AboutSettingsPanel.vue'
import ContactSyncSettings from '../components/ContactSyncSettings.vue'
import StatePanel from '../components/StatePanel.vue'
import DeviceConfigurationPanel from '../components/DeviceConfigurationPanel.vue'
import DiagnosticsPanel from '../components/DiagnosticsPanel.vue'
import TelegramSettingsForm from '../components/TelegramSettingsForm.vue'
import TLSSettingsForm from '../components/TLSSettingsForm.vue'
import UserSettingsPanel from '../components/UserSettingsPanel.vue'
import { fixtureMode } from '../api/client'
import { logout as logoutSession, sessionState } from '../state/session'
import {
  bootstrapResource,
  devicesResource,
  loadBootstrap,
  loadDevices,
  presentModuleLines
} from '../state/workspace'
import { isRegisteredNetwork } from '../utils/operatorNetwork'

type SettingsSection =
  | 'account'
  | 'contacts'
  | 'audio'
  | 'devices'
  | 'telegram'
  | 'tls'
  | 'diagnostics'
  | 'about'

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
const overviewSummary = computed(() =>
  t('dashboard.modulesOnline', {
    online: onlineModules.value,
    total: presentModules.value.length
  })
)
const sections = computed<Array<{
  id: SettingsSection
  label: string
  description: string
  icon: typeof RadioTower
}>>(() => {
  const personal = [
    {
      id: 'account' as const,
      label:
        sessionState.role === 'admin'
          ? t('settings.accountManagement')
          : t('settings.account'),
      description:
        sessionState.role === 'admin'
          ? t('settings.accountManagementDescription')
          : t('settings.accountDescription'),
      icon: UserRound
    },
    {
      id: 'contacts' as const,
      label: t('settings.contactsSync'),
      description: t('settings.contactsSyncDescription'),
      icon: ContactRound
    },
    {
      id: 'audio' as const,
      label: t('settings.audio'),
      description: t('settings.audioDescription'),
      icon: Volume2
    },
    {
      id: 'devices' as const,
      label: t('settings.devices'),
      description: t('settings.devicesDescription'),
      icon: RadioTower
    }
  ]
  const about = {
    id: 'about' as const,
    label: t('settings.about'),
    description: t('settings.aboutDescription'),
    icon: Info
  }
  if (sessionState.mustChangePassword) return personal.slice(0, 1)
  if (sessionState.role !== 'admin') return [...personal, about]
  return [
    ...personal,
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
    },
    about
  ]
})
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

watch(
  () => route.params.section,
  value => {
    const section = String(value || '')
    if (!section || sections.value.some(item => item.id === section)) return
    void router.replace({ name: 'settings', params: { section: 'account' } })
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
  if (sessionState.mustChangePassword) return
  void Promise.all([loadBootstrap(), loadDevices()])
})
</script>

<template>
  <section class="workspace settings-workspace" :class="{ 'has-selection': selectedSection }">
    <aside class="list-pane settings-list-pane">
      <header class="pane-header"><h1>{{ t('settings.title') }}</h1></header>
      <div class="item-list settings-list">
        <button
          v-if="!sessionState.mustChangePassword"
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
          <strong>{{ sessionState.username || t('settings.account') }}</strong>
          <small>
            {{
              sessionState.role === 'admin'
                ? t('settings.administratorAccount')
                : t('settings.memberAccount')
            }}
          </small>
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

        <div v-if="selectedSection === 'account'" class="settings-content">
          <UserSettingsPanel
            v-if="sessionState.role === 'admin' && !sessionState.mustChangePassword"
          />
          <AccountSettingsPanel v-else />
        </div>

        <div v-else-if="selectedSection === 'contacts'" class="settings-content">
          <ContactSyncSettings />
        </div>

        <div v-else-if="selectedSection === 'audio'" class="settings-content">
          <AudioSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'devices'" class="settings-content">
          <DeviceConfigurationPanel />
        </div>

        <div v-else-if="selectedSection === 'telegram'" class="settings-content">
          <TelegramSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'tls'" class="settings-content">
          <TLSSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'diagnostics'" class="settings-content">
          <DiagnosticsPanel />
        </div>

        <div v-else class="settings-content">
          <AboutSettingsPanel />
        </div>
      </template>

      <StatePanel v-else state="empty" :title="t('settings.selectSetting')" />
    </article>
  </section>
</template>

<style scoped>
.settings-content {
  container-type: inline-size;
}
</style>
