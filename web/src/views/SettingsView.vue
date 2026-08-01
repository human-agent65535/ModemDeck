<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  ChartNoAxesCombined,
  ContactRound,
  Globe2,
  House,
  Info,
  KeyRound,
  LoaderCircle,
  LogOut,
  RadioTower,
  Send,
  ShieldCheck,
  SlidersHorizontal,
  UserRound,
  Volume2
} from '@lucide/vue'
import StatePanel from '../components/StatePanel.vue'
import PageContentFrame from '../components/PageContentFrame.vue'
import SettingsAsyncBoundary from '../components/settings/SettingsAsyncBoundary.vue'
import SettingsContentTransition from '../components/settings/SettingsContentTransition.vue'
import {
  settingsSectionGroup,
  visibleSettingsSectionIDs
} from '../components/settings/settingsNavigation'
import type {
  SettingsSection,
  SettingsSectionGroup
} from '../components/settings/settingsNavigation'
import {
  settingsSkeletonShape,
  type SettingsSkeletonShape
} from '../components/settings/settingsSkeleton'
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

type SettingsSectionDefinition = {
  id: SettingsSection
  label: string
  description: string
  icon: typeof RadioTower
}

const loadAccountPreferencesPanel = () =>
  import('../components/AccountPreferencesPanel.vue')
const loadSecuritySettingsPanel = () =>
  import('../components/SecuritySettingsPanel.vue')
const loadUserSettingsPanel = () => import('../components/UserSettingsPanel.vue')
const loadAudioSettingsForm = () => import('../components/AudioSettingsForm.vue')
const loadAboutSettingsPanel = () => import('../components/AboutSettingsPanel.vue')
const loadContactSyncSettings = () => import('../components/ContactSyncSettings.vue')
const loadDeviceConfigurationPanel = () =>
  import('../components/DeviceConfigurationPanel.vue')
const loadDiagnosticsPanel = () => import('../components/DiagnosticsPanel.vue')
const loadPairingSettingsPanel = () =>
  import('../components/PairingSettingsPanel.vue')
const loadConnectivitySettingsPanel = () =>
  import('../components/ConnectivitySettingsPanel.vue')
const loadTelegramSettingsForm = () => import('../components/TelegramSettingsForm.vue')

const AccountPreferencesPanel = defineAsyncComponent(loadAccountPreferencesPanel)
const SecuritySettingsPanel = defineAsyncComponent(loadSecuritySettingsPanel)
const UserSettingsPanel = defineAsyncComponent(loadUserSettingsPanel)
const AudioSettingsForm = defineAsyncComponent(loadAudioSettingsForm)
const AboutSettingsPanel = defineAsyncComponent(loadAboutSettingsPanel)
const ContactSyncSettings = defineAsyncComponent(loadContactSyncSettings)
const DeviceConfigurationPanel = defineAsyncComponent(loadDeviceConfigurationPanel)
const DiagnosticsPanel = defineAsyncComponent(loadDiagnosticsPanel)
const PairingSettingsPanel = defineAsyncComponent(loadPairingSettingsPanel)
const ConnectivitySettingsPanel = defineAsyncComponent(
  loadConnectivitySettingsPanel
)
const TelegramSettingsForm = defineAsyncComponent(loadTelegramSettingsForm)

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const logoutPending = ref(false)
const logoutError = ref('')
const isAdmin = computed(() => sessionState.role === 'admin')
const canManageExternalAccess = isAdmin
const canPairIOS = computed(() => sessionState.iosPairingEnabled)
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
const sectionCatalog = computed<SettingsSectionDefinition[]>(() => [
  {
    id: 'preferences',
    label: t('settings.system'),
    description: t('settings.systemDescription'),
    icon: SlidersHorizontal
  },
  {
    id: 'security',
    label: t('settings.account'),
    description: t('settings.accountDescription'),
    icon: ShieldCheck
  },
  {
    id: 'audio',
    label: t('settings.audio'),
    description: t('settings.audioDescription'),
    icon: Volume2
  },
  {
    id: 'pairing',
    label: t('settings.iosApp'),
    description: t('settings.iosAppDescription'),
    icon: KeyRound
  },
  {
    id: 'contacts',
    label: t('settings.contactsSync'),
    description: t('settings.contactsSyncDescription'),
    icon: ContactRound
  },
  {
    id: 'users',
    label: t('settings.users'),
    description: t('settings.usersDescription'),
    icon: UserRound
  },
  {
    id: 'devices',
    label: t('settings.devices'),
    description: t('settings.devicesDescription'),
    icon: RadioTower
  },
  {
    id: 'telegram',
    label: t('settings.telegram'),
    description: t('settings.telegramDescription'),
    icon: Send
  },
  {
    id: 'connectivity',
    label: t('settings.tls'),
    description: t('settings.tlsDescription'),
    icon: Globe2
  },
  {
    id: 'diagnostics',
    label: t('settings.diagnostics'),
    description: t('settings.diagnosticsDescription'),
    icon: Activity
  },
  {
    id: 'about',
    label: t('settings.about'),
    description: t('settings.aboutDescription'),
    icon: Info
  }
])
const sections = computed<SettingsSectionDefinition[]>(() => {
  const catalog = new Map(sectionCatalog.value.map(section => [section.id, section]))
  return visibleSettingsSectionIDs({
    isAdmin: isAdmin.value,
    canPairIOS: canPairIOS.value
  })
    .map(sectionID => catalog.get(sectionID))
    .filter((section): section is SettingsSectionDefinition => section !== undefined)
})
const sectionGroups = computed(() =>
  (['personal', 'management'] as const).map((group: SettingsSectionGroup) => ({
    id: group,
    label: t(`settings.${group}`),
    sections: sections.value.filter(section => settingsSectionGroup(section.id) === group)
  }))
)
const selectedSection = computed<SettingsSection | ''>(() => {
  const value = String(route.params.section || '')
  return sections.value.some(section => section.id === value) ? (value as SettingsSection) : ''
})
const currentTitle = computed(
  () =>
    sections.value.find(section => section.id === selectedSection.value)?.label ||
    t('settings.title')
)
const settingsLoadingShape = computed<SettingsSkeletonShape>(() =>
  settingsSkeletonShape(selectedSection.value)
)
const settingsSkeletonHasSelection = computed(() => {
  if (selectedSection.value === 'users') {
    return typeof route.query.user === 'string' || route.query.newUser === '1'
  }
  if (selectedSection.value === 'telegram') {
    return typeof route.query.bot === 'string' || route.query.newBot === '1'
  }
  return false
})

