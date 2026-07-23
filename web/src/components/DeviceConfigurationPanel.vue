<script setup lang="ts">
import {
  AlertCircle,
  CardSim,
  CheckCircle2,
  Database,
  LoaderCircle,
  Network,
  Phone,
  PhoneIncoming,
  Plus,
  Power,
  RadioTower,
  Save,
  Send,
  ShieldAlert,
  Trash2,
  X
} from '@lucide/vue'
import { computed, onMounted, ref, watch } from 'vue'
import { gateway } from '../api/client'
import type {
  ConnectionProfile,
  DeviceFeatureCapability,
  IncomingCallPolicy,
  IPFamily,
  LineSummary,
  SIMOperation,
  SIMStatus,
  USSDStatus
} from '../api/types'
import {
  connectData,
  deviceConfigurationResource,
  deviceConfigurationState,
  disconnectData,
  loadDeviceConfiguration,
  selectDeviceConfiguration,
  setIncomingCallPolicy,
  setRadioEnabled,
  setVoLTEPolicy
} from '../state/deviceConfiguration'
import {
  bootstrapResource,
  createDevice,
  devicesResource,
  lineKey,
  lineLabel,
  loadBootstrap,
  loadDevices,
  renameDevice,
  updateDefaultLine
} from '../state/workspace'
import ModuleCard from './ModuleCard.vue'
import StatePanel from './StatePanel.vue'

type DeviceTab = 'overview' | 'network' | 'sim' | 'voice' | 'ussd'
type AsyncStatus = 'idle' | 'loading' | 'ready' | 'error'

const tabs: Array<{ id: DeviceTab; label: string; icon: typeof RadioTower }> = [
  { id: 'overview', label: '概览', icon: RadioTower },
  { id: 'network', label: '网络', icon: Network },
  { id: 'sim', label: 'SIM', icon: CardSim },
  { id: 'voice', label: '通话', icon: Phone },
  { id: 'ussd', label: 'USSD', icon: Send }
]

const activeTab = ref<DeviceTab>('overview')
const apn = ref('')
const ipFamily = ref<IPFamily>('auto')
const incomingPolicyDraft = ref<IncomingCallPolicy>('follow_global')
const voltePolicyDraft = ref<'enabled' | 'disabled'>('disabled')

const addOpen = ref(false)
const addIMEI = ref('')
const addAlias = ref('')
const addPending = ref(false)
const addError = ref('')
const renameIMEI = ref('')
const renameAlias = ref('')
const renamePending = ref(false)
const moduleError = ref('')

const simStatus = ref<SIMStatus | null>(null)
const simLoadStatus = ref<AsyncStatus>('idle')
const simError = ref('')
const simOperation = ref<SIMOperation>('send_pin')
const simPIN = ref('')
const simPUK = ref('')
const simNewPIN = ref('')
const simProtectionEnabled = ref(true)
const simPending = ref(false)

const profiles = ref<ConnectionProfile[]>([])
const profileLoadStatus = ref<AsyncStatus>('idle')
const profileError = ref('')
const editingProfileID = ref<number | null>(null)
const profileName = ref('')
const profileAPN = ref('')
const profileIPFamily = ref('ipv4v6')
const profileUser = ref('')
const profilePassword = ref('')
const profilePending = ref(false)

const ussdStatus = ref<USSDStatus | null>(null)
const ussdLoadStatus = ref<AsyncStatus>('idle')
const ussdError = ref('')
const ussdCommand = ref('')
const ussdResult = ref('')
const ussdPending = ref(false)

const lines = computed(() => bootstrapResource.data?.lines || [])
const defaultDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
)
const selectedLineID = computed(() => deviceConfigurationState.selectedLineID)
const selectedLine = computed(() =>
  lines.value.find(line => line.id === selectedLineID.value)
)
const selectedDevice = computed(() =>
  devicesResource.data.find(device => device.imei === selectedLine.value?.device_imei)
)
const selectedResource = computed(() =>
  selectedLineID.value ? deviceConfigurationResource(selectedLineID.value) : null
)
const configuration = computed(() => selectedResource.value?.data || null)
const hardware = computed(() => configuration.value?.hardware)
const incomingCalls = computed(() => configuration.value?.incoming_calls)
const savingOperation = computed(() => selectedResource.value?.savingOperation || '')
const hardwareBusy = computed(() => savingOperation.value !== '')
const selectedIsDefault = computed(
  () => selectedLine.value?.device_imei === defaultDeviceIMEI.value
)
const voiceAvailable = computed(() => selectedLine.value?.capabilities?.voice === true)
const mediaAvailable = computed(() => selectedLine.value?.capabilities?.media === true)

const otherCapabilities = computed(() => {
  const capabilities = hardware.value?.capabilities
  if (!capabilities) return []
  return [
    { id: 'voice', label: 'Voice', capability: capabilities.voice },
    { id: 'flight_mode', label: '飞行模式', capability: capabilities.flight_mode },
    { id: 'vowifi', label: 'VoWiFi', capability: capabilities.vowifi },
    { id: 'volte', label: 'VoLTE', capability: capabilities.volte },
    { id: 'esim', label: 'eSIM', capability: capabilities.esim },
    { id: 'ussd', label: 'USSD', capability: capabilities.ussd },
    {
      id: 'connection_profile',
      label: '连接配置',
      capability: capabilities.connection_profile
    }
  ]
})

