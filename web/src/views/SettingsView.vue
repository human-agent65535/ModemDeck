<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  Circle,
  LoaderCircle,
  LogOut,
  RadioTower,
  Send,
  UserRound
} from '@lucide/vue'
import StatePanel from '../components/StatePanel.vue'
import DeviceConfigurationPanel from '../components/DeviceConfigurationPanel.vue'
import RecordingSettingsForm from '../components/RecordingSettingsForm.vue'
import TelegramSettingsForm from '../components/TelegramSettingsForm.vue'
import { fixtureMode } from '../api/client'
import { logout as logoutSession, sessionState } from '../state/session'
import {
  bootstrapResource,
  devicesResource,
  loadBootstrap,
  loadDevices
} from '../state/workspace'

type SettingsSection = 'devices' | 'recording' | 'telegram' | 'diagnostics'

const route = useRoute()
const router = useRouter()
const logoutPending = ref(false)
const logoutError = ref('')
const sections: Array<{
  id: SettingsSection
  label: string
  description: string
  icon: typeof RadioTower
}> = [
  { id: 'devices', label: '设备', description: '蜂窝线路与模组', icon: RadioTower },
  { id: 'recording', label: '通话录音', description: '默认录音设置', icon: Circle },
  { id: 'telegram', label: 'Telegram', description: '消息通知', icon: Send },
  { id: 'diagnostics', label: '诊断', description: '服务与接口状态', icon: Activity }
]
const selectedSection = computed<SettingsSection | ''>(() => {
  const value = String(route.params.section || '')
  return sections.some(section => section.id === value) ? (value as SettingsSection) : ''
})
const currentTitle = computed(
  () => sections.find(section => section.id === selectedSection.value)?.label || '设置'
)

watch(
  selectedSection,
  section => {
    if (section === 'devices') void loadBootstrap()
    if (section === 'diagnostics') void Promise.all([loadBootstrap(), loadDevices()])
  },
  { immediate: true }
)

function openSection(section: SettingsSection): void {
  void router.push({ name: 'settings', params: { section } })
}

function backToSettings(): void {
  void router.push({ name: 'settings', params: { section: '' } })
}

async function logout(): Promise<void> {
  if (logoutPending.value) return
  logoutPending.value = true
  logoutError.value = ''
  try {
    await logoutSession()
    await router.replace({ name: 'login' })
  } catch (error) {
    logoutError.value = error instanceof Error ? error.message : '退出登录失败'
  } finally {
    logoutPending.value = false
  }
}

onMounted(() => {
  void loadBootstrap()
})
</script>

<template>
  <section class="workspace settings-workspace" :class="{ 'has-selection': selectedSection }">
    <aside class="list-pane settings-list-pane">
      <header class="pane-header"><h1>设置</h1></header>
      <div class="item-list settings-list">
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
          <strong>{{ sessionState.username || '管理员' }}</strong>
          <small>管理员账户</small>
          <small v-if="logoutError" class="settings-account__error" role="alert">
            {{ logoutError }}
          </small>
        </span>
        <button
          class="icon-button"
          type="button"
          title="退出登录"
          aria-label="退出登录"
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
          <button class="icon-button mobile-back" type="button" title="返回设置" @click="backToSettings">
            <ArrowLeft :size="20" />
          </button>
          <h2>{{ currentTitle }}</h2>
        </header>

        <div v-if="selectedSection === 'devices'" class="settings-content">
          <DeviceConfigurationPanel />
        </div>

        <div v-else-if="selectedSection === 'recording'" class="settings-content">
          <RecordingSettingsForm />
        </div>

        <div v-else-if="selectedSection === 'telegram'" class="settings-content">
          <TelegramSettingsForm />
        </div>

        <div v-else class="settings-content">
          <section class="diagnostics-summary">
            <h3>接口状态</h3>
            <dl class="settings-facts">
              <div><dt>数据源</dt><dd>{{ fixtureMode ? '开发 fixture' : '生产 API' }}</dd></div>
              <div><dt>Bootstrap</dt><dd>{{ bootstrapResource.status }}</dd></div>
              <div><dt>设备接口</dt><dd>{{ devicesResource.status }}</dd></div>
              <template v-if="bootstrapResource.data">
                <div><dt>Host agent</dt><dd>{{ bootstrapResource.data.capabilities.agent_connected ? 'connected' : 'disconnected' }}</dd></div>
                <div><dt>消息发送</dt><dd>{{ bootstrapResource.data.capabilities.message ? 'available' : 'unavailable' }}</dd></div>
                <div><dt>通话控制</dt><dd>{{ bootstrapResource.data.capabilities.dial ? 'available' : 'unavailable' }}</dd></div>
                <div><dt>WebRTC 音频</dt><dd>{{ bootstrapResource.data.capabilities.webrtc_audio ? 'available' : 'unavailable' }}</dd></div>
                <div><dt>设备控制</dt><dd>{{ bootstrapResource.data.capabilities.device_control ? 'available' : 'unavailable' }}</dd></div>
                <div><dt>VoLTE 控制</dt><dd>{{ bootstrapResource.data.capabilities.volte_control ? 'available' : 'unavailable' }}</dd></div>
                <div><dt>VoWiFi 控制</dt><dd>{{ bootstrapResource.data.capabilities.vowifi_control ? 'available' : 'unavailable' }}</dd></div>
              </template>
            </dl>
          </section>
          <StatePanel
            v-if="bootstrapResource.status === 'forbidden'"
            state="forbidden"
            title="无权查看诊断信息"
            :detail="bootstrapResource.error"
          />
          <StatePanel
            v-else-if="bootstrapResource.status === 'error'"
            state="error"
            title="Bootstrap 请求失败"
            :detail="bootstrapResource.error"
            retryable
            @retry="loadBootstrap(true)"
          />
        </div>
      </template>

      <StatePanel v-else state="empty" title="选择一项设置" />
    </article>
  </section>
</template>