watch(
  selectedSection,
  section => {
    if (section === 'devices') void loadBootstrap()
  },
  { immediate: true }
)

watch(
  [() => route.params.section, () => sessionState.role, canPairIOS],
  ([value]) => {
    const section = String(value || '')
    let destination: SettingsSection | '' = ''
    if (section === 'account') {
      destination = sessionState.role === 'admin' ? 'users' : 'preferences'
    } else if (section === 'external-access') {
      destination = canManageExternalAccess.value
        ? 'connectivity'
        : canPairIOS.value
          ? 'pairing'
          : ''
    } else if (section === 'web-certificate') {
      destination = canManageExternalAccess.value ? 'connectivity' : ''
    } else if (section === 'ios') {
      destination = canPairIOS.value ? 'pairing' : ''
    }
    if (!destination) return
    void router.replace({
      name: 'settings',
      params: { section: destination },
      query: route.query
    })
  },
  { immediate: true }
)

function openSection(section: SettingsSection): void {
  void router.push({ name: 'settings', params: { section } })
}

function preloadSection(section: SettingsSection): void {
  const loader = (() => {
    switch (section) {
      case 'preferences':
        return loadAccountPreferencesPanel
      case 'security':
        return loadSecuritySettingsPanel
      case 'users':
        return loadUserSettingsPanel
      case 'contacts':
        return loadContactSyncSettings
      case 'audio':
        return loadAudioSettingsForm
      case 'devices':
        return loadDeviceConfigurationPanel
      case 'telegram':
        return loadTelegramSettingsForm
      case 'pairing':
        return loadPairingSettingsPanel
      case 'connectivity':
        return loadConnectivitySettingsPanel
      case 'diagnostics':
        return loadDiagnosticsPanel
      case 'about':
        return loadAboutSettingsPanel
    }
  })()
  void loader().catch(() => undefined)
}

function backToSettings(): void {
  if (
    selectedSection.value === 'users' &&
    (typeof route.query.user === 'string' || route.query.newUser === '1')
  ) {
    void router.push({
      name: 'settings',
      params: { section: 'users' },
      query: { ...route.query, user: undefined, newUser: undefined }
    })
    return
  }
  if (
    selectedSection.value === 'telegram' &&
    (typeof route.query.bot === 'string' || route.query.newBot === '1')
  ) {
    void router.push({
      name: 'settings',
      params: { section: 'telegram' },
      query: { ...route.query, bot: undefined, newBot: undefined }
    })
    return
  }
  void router.push({ name: 'settings', params: { section: '' } })
}

function openDashboard(): void {
  void router.push({
    name: 'dashboard',
    query: { item: 'overview', from: 'settings' }
  })
}