watch(
  [lines, defaultDeviceIMEI],
  ([currentLines, defaultIMEI]) => {
    if (
      selectedLineID.value &&
      currentLines.some(line => line.id === selectedLineID.value)
    ) {
      return
    }
    const next =
      currentLines.find(line => line.device_imei === defaultIMEI && line.id) ||
      currentLines.find(line => line.id)
    if (next?.id) selectLine(next)
  },
  { immediate: true }
)

watch(
  () => incomingCalls.value?.revision,
  () => {
    incomingPolicyDraft.value = incomingCalls.value?.policy || 'follow_global'
  }
)

watch(
  () => hardware.value?.revision,
  () => {
    apn.value = ''
    ipFamily.value = 'auto'
    voltePolicyDraft.value =
      hardware.value?.volte.policy_known && hardware.value.volte.policy
        ? hardware.value.volte.policy
        : 'disabled'
  }
)

watch(activeTab, tab => {
  if (tab === 'sim') void loadSIM()
  if (tab === 'network') void loadProfiles()
  if (tab === 'ussd') void loadUSSD()
})

function selectLine(line: LineSummary): void {
  if (!line.id) return
  selectDeviceConfiguration(line.id)
  void loadDeviceConfiguration(line.id)
  resetLineServices()
}

function resetLineServices(): void {
  simStatus.value = null
  simLoadStatus.value = 'idle'
  simError.value = ''
  profiles.value = []
  profileLoadStatus.value = 'idle'
  profileError.value = ''
  ussdStatus.value = null
  ussdLoadStatus.value = 'idle'
  ussdError.value = ''
  ussdResult.value = ''
}

function deviceFor(line: LineSummary) {
  return devicesResource.data.find(device => device.imei === line.device_imei)
}

function readOnlyReason(capability: DeviceFeatureCapability): string {
  if (!capability.supported) return capability.reason || '不支持'
  if (!capability.implemented) return capability.reason || '未实现'
  if (!capability.readable) return capability.reason || '不可读取'
  return ''
}

function capabilityStatus(capability: DeviceFeatureCapability): string {
  if (capability.writable) return '可读写'
  if (capability.readable) return '只读'
  if (capability.supported && capability.implemented) return '暂不可用'
  if (capability.supported) return '未实现'
  return '不支持'
}

function policyLabel(policy: IncomingCallPolicy): string {
  if (policy === 'follow_global') return '跟随全局'
  if (policy === 'receive') return '接听来电'
  return '免打扰'
}

async function makeDefault(line: LineSummary): Promise<void> {
  if (!line.device_imei || line.device_imei === defaultDeviceIMEI.value) return
  moduleError.value = ''
  try {
    await updateDefaultLine(line.device_imei)
  } catch (error) {
    moduleError.value = error instanceof Error ? error.message : '默认线路保存失败'
  }
}

function beginRename(line: LineSummary): void {
  moduleError.value = ''
  renameIMEI.value = line.device_imei
  renameAlias.value = lineLabel(line)
}

async function saveRename(): Promise<void> {
  if (!renameIMEI.value || renamePending.value || !renameAlias.value.trim()) return
  renamePending.value = true
  moduleError.value = ''
  try {
    await renameDevice(renameIMEI.value, { alias: renameAlias.value.trim() })
    renameIMEI.value = ''
    await loadBootstrap(true)
  } catch (error) {
    moduleError.value = error instanceof Error ? error.message : '模组名称保存失败'
  } finally {
    renamePending.value = false
  }
}

async function addModule(): Promise<void> {
  if (addPending.value || !addIMEI.value.trim()) return
  addPending.value = true
  addError.value = ''
  try {
    await createDevice({ imei: addIMEI.value.trim(), alias: addAlias.value.trim() })
    addIMEI.value = ''
    addAlias.value = ''
    addOpen.value = false
    await Promise.all([loadDevices(true), loadBootstrap(true)])
  } catch (error) {
    addError.value = error instanceof Error ? error.message : '新增模组失败'
  } finally {
    addPending.value = false
  }
}

async function changeRadio(event: Event): Promise<void> {
  if (!selectedLineID.value) return
  const control = event.target as HTMLInputElement
  const enabled = control.checked
  if (!enabled && !window.confirm('关闭蜂窝射频会中断驻网和数据连接。继续？')) {
    control.checked = Boolean(hardware.value?.radio.enabled)
    return
  }
  await setRadioEnabled(selectedLineID.value, enabled)
}

async function applyDataConnection(): Promise<void> {
  if (selectedLineID.value) {
    await connectData(selectedLineID.value, apn.value, ipFamily.value)
  }
}

async function stopDataConnection(): Promise<void> {
  if (!selectedLineID.value || !window.confirm('断开此模组的数据连接？')) return
  await disconnectData(selectedLineID.value)
}

