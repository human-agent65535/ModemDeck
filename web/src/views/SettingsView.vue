<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, ArrowLeft, RadioTower, Send } from '@lucide/vue'
import StatePanel from '../components/StatePanel.vue'
import { fixtureMode } from '../api/client'
import type { Device } from '../api/types'
import {
  bootstrapResource,
  devicesResource,
  loadBootstrap,
  loadDevices
} from '../state/workspace'

type SettingsSection = 'devices' | 'telegram' | 'diagnostics'

const route = useRoute()
const router = useRouter()
const sections: Array<{
  id: SettingsSection
  label: string
  description: string
  icon: typeof RadioTower
}> = [
  { id: 'devices', label: '设备', description: '蜂窝线路与模组', icon: RadioTower },
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
    if (section === 'devices') void loadDevices()
  },
  { immediate: true }
)

function deviceName(device: Device): string {
  return device.alias || device.model || device.imei
}

function openSection(section: SettingsSection): void {
  void router.push({ name: 'settings', params: { section } })
}

function backToSettings(): void {
  void router.push({ name: 'settings', params: { section: '' } })
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
          <StatePanel v-if="devicesResource.status === 'loading'" state="loading" title="正在载入设备" />
          <StatePanel
            v-else-if="devicesResource.status === 'error'"
            state="error"
            title="无法载入设备"
            :detail="devicesResource.error"
            retryable
            @retry="loadDevices(true)"
          />
          <StatePanel
            v-else-if="devicesResource.status === 'ready' && devicesResource.data.length === 0"
            state="empty"
            title="没有设备"
            detail="服务未返回蜂窝设备"
          />
          <div v-else class="device-list">
            <section v-for="device in devicesResource.data" :key="device.imei" class="device-row">
              <header>
                <span class="settings-icon"><RadioTower :size="19" /></span>
                <div>
                  <h3>{{ deviceName(device) }}</h3>
                  <span>{{ device.model || device.imei }}</span>
                </div>
                <span class="status-label" :class="{ 'status-label--unknown': !device.sim_inserted }">
                  {{ device.sim_inserted ? 'SIM 已插入' : '未插入 SIM' }}
                </span>
              </header>
              <dl class="device-facts">
                <div><dt>IMEI</dt><dd>{{ device.imei }}</dd></div>
                <div v-if="device.firmware"><dt>固件</dt><dd>{{ device.firmware }}</dd></div>
                <div v-if="device.current_iccid"><dt>当前 ICCID</dt><dd>{{ device.current_iccid }}</dd></div>
                <div v-if="device.signal_dbm != null"><dt>信号</dt><dd>{{ device.signal_dbm }} dBm</dd></div>
                <div v-if="device.sim?.phone_number"><dt>号码</dt><dd>{{ device.sim.phone_number }}</dd></div>
                <div v-if="device.sim?.operator"><dt>运营商</dt><dd>{{ device.sim.operator }}</dd></div>
                <div v-if="device.sim?.imsi"><dt>IMSI</dt><dd>{{ device.sim.imsi }}</dd></div>
                <div v-if="device.sim?.reg_status_text"><dt>驻网</dt><dd>{{ device.sim.reg_status_text }}</dd></div>
                <div v-if="device.sim?.apn"><dt>APN</dt><dd>{{ device.sim.apn }}</dd></div>
                <div v-if="device.sim"><dt>IMS 状态码</dt><dd>{{ device.sim.ims_status }}</dd></div>
              </dl>
            </section>
          </div>
        </div>

        <div v-else-if="selectedSection === 'telegram'" class="settings-content">
          <StatePanel
            state="empty"
            title="Telegram 状态暂不可用"
            detail="当前只读 API 尚未提供 Telegram 配置接口"
          />
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
            v-if="bootstrapResource.status === 'error'"
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