function openTraffic(): void {
  void router.push({
    name: 'traffic',
    query: { from: 'settings' }
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
  void Promise.all([loadBootstrap(), loadDevices()])
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
          class="list-item settings-overview-link settings-traffic-link"
          type="button"
          @click="openTraffic"
        >
          <span class="settings-icon"><ChartNoAxesCombined :size="19" /></span>
          <span class="list-item__content">
            <strong>{{ t('shell.traffic') }}</strong>
            <small>{{ t('traffic.overview') }}</small>
          </span>
        </button>
        <section
          v-for="group in sectionGroups"
          :key="group.id"
          class="settings-nav-group"
          :aria-labelledby="`settings-group-${group.id}`"
        >
          <h2 :id="`settings-group-${group.id}`" class="settings-nav-group__title">
            {{ group.label }}
          </h2>
          <button
            v-for="section in group.sections"
            :key="section.id"
            class="list-item"
            :class="{ 'is-selected': selectedSection === section.id }"
            type="button"
            @focus="preloadSection(section.id)"
            @pointerenter="preloadSection(section.id)"
            @click="openSection(section.id)"
          >
            <span class="settings-icon"><component :is="section.icon" :size="19" /></span>
            <span class="list-item__content">
              <strong>{{ section.label }}</strong>
              <small>{{ section.description }}</small>
            </span>
          </button>
        </section>
        <footer class="settings-account">
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
            :disabled="fixtureMode || logoutPending"
            @click="logout"
          >
            <LoaderCircle v-if="logoutPending" class="spin" :size="19" />
            <LogOut v-else :size="19" />
          </button>
        </footer>
      </div>
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

        <SettingsContentTransition :content-key="selectedSection">
          <SettingsAsyncBoundary
            :loading-title="t('common.loading')"
            :loading-shape="settingsLoadingShape"
            :has-selection="settingsSkeletonHasSelection"
          >
            <div v-if="selectedSection === 'preferences'" class="settings-content">
              <PageContentFrame mode="reading">
                <AccountPreferencesPanel />
              </PageContentFrame>
            </div>

            <div v-else-if="selectedSection === 'security'" class="settings-content">
              <PageContentFrame mode="reading">
                <SecuritySettingsPanel />
              </PageContentFrame>
            </div>

            <div
              v-else-if="selectedSection === 'users'"
              class="settings-content settings-content--master-detail"
            >
              <UserSettingsPanel />
            </div>

            <div v-else-if="selectedSection === 'contacts'" class="settings-content">
              <PageContentFrame mode="reading">
                <ContactSyncSettings />
              </PageContentFrame>
            </div>

            <div v-else-if="selectedSection === 'audio'" class="settings-content">
              <PageContentFrame mode="reading">
                <AudioSettingsForm />
              </PageContentFrame>
            </div>

            <div
              v-else-if="selectedSection === 'devices'"
              class="settings-content settings-content--master-detail"
            >
              <DeviceConfigurationPanel />
            </div>

            <div
              v-else-if="selectedSection === 'telegram'"
              class="settings-content settings-content--master-detail"
            >
              <TelegramSettingsForm />
            </div>

            <div v-else-if="selectedSection === 'pairing'" class="settings-content">
              <PageContentFrame mode="reading">
                <PairingSettingsPanel />
              </PageContentFrame>
            </div>

            <div v-else-if="selectedSection === 'connectivity'" class="settings-content">
              <PageContentFrame mode="reading">
                <ConnectivitySettingsPanel />
              </PageContentFrame>
            </div>

            <div v-else-if="selectedSection === 'diagnostics'" class="settings-content">
              <PageContentFrame mode="fluid">
                <DiagnosticsPanel />
              </PageContentFrame>
            </div>

            <div v-else-if="selectedSection === 'about'" class="settings-content">
              <PageContentFrame mode="reading">
                <AboutSettingsPanel />
              </PageContentFrame>
            </div>
          </SettingsAsyncBoundary>
        </SettingsContentTransition>
      </template>

      <StatePanel v-else state="empty" :title="t('settings.selectSetting')" />
    </article>
  </section>
</template>

<style scoped>
.settings-nav-group {
  display: grid;
  padding-block: 10px 4px;
}

.settings-nav-group + .settings-nav-group {
  margin-top: 6px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}

.settings-nav-group__title {
  margin: 0;
  padding: 0 12px 6px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  letter-spacing: 0.08em;
  line-height: 1.2;
  text-transform: uppercase;
}

.settings-content {
  flex: 1 1 auto;
  container-type: inline-size;
}

.settings-content.settings-content--master-detail {
  display: flex;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}

/*
 * A compact desktop cannot carry the app rail, settings directory, resource
 * rail, and editor at once. Keep the desktop shell, but make the settings
 * directory and selected section separate steps until the workspace widens.
 */
@media (min-width: 861px) and (max-width: 1100px) {
  .settings-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .settings-workspace.has-selection > .settings-list-pane,
  .settings-workspace:not(.has-selection) > .settings-detail-pane {
    display: none;
  }

  .settings-detail-header .mobile-back {
    display: inline-grid;
  }
}
</style>