async function applyVoLTE(): Promise<void> {
  if (!selectedLineID.value) return
  if (!window.confirm(`将 VoLTE 设为${voltePolicyDraft.value === 'enabled' ? '开启' : '关闭'}？`)) {
    return
  }
  await setVoLTEPolicy(selectedLineID.value, voltePolicyDraft.value)
}

async function applyIncomingPolicy(): Promise<void> {
  if (selectedLineID.value) {
    await setIncomingCallPolicy(selectedLineID.value, incomingPolicyDraft.value)
  }
}

async function loadSIM(force = false): Promise<void> {
  if (!selectedLineID.value || (simLoadStatus.value === 'ready' && !force)) return
  simLoadStatus.value = 'loading'
  simError.value = ''
  try {
    simStatus.value = await gateway.getSIMStatus(selectedLineID.value)
    simLoadStatus.value = 'ready'
    if (simStatus.value.unlock_required.includes('puk')) simOperation.value = 'send_puk'
    else if (simStatus.value.unlock_required.includes('pin')) simOperation.value = 'send_pin'
  } catch (error) {
    simLoadStatus.value = 'error'
    simError.value = error instanceof Error ? error.message : 'SIM 状态读取失败'
  }
}

async function applySIMCommand(): Promise<void> {
  if (!selectedLineID.value || simPending.value) return
  const label: Record<SIMOperation, string> = {
    send_pin: '提交 PIN',
    send_puk: '提交 PUK 并设置新 PIN',
    enable_pin: simProtectionEnabled.value ? '开启 PIN 保护' : '关闭 PIN 保护',
    change_pin: '修改 PIN'
  }
  if (!window.confirm(`${label[simOperation.value]}？`)) return
  simPending.value = true
  simError.value = ''
  try {
    await gateway.commandSIM(selectedLineID.value, {
      operation: simOperation.value,
      ...(simPIN.value ? { pin: simPIN.value } : {}),
      ...(simPUK.value ? { puk: simPUK.value } : {}),
      ...(simNewPIN.value ? { new_pin: simNewPIN.value } : {}),
      ...(simOperation.value === 'enable_pin'
        ? { enabled: simProtectionEnabled.value }
        : {})
    })
    simPIN.value = ''
    simPUK.value = ''
    simNewPIN.value = ''
    await loadSIM(true)
  } catch (error) {
    simError.value = error instanceof Error ? error.message : 'SIM 操作失败'
  } finally {
    simPending.value = false
  }
}

async function loadProfiles(force = false): Promise<void> {
  if (!selectedLineID.value || (profileLoadStatus.value === 'ready' && !force)) return
  profileLoadStatus.value = 'loading'
  profileError.value = ''
  try {
    profiles.value = await gateway.listConnectionProfiles(selectedLineID.value)
    profileLoadStatus.value = 'ready'
  } catch (error) {
    profileLoadStatus.value = 'error'
    profileError.value = error instanceof Error ? error.message : '连接配置读取失败'
  }
}

function editProfile(profile?: ConnectionProfile): void {
  editingProfileID.value = profile?.profile_id ?? null
  profileName.value = profile?.profile_name || ''
  profileAPN.value = profile?.apn || ''
  profileIPFamily.value = profile?.ip_family || 'ipv4v6'
  profileUser.value = profile?.user || ''
  profilePassword.value = ''
}

async function saveProfile(): Promise<void> {
  if (!selectedLineID.value || profilePending.value) return
  if (!profileName.value.trim() && !profileAPN.value.trim()) {
    profileError.value = '名称或 APN 至少填写一项'
    return
  }
  if (!window.confirm('保存此连接配置？')) return
  profilePending.value = true
  profileError.value = ''
  try {
    await gateway.saveConnectionProfile(selectedLineID.value, {
      ...(editingProfileID.value === null ? {} : { profile_id: editingProfileID.value }),
      profile_name: profileName.value.trim(),
      apn: profileAPN.value.trim(),
      ip_family: profileIPFamily.value,
      user: profileUser.value.trim(),
      ...(profilePassword.value ? { password: profilePassword.value } : {})
    })
    editProfile()
    await loadProfiles(true)
  } catch (error) {
    profileError.value = error instanceof Error ? error.message : '连接配置保存失败'
  } finally {
    profilePending.value = false
  }
}

async function deleteProfile(profile: ConnectionProfile): Promise<void> {
  if (!selectedLineID.value || profilePending.value) return
  if (!window.confirm(`删除连接配置“${profile.profile_name || profile.profile_id}”？`)) return
  profilePending.value = true
  profileError.value = ''
  try {
    await gateway.deleteConnectionProfile(selectedLineID.value, {
      profile_id: profile.profile_id
    })
    if (editingProfileID.value === profile.profile_id) editProfile()
    await loadProfiles(true)
  } catch (error) {
    profileError.value = error instanceof Error ? error.message : '连接配置删除失败'
  } finally {
    profilePending.value = false
  }
}

async function loadUSSD(force = false): Promise<void> {
  if (!selectedLineID.value || (ussdLoadStatus.value === 'ready' && !force)) return
  ussdLoadStatus.value = 'loading'
  ussdError.value = ''
  try {
    ussdStatus.value = await gateway.getUSSDStatus(selectedLineID.value)
    ussdLoadStatus.value = 'ready'
  } catch (error) {
    ussdLoadStatus.value = 'error'
    ussdError.value = error instanceof Error ? error.message : 'USSD 状态读取失败'
  }
}

async function submitUSSD(action: 'initiate' | 'respond' | 'cancel'): Promise<void> {
  if (!selectedLineID.value || ussdPending.value) return
  ussdPending.value = true
  ussdError.value = ''
  try {
    const result = await gateway.commandUSSD(selectedLineID.value, {
      action,
      ...(action === 'cancel' ? {} : { command: ussdCommand.value.trim() })
    })
    ussdResult.value = result.response || ''
    if (action !== 'respond') ussdCommand.value = ''
    await loadUSSD(true)
  } catch (error) {
    ussdError.value = error instanceof Error ? error.message : 'USSD 请求失败'
  } finally {
    ussdPending.value = false
  }
}

onMounted(() => {
  void Promise.all([loadBootstrap(), loadDevices()])
})
</script>

<template>
  <section class="device-configuration" aria-labelledby="device-configuration-title">
    <header class="module-toolbar">
      <div>
        <h3 id="device-configuration-title">模组</h3>
        <span>{{ lines.length }}</span>
      </div>
      <button class="secondary-action" type="button" @click="addOpen = !addOpen">
        <Plus :size="16" />
        新增
      </button>
    </header>

    <form v-if="addOpen" class="module-edit-row" @submit.prevent="addModule">
      <label>
        <span>IMEI</span>
        <input v-model.trim="addIMEI" inputmode="numeric" autocomplete="off" required />
      </label>
      <label>
        <span>名称</span>
        <input v-model.trim="addAlias" autocomplete="off" />
      </label>
      <button class="primary-action" type="submit" :disabled="addPending || !addIMEI">
        <LoaderCircle v-if="addPending" class="spin" :size="16" />
        <Save v-else :size="16" />
        保存
      </button>
      <button class="icon-button" type="button" title="取消" @click="addOpen = false">
        <X :size="18" />
      </button>
      <p v-if="addError" class="field-error">{{ addError }}</p>
    </form>

    <StatePanel
      v-if="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
      state="loading"
      title="正在载入模组"
    />
    <StatePanel
      v-else-if="bootstrapResource.status === 'error'"
      state="error"
      title="无法载入模组"
      :detail="bootstrapResource.error"
      retryable
      @retry="loadBootstrap(true)"
    />
    <StatePanel
      v-else-if="lines.length === 0"
      state="empty"
      title="没有检测到模组"
    />
    <div v-else class="module-grid">
      <ModuleCard
        v-for="line in lines"
        :key="lineKey(line)"
        :line="line"
        :device="deviceFor(line)"
        :selected="line.id === selectedLineID"
        :default-line="line.device_imei === defaultDeviceIMEI"
        actions
        @select="selectLine(line)"
        @make-default="makeDefault(line)"
        @rename="beginRename(line)"
      />
    </div>
    <p v-if="moduleError" class="field-error" role="alert">{{ moduleError }}</p>

    <form v-if="renameIMEI" class="module-edit-row" @submit.prevent="saveRename">
      <label class="module-edit-row__wide">
        <span>模组名称</span>
        <input v-model.trim="renameAlias" autocomplete="off" required />
      </label>
      <button class="primary-action" type="submit" :disabled="renamePending || !renameAlias">
        <LoaderCircle v-if="renamePending" class="spin" :size="16" />
        <Save v-else :size="16" />
        保存
      </button>
      <button class="icon-button" type="button" title="取消" @click="renameIMEI = ''">
        <X :size="18" />
      </button>
    </form>

    <template v-if="selectedLineID">
      <nav class="device-tabs" aria-label="模组设置">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          :class="{ 'is-selected': activeTab === tab.id }"
          @click="activeTab = tab.id"
        >
          <component :is="tab.icon" :size="16" />
          {{ tab.label }}
        </button>
      </nav>

      <StatePanel
        v-if="selectedResource?.status === 'loading' || selectedResource?.status === 'idle'"
        state="loading"
        title="正在读取模组"
      />
      <StatePanel
        v-else-if="selectedResource?.status === 'error' && !configuration"
        state="error"
        title="无法读取模组"
        :detail="selectedResource.error"
        retryable
        @retry="loadDeviceConfiguration(selectedLineID, true)"
      />

      <div v-else-if="hardware && incomingCalls" class="device-configuration__body">
        <p v-if="selectedResource?.error" class="inline-error" role="alert">
          <AlertCircle :size="16" />
          {{ selectedResource.error }}
        </p>

        <template v-if="activeTab === 'overview'">
          <section class="configuration-section configuration-summary">
            <header>
              <div>
                <h4>{{ selectedLine ? lineLabel(selectedLine) : hardware.line_id }}</h4>
                <span v-if="selectedIsDefault">默认线路</span>
              </div>
              <strong>{{ selectedLine?.state || 'unknown' }}</strong>
            </header>
            <dl>
              <div><dt>制造商</dt><dd>{{ hardware.identity.manufacturer || '—' }}</dd></div>
              <div><dt>型号</dt><dd>{{ hardware.identity.model || '—' }}</dd></div>
              <div><dt>IMEI</dt><dd>{{ hardware.identity.equipment_identifier || '—' }}</dd></div>
              <div><dt>固件</dt><dd>{{ hardware.identity.firmware || '—' }}</dd></div>
              <div><dt>ICCID</dt><dd>{{ selectedLine?.iccid || '—' }}</dd></div>
              <div><dt>端口</dt><dd>{{ selectedDevice?.port || '—' }}</dd></div>
            </dl>
          </section>

          <section class="configuration-section">
            <header><h4>能力</h4></header>
            <div class="capability-grid">
              <div v-for="item in otherCapabilities" :key="item.id">
                <span>
                  <CheckCircle2 v-if="item.capability.readable" :size="15" />
                  <ShieldAlert v-else :size="15" />
                  {{ item.label }}
                </span>
                <strong>{{ capabilityStatus(item.capability) }}</strong>
                <small>{{ readOnlyReason(item.capability) }}</small>
              </div>
            </div>
          </section>
        </template>

        <template v-else-if="activeTab === 'network'">
          <section class="configuration-section">
            <header><Power :size="18" /><h4>蜂窝射频</h4></header>
            <label
              v-if="hardware.capabilities.radio.writable && hardware.radio.enabled_known"
              class="configuration-toggle"
            >
              <span>
                <strong>{{ hardware.radio.enabled ? '已开启' : '已关闭' }}</strong>
                <small>{{ hardware.radio.power_state || '状态未知' }}</small>
              </span>
              <span class="configuration-toggle__control">
                <LoaderCircle
                  v-if="savingOperation === 'set_radio_enabled'"
                  class="spin"
                  :size="16"
                />
                <input
                  type="checkbox"
                  role="switch"
                  :checked="hardware.radio.enabled"
                  :disabled="hardwareBusy"
                  @change="changeRadio"
                />
              </span>
            </label>
            <div v-else class="configuration-readonly">
              <strong>{{ hardware.radio.enabled ? '已开启' : '已关闭' }}</strong>
              <small>{{ readOnlyReason(hardware.capabilities.radio) }}</small>
            </div>
          </section>

          <section class="configuration-section">
            <header><Network :size="18" /><h4>数据连接</h4></header>
            <div v-if="hardware.capabilities.data_connection.writable" class="data-form">
              <label><span>APN</span><input v-model.trim="apn" placeholder="自动" /></label>
              <label>
                <span>IP</span>
                <select v-model="ipFamily">
                  <option value="auto">自动</option>
                  <option value="ipv4">IPv4</option>
                  <option value="ipv6">IPv6</option>
                  <option value="ipv4v6">IPv4 + IPv6</option>
                </select>
              </label>
              <button class="primary-action" type="button" :disabled="hardwareBusy" @click="applyDataConnection">
                连接
              </button>
              <button
                v-if="hardware.network_enabled"
                class="secondary-action"
                type="button"
                :disabled="hardwareBusy"
                @click="stopDataConnection"
              >
                断开
              </button>
            </div>
            <div v-else class="configuration-readonly">
              <strong>{{ hardware.network_enabled ? '已连接' : '未连接' }}</strong>
              <small>{{ readOnlyReason(hardware.capabilities.data_connection) }}</small>
            </div>
            <div v-if="hardware.data_connections.length" class="bearer-list">
              <article v-for="connection in hardware.data_connections" :key="connection.id">
                <strong>{{ connection.apn || '自动 APN' }}</strong>
                <span>{{ connection.ip_family }} · {{ connection.interface || '—' }}</span>
                <small>{{ connection.ipv4.address || connection.ipv6.address || '地址未分配' }}</small>
              </article>
            </div>
          </section>

          <section class="configuration-section">
            <header>
              <Database :size="18" />
              <h4>连接配置</h4>
              <button class="secondary-action section-action" type="button" @click="editProfile()">
                <Plus :size="15" />
                新增
              </button>
            </header>
            <StatePanel v-if="profileLoadStatus === 'loading'" state="loading" title="正在读取配置" />
            <p v-else-if="profileError" class="inline-error">{{ profileError }}</p>
            <div v-else class="profile-list">
              <div
                v-for="profile in profiles"
                :key="profile.profile_id"
                :class="{ 'is-selected': editingProfileID === profile.profile_id }"
              >
                <button type="button" @click="editProfile(profile)">
                  <span><strong>{{ profile.profile_name || `Profile ${profile.profile_id}` }}</strong><small>{{ profile.apn || '无 APN' }}</small></span>
                  <span>{{ profile.ip_family || profile.ip_type }}</span>
                </button>
                <button
                  class="icon-button"
                  type="button"
                  title="删除连接配置"
                  @click.stop="deleteProfile(profile)"
                >
                  <Trash2 :size="16" />
                </button>
              </div>
            </div>
            <form class="profile-form" @submit.prevent="saveProfile">
              <label><span>名称</span><input v-model.trim="profileName" /></label>
              <label><span>APN</span><input v-model.trim="profileAPN" /></label>
              <label>
                <span>IP</span>
                <select v-model="profileIPFamily">
                  <option value="ipv4">IPv4</option>
                  <option value="ipv6">IPv6</option>
                  <option value="ipv4v6">IPv4 + IPv6</option>
                </select>
              </label>
              <label><span>用户名</span><input v-model.trim="profileUser" autocomplete="username" /></label>
              <label><span>密码</span><input v-model="profilePassword" type="password" autocomplete="new-password" /></label>
              <button class="primary-action" type="submit" :disabled="profilePending">
                <LoaderCircle v-if="profilePending" class="spin" :size="16" />
                <Save v-else :size="16" />
                保存
              </button>
            </form>
          </section>
        </template>

        <template v-else-if="activeTab === 'sim'">
          <section class="configuration-section">
            <header><CardSim :size="18" /><h4>SIM</h4></header>
            <StatePanel v-if="simLoadStatus === 'loading'" state="loading" title="正在读取 SIM" />
            <p v-else-if="simError" class="inline-error">{{ simError }}</p>
            <template v-else-if="simStatus">
              <dl class="configuration-facts">
                <div><dt>ICCID</dt><dd>{{ simStatus.identifier || '—' }}</dd></div>
                <div><dt>IMSI</dt><dd>{{ simStatus.imsi || '—' }}</dd></div>
                <div><dt>运营商</dt><dd>{{ simStatus.operator_name || simStatus.operator_identifier || '—' }}</dd></div>
                <div><dt>锁定</dt><dd>{{ simStatus.unlock_required || 'none' }}</dd></div>
              </dl>
              <div class="retry-row">
                <span v-for="(count, name) in simStatus.unlock_retries" :key="name">
                  {{ name }} {{ count }}
                </span>
              </div>
            </template>
          </section>

          <section class="configuration-section">
            <header><ShieldAlert :size="18" /><h4>PIN</h4></header>
            <form class="sim-form" @submit.prevent="applySIMCommand">
              <label>
                <span>操作</span>
                <select v-model="simOperation">
                  <option value="send_pin">解锁 PIN</option>
                  <option value="send_puk">解锁 PUK</option>
                  <option value="enable_pin">PIN 保护</option>
                  <option value="change_pin">修改 PIN</option>
                </select>
              </label>
              <label v-if="simOperation !== 'send_puk'">
                <span>{{ simOperation === 'change_pin' ? '当前 PIN' : 'PIN' }}</span>
                <input v-model="simPIN" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'send_puk'">
                <span>PUK</span>
                <input v-model="simPUK" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'send_puk' || simOperation === 'change_pin'">
                <span>新 PIN</span>
                <input v-model="simNewPIN" type="password" inputmode="numeric" maxlength="8" autocomplete="off" />
              </label>
              <label v-if="simOperation === 'enable_pin'" class="inline-check">
                <input v-model="simProtectionEnabled" type="checkbox" />
                <span>开启保护</span>
              </label>
              <button class="primary-action" type="submit" :disabled="simPending">
                <LoaderCircle v-if="simPending" class="spin" :size="16" />
                应用
              </button>
            </form>
          </section>
        </template>

        <template v-else-if="activeTab === 'voice'">
          <section class="configuration-section">
            <header><Phone :size="18" /><h4>Voice</h4></header>
            <div class="voice-capabilities">
              <div class="voice-status" :class="{ 'is-available': voiceAvailable }">
                <CheckCircle2 v-if="voiceAvailable" :size="18" />
                <AlertCircle v-else :size="18" />
                <span><strong>通话控制</strong><small>{{ voiceAvailable ? '可用' : '不可用' }}</small></span>
              </div>
              <div class="voice-status" :class="{ 'is-available': mediaAvailable }">
                <CheckCircle2 v-if="mediaAvailable" :size="18" />
                <AlertCircle v-else :size="18" />
                <span><strong>通话音频</strong><small>{{ mediaAvailable ? '可用' : '不可用' }}</small></span>
              </div>
            </div>
          </section>

          <section class="configuration-section">
            <header><PhoneIncoming :size="18" /><h4>来电</h4></header>
            <div class="configuration-control-row">
              <label>
                <span>策略</span>
                <select v-model="incomingPolicyDraft" :disabled="Boolean(savingOperation)">
                  <option value="follow_global">跟随全局</option>
                  <option value="receive">接听来电</option>
                  <option value="do_not_disturb">免打扰</option>
                </select>
              </label>
              <button
                class="primary-action"
                type="button"
                :disabled="Boolean(savingOperation) || incomingPolicyDraft === incomingCalls.policy"
                @click="applyIncomingPolicy"
              >
                保存
              </button>
            </div>
            <dl class="configuration-facts">
              <div><dt>线路</dt><dd>{{ policyLabel(incomingCalls.policy) }}</dd></div>
              <div><dt>当前</dt><dd>{{ policyLabel(incomingCalls.effective_policy) }}</dd></div>
              <div><dt>全局</dt><dd>{{ incomingCalls.global_receive_calls ? '接听来电' : '免打扰' }}</dd></div>
            </dl>
            <p v-if="incomingCalls.enforcement.config_only" class="inline-warning">
              当前模组未报告拒接能力
            </p>
          </section>

          <section class="configuration-section">
            <header><RadioTower :size="18" /><h4>VoLTE</h4></header>
            <div
              v-if="hardware.capabilities.volte.writable"
              class="configuration-control-row"
            >
              <label>
                <span>状态</span>
                <select v-model="voltePolicyDraft" :disabled="hardwareBusy">
                  <option value="enabled">开启</option>
                  <option value="disabled">关闭</option>
                </select>
              </label>
              <button
                class="primary-action"
                type="button"
                :disabled="hardwareBusy || voltePolicyDraft === hardware.volte.policy"
                @click="applyVoLTE"
              >
                保存
              </button>
            </div>
            <div v-else class="configuration-readonly">
              <strong>
                {{
                  hardware.volte.policy_known
                    ? hardware.volte.policy === 'enabled'
                      ? '已开启'
                      : '已关闭'
                    : '状态未知'
                }}
              </strong>
              <small>{{ readOnlyReason(hardware.capabilities.volte) }}</small>
            </div>
          </section>
        </template>

        <template v-else>
          <section class="configuration-section">
            <header><Send :size="18" /><h4>USSD</h4></header>
            <StatePanel v-if="ussdLoadStatus === 'loading'" state="loading" title="正在读取 USSD" />
            <p v-else-if="ussdError" class="inline-error">{{ ussdError }}</p>
            <div v-else-if="ussdStatus" class="ussd-status">
              <strong>{{ ussdStatus.state }}</strong>
              <span v-if="ussdStatus.network_notification">{{ ussdStatus.network_notification }}</span>
              <span v-if="ussdStatus.network_request">{{ ussdStatus.network_request }}</span>
            </div>
            <form class="ussd-form" @submit.prevent="submitUSSD(ussdStatus?.state === 'user-response' ? 'respond' : 'initiate')">
              <input v-model.trim="ussdCommand" placeholder="*123#" autocomplete="off" />
              <button class="primary-action" type="submit" :disabled="ussdPending || !ussdCommand">
                <LoaderCircle v-if="ussdPending" class="spin" :size="16" />
                <Send v-else :size="16" />
                发送
              </button>
              <button
                v-if="ussdStatus && ussdStatus.state !== 'idle'"
                class="secondary-action"
                type="button"
                :disabled="ussdPending"
                @click="submitUSSD('cancel')"
              >
                取消会话
              </button>
            </form>
            <pre v-if="ussdResult">{{ ussdResult }}</pre>
          </section>
        </template>
      </div>
    </template>
  </section>
</template>

<style scoped>
.device-configuration {
  width: 100%;
  min-width: 0;
}

.module-toolbar,
.configuration-section > header {
  display: flex;
  align-items: center;
  gap: 9px;
}

.module-toolbar {
  min-height: 46px;
  justify-content: space-between;
  border-bottom: 1px solid var(--border);
}

.module-toolbar > div {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.module-toolbar h3,
.configuration-section h4 {
  margin: 0;
  font-size: 14px;
}

.module-toolbar span {
  color: var(--muted);
  font-size: 11px;
}

.module-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 310px), 1fr));
  gap: 10px;
  padding: 14px 0;
}

.module-edit-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(160px, 1fr) auto auto;
  align-items: end;
  gap: 9px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}

.module-edit-row__wide {
  grid-column: span 2;
}

.module-edit-row label,
.data-form label,
.profile-form label,
.sim-form label,
.configuration-control-row label {
  display: grid;
  gap: 5px;
}

.module-edit-row label > span,
.data-form label > span,
.profile-form label > span,
.sim-form label > span,
.configuration-control-row label > span {
  color: var(--muted);
  font-size: 10px;
  font-weight: 650;
}

.module-edit-row input,
.data-form input,
.data-form select,
.profile-form input,
.profile-form select,
.sim-form input,
.sim-form select,
.configuration-control-row select,
.ussd-form input {
  width: 100%;
  height: 36px;
  min-width: 0;
  padding: 0 9px;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
}

.primary-action,
.secondary-action {
  display: inline-flex;
  min-height: 34px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 11px;
  font-size: 10px;
  font-weight: 650;
  border-radius: 5px;
}

.primary-action {
  color: #fff;
  background: var(--accent);
}

.secondary-action {
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
}

.primary-action:disabled,
.secondary-action:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.device-tabs {
  display: flex;
  min-width: 0;
  overflow-x: auto;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.device-tabs button {
  display: inline-flex;
  min-width: 92px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--muted);
  font-size: 11px;
  border-bottom: 2px solid transparent;
}

.device-tabs button.is-selected {
  color: var(--accent-strong);
  border-bottom-color: var(--accent);
}

.device-configuration__body {
  min-width: 0;
}

.configuration-section {
  padding: 18px 0;
  border-bottom: 1px solid var(--border);
}

.configuration-section > header {
  min-height: 32px;
  margin-bottom: 13px;
  color: var(--accent-strong);
}

.configuration-section > header h4 {
  color: var(--text);
}

.section-action {
  margin-left: auto;
}

.configuration-summary > header {
  justify-content: space-between;
}

.configuration-summary > header > div {
  display: flex;
  align-items: center;
  gap: 8px;
}

.configuration-summary > header span {
  color: var(--accent-strong);
  font-size: 10px;
}

.configuration-summary > header > strong {
  color: var(--muted);
  font-size: 10px;
  font-weight: 500;
}

.configuration-summary dl,
.configuration-facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px 20px;
  margin: 0;
}

.configuration-summary dt,
.configuration-facts dt {
  color: var(--muted);
  font-size: 9px;
}

.configuration-summary dd,
.configuration-facts dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  font-size: 11px;
}

.capability-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  border-top: 1px solid var(--border);
}

.capability-grid > div {
  display: grid;
  min-width: 0;
  min-height: 64px;
  align-content: center;
  gap: 3px;
  padding: 10px 12px;
  border-right: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.capability-grid span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 10px;
}

.capability-grid strong {
  font-size: 11px;
}

.capability-grid small {
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: 9px;
}

.configuration-toggle,
.configuration-readonly,
.voice-status,
.ussd-status {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.configuration-toggle > span:first-child,
.configuration-readonly,
.ussd-status {
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
}

.configuration-toggle small,
.configuration-readonly small {
  color: var(--muted);
  font-size: 10px;
}

.configuration-toggle__control {
  display: flex;
  align-items: center;
  gap: 7px;
}

.configuration-toggle input {
  position: relative;
  width: 42px;
  height: 24px;
  appearance: none;
  background: #d8dde2;
  border-radius: 12px;
}

.configuration-toggle input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.configuration-toggle input:checked {
  background: var(--accent);
}

.configuration-toggle input:checked::before {
  transform: translateX(18px);
}

.data-form {
  display: grid;
  grid-template-columns: minmax(160px, 1fr) 150px auto auto;
  align-items: end;
  gap: 9px;
}

.bearer-list,
.profile-list {
  display: grid;
  gap: 7px;
  margin-top: 12px;
}

.bearer-list article,
.profile-list > div {
  display: grid;
  min-width: 0;
  min-height: 48px;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.bearer-list article {
  padding: 8px 10px;
}

.profile-list > div {
  grid-template-columns: minmax(0, 1fr) auto;
  padding: 4px;
}

.profile-list > div > button:first-child {
  display: grid;
  min-width: 0;
  min-height: 38px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 4px 6px;
  color: inherit;
  text-align: left;
  background: transparent;
}

.bearer-list span,
.bearer-list small,
.profile-list small,
.profile-list > div > button > span:nth-child(2) {
  color: var(--muted);
  font-size: 10px;
}

.profile-list > div.is-selected {
  border-color: var(--accent);
}

.profile-list > div > button > span:first-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.profile-form,
.sim-form {
  display: grid;
  grid-template-columns: repeat(5, minmax(120px, 1fr)) auto;
  align-items: end;
  gap: 9px;
  margin-top: 12px;
}

.sim-form {
  grid-template-columns: repeat(4, minmax(130px, 1fr)) auto;
}

.inline-check {
  display: flex !important;
  min-height: 36px;
  align-items: center;
  gap: 7px;
}

.inline-check input {
  width: 15px;
  height: 15px;
}

.retry-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.retry-row span {
  padding: 4px 7px;
  color: var(--muted);
  font-size: 9px;
  background: var(--surface-subtle);
  border-radius: 4px;
}

.configuration-control-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  align-items: end;
  gap: 9px;
}

.configuration-control-row + .configuration-facts {
  margin-top: 13px;
}

.voice-capabilities {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.voice-status {
  justify-content: flex-start;
  padding: 0 12px;
  color: var(--danger);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.voice-status span {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.voice-status small {
  color: var(--muted);
  font-size: 10px;
}

.voice-status.is-available {
  color: var(--accent-strong);
}

.inline-error,
.inline-warning {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 10px 0;
  color: var(--danger);
  font-size: 10px;
}

.inline-warning {
  color: #8a4b10;
}

.ussd-form {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.ussd-form input {
  flex: 1;
}

.ussd-status {
  gap: 4px;
}

.ussd-status span {
  color: var(--muted);
  font-size: 10px;
}

pre {
  margin: 12px 0 0;
  padding: 10px;
  overflow: auto;
  font-size: 11px;
  white-space: pre-wrap;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

@media (max-width: 980px) {
  .profile-form,
  .sim-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .module-edit-row,
  .data-form,
  .profile-form,
  .sim-form,
  .configuration-control-row,
  .configuration-summary dl,
  .configuration-facts {
    grid-template-columns: 1fr;
  }

  .module-edit-row__wide {
    grid-column: auto;
  }

  .primary-action,
  .secondary-action {
    width: 100%;
  }

  .module-toolbar > .secondary-action,
  .section-action {
    width: auto;
  }

  .ussd-form {
    flex-direction: column;
  }

  .voice-capabilities {
    grid-template-columns: 1fr;
  }
}
</style>
