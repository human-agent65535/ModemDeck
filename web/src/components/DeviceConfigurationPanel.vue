<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  AlertCircle,
  LoaderCircle,
  Network,
  PhoneIncoming,
  Power,
  RadioTower,
  ShieldAlert
} from '@lucide/vue'
import StatePanel from './StatePanel.vue'
import type {
  DeviceFeatureCapability,
  IncomingCallPolicy,
  IPFamily,
  LineSummary
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
  lineKey,
  lineLabel,
  loadBootstrap
} from '../state/workspace'

const apn = ref('')
const ipFamily = ref<IPFamily>('auto')
const incomingPolicyDraft = ref<IncomingCallPolicy>('follow_global')
const voltePolicyDraft = ref<'enabled' | 'disabled'>('disabled')

const lines = computed(() => bootstrapResource.data?.lines || [])
const selectedLineID = computed(() => deviceConfigurationState.selectedLineID)
const selectedLine = computed(() =>
  lines.value.find(line => line.id === selectedLineID.value)
)
const selectedResource = computed(() =>
  selectedLineID.value ? deviceConfigurationResource(selectedLineID.value) : null
)
const configuration = computed(() => selectedResource.value?.data || null)
const hardware = computed(() => configuration.value?.hardware)
const incomingCalls = computed(() => configuration.value?.incoming_calls)
const savingOperation = computed(() => selectedResource.value?.savingOperation || '')
const hardwareBusy = computed(() => savingOperation.value !== '')
const otherCapabilities = computed(() => {
  const capabilities = hardware.value?.capabilities
  if (!capabilities) return []
  return [
    { id: 'flight_mode', label: '飞行模式', capability: capabilities.flight_mode },
    { id: 'vowifi', label: 'VoWiFi', capability: capabilities.vowifi },
    { id: 'alias', label: '设备别名', capability: capabilities.alias },
    { id: 'esim', label: 'eSIM', capability: capabilities.esim },
    { id: 'at_terminal', label: 'AT 终端', capability: capabilities.at_terminal },
    { id: 'ussd', label: 'USSD', capability: capabilities.ussd },
    {
      id: 'connection_profile',
      label: '连接配置档',
      capability: capabilities.connection_profile
    }
  ]
})

watch(
  lines,
  currentLines => {
    if (
      selectedLineID.value &&
      !currentLines.some(line => line.id === selectedLineID.value)
    ) {
      selectDeviceConfiguration('')
    }
  },
  { deep: false }
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
    if (hardware.value?.volte.policy_known && hardware.value.volte.policy) {
      voltePolicyDraft.value = hardware.value.volte.policy
    } else {
      voltePolicyDraft.value = 'disabled'
    }
  }
)

function chooseLine(event: Event): void {
  const lineID = (event.target as HTMLSelectElement).value
  selectDeviceConfiguration(lineID)
  if (lineID) void loadDeviceConfiguration(lineID)
}

function selectOptionLabel(line: LineSummary): string {
  const secondary = line.phone_number || line.operator || line.device_imei
  return secondary ? `${lineLabel(line)} · ${secondary}` : lineLabel(line)
}

function readOnlyReason(capability: DeviceFeatureCapability): string {
  if (capability.reason) return capability.reason
  if (!capability.supported) return '当前后端不支持此能力'
  if (!capability.implemented) return '当前版本尚未实现此能力'
  if (!capability.readable) return '当前后端不能读取此状态'
  if (!capability.writable) return '当前后端仅提供只读状态'
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

async function changeRadio(event: Event): Promise<void> {
  if (!selectedLineID.value) return
  const control = event.target as HTMLInputElement
  const enabled = control.checked
  if (
    !enabled &&
    !window.confirm('关闭蜂窝射频会中断该模组的驻网与数据连接。继续关闭？')
  ) {
    control.checked = Boolean(hardware.value?.radio.enabled)
    return
  }
  await setRadioEnabled(selectedLineID.value, enabled)
}

async function applyDataConnection(): Promise<void> {
  if (!selectedLineID.value) return
  await connectData(selectedLineID.value, apn.value, ipFamily.value)
}

async function stopDataConnection(): Promise<void> {
  if (!selectedLineID.value) return
  if (!window.confirm('断开该线路的全部数据 bearer？')) return
  await disconnectData(selectedLineID.value)
}

async function applyVoLTE(): Promise<void> {
  if (!selectedLineID.value) return
  const current = hardware.value?.volte.policy
  const currentLabel = current === 'enabled' ? '开启' : '关闭'
  const nextLabel = voltePolicyDraft.value === 'enabled' ? '开启' : '关闭'
  if (
    !window.confirm(
      `将 VoLTE 策略从“${currentLabel}”改为“${nextLabel}”会写入模组。继续？`
    )
  ) {
    return
  }
  await setVoLTEPolicy(selectedLineID.value, voltePolicyDraft.value)
}

async function applyIncomingPolicy(): Promise<void> {
  if (!selectedLineID.value) return
  await setIncomingCallPolicy(selectedLineID.value, incomingPolicyDraft.value)
}

onMounted(() => {
  void loadBootstrap()
})
</script>

<template>
  <section class="device-configuration" aria-labelledby="device-configuration-title">
    <header class="device-configuration__header">
      <span class="device-configuration__icon"><RadioTower :size="19" /></span>
      <div>
        <h3 id="device-configuration-title">设备配置</h3>
        <p>选择一条线路后读取并修改模组的实时配置</p>
      </div>
    </header>

    <StatePanel
      v-if="bootstrapResource.status === 'loading' || bootstrapResource.status === 'idle'"
      state="loading"
      title="正在载入线路"
    />
    <StatePanel
      v-else-if="bootstrapResource.status === 'forbidden'"
      state="forbidden"
      title="无权查看线路"
      :detail="bootstrapResource.error"
    />
    <StatePanel
      v-else-if="bootstrapResource.status === 'error'"
      state="error"
      title="无法载入线路"
      :detail="bootstrapResource.error"
      retryable
      @retry="loadBootstrap(true)"
    />
    <StatePanel
      v-else-if="lines.length === 0"
      state="empty"
      title="没有可配置线路"
      detail="Host agent 当前没有返回任何线路。"
    />

    <template v-else>
      <label class="device-configuration__selector">
        <span>线路 / 模组</span>
        <select :value="selectedLineID" @change="chooseLine">
          <option value="">选择线路</option>
          <option
            v-for="line in lines"
            :key="lineKey(line)"
            :value="line.id || ''"
            :disabled="!line.id"
          >
            {{ selectOptionLabel(line) }}{{ line.id ? '' : ' · 缺少线路 ID' }}
          </option>
        </select>
        <small>共 {{ lines.length }} 条；不会自动选择或隐藏线路</small>
      </label>

      <StatePanel
        v-if="!selectedLineID"
        state="empty"
        title="请选择要配置的线路"
        detail="即使当前只有一条线路，也需要明确选择后才会读取或写入设备。"
      />
      <StatePanel
        v-else-if="
          selectedResource?.status === 'loading' || selectedResource?.status === 'idle'
        "
        state="loading"
        title="正在读取设备配置"
      />
      <StatePanel
        v-else-if="selectedResource?.status === 'forbidden'"
        state="forbidden"
        title="无权查看设备配置"
        :detail="selectedResource.error"
      />
      <StatePanel
        v-else-if="selectedResource?.status === 'error' && !configuration"
        state="error"
        title="无法读取设备配置"
        :detail="selectedResource.error"
        retryable
        @retry="loadDeviceConfiguration(selectedLineID, true)"
      />

      <div v-else-if="hardware && incomingCalls" class="device-configuration__body">
        <p v-if="selectedResource?.error" class="device-configuration__error" role="alert">
          <AlertCircle :size="16" />
          {{ selectedResource.error }}
        </p>

        <section class="configuration-section configuration-summary">
          <header>
            <div>
              <h4>{{ selectedLine ? lineLabel(selectedLine) : hardware.line_id }}</h4>
              <p>
                {{ hardware.identity.manufacturer || '制造商未知' }}
                {{ hardware.identity.model || '型号未知' }}
              </p>
            </div>
            <span>{{ selectedLine?.state || '状态未报告' }}</span>
          </header>
          <dl>
            <div>
              <dt>线路 ID</dt>
              <dd>{{ hardware.line_id }}</dd>
            </div>
            <div>
              <dt>设备标识</dt>
              <dd>{{ hardware.identity.equipment_identifier || '未报告' }}</dd>
            </div>
            <div>
              <dt>固件</dt>
              <dd>{{ hardware.identity.firmware || '未报告' }}</dd>
            </div>
            <div>
              <dt>观测时间</dt>
              <dd>{{ new Date(hardware.observed_at).toLocaleString() }}</dd>
            </div>
          </dl>
        </section>

        <section class="configuration-section" aria-labelledby="incoming-policy-title">
          <header>
            <span class="configuration-section__icon"><PhoneIncoming :size="18" /></span>
            <div>
              <h4 id="incoming-policy-title">线路来电策略</h4>
              <p>此线路可跟随全局设置，也可单独覆盖</p>
            </div>
          </header>
          <div class="configuration-control-row">
            <label class="configuration-field">
              <span>线路策略</span>
              <select
                v-model="incomingPolicyDraft"
                :disabled="Boolean(savingOperation)"
              >
                <option value="follow_global">跟随全局</option>
                <option value="receive">接听来电</option>
                <option value="do_not_disturb">免打扰</option>
              </select>
            </label>
            <button
              class="configuration-action"
              type="button"
              :disabled="
                Boolean(savingOperation) ||
                incomingPolicyDraft === incomingCalls.policy
              "
              @click="applyIncomingPolicy"
            >
              <LoaderCircle
                v-if="savingOperation === 'set_incoming_call_policy'"
                class="spin"
                :size="16"
              />
              保存
            </button>
          </div>
          <dl class="configuration-facts">
            <div>
              <dt>当前设置</dt>
              <dd>{{ policyLabel(incomingCalls.policy) }}</dd>
            </div>
            <div>
              <dt>实际策略</dt>
              <dd>{{ policyLabel(incomingCalls.effective_policy) }}</dd>
            </div>
            <div>
              <dt>全局设置</dt>
              <dd>{{ incomingCalls.global_receive_calls ? '接听来电' : '免打扰' }}</dd>
            </div>
          </dl>
          <p
            v-if="incomingCalls.enforcement.config_only"
            class="configuration-notice configuration-notice--warning"
          >
            <ShieldAlert :size="16" />
            <span>
              此策略可以保存，但当前模组未报告拒接能力，ModemDeck 不能承诺自动拒接。
              {{ incomingCalls.enforcement.reason || '' }}
            </span>
          </p>
          <p v-else-if="incomingCalls.enforcement.available" class="configuration-notice">
            <PhoneIncoming :size="16" />
            <span>
              执行能力可用；仅对新进入响铃状态的来电提交
              {{ incomingCalls.enforcement.max_submissions_per_call }} 次拒接。
            </span>
          </p>
          <p v-if="incomingCalls.last_action" class="configuration-last-action">
            最近一次策略执行：{{ incomingCalls.last_action.status }}
            <template v-if="incomingCalls.last_action.error_code">
              · {{ incomingCalls.last_action.error_code }}
            </template>
          </p>
        </section>

        <section class="configuration-section" aria-labelledby="radio-title">
          <header>
            <span class="configuration-section__icon"><Power :size="18" /></span>
            <div>
              <h4 id="radio-title">蜂窝射频</h4>
              <p>ModemManager 报告的射频与电源状态</p>
            </div>
          </header>
          <label
            v-if="
              hardware.capabilities.radio.writable &&
              hardware.radio.enabled_known
            "
            class="configuration-toggle"
          >
            <span>
              <strong>开启射频</strong>
              <small>
                {{ hardware.radio.enabled ? '已开启' : '已关闭' }} ·
                {{ hardware.radio.power_state || '电源状态未知' }}
              </small>
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
            <strong>
              {{
                hardware.radio.enabled_known
                  ? hardware.radio.enabled
                    ? '射频已开启'
                    : '射频已关闭'
                  : '射频状态未知'
              }}
            </strong>
            <small>{{ readOnlyReason(hardware.capabilities.radio) }}</small>
          </div>
        </section>

        <section class="configuration-section" aria-labelledby="data-title">
          <header>
            <span class="configuration-section__icon"><Network :size="18" /></span>
            <div>
              <h4 id="data-title">数据连接</h4>
              <p>APN 与 IP family 只用于建立真实数据 bearer</p>
            </div>
          </header>

          <div
            v-if="hardware.capabilities.data_connection.writable"
            class="data-connection-form"
          >
            <label class="configuration-field">
              <span>APN（可选）</span>
              <input
                v-model.trim="apn"
                type="text"
                autocomplete="off"
                placeholder="留空自动选择"
                :disabled="hardwareBusy"
              />
            </label>
            <label class="configuration-field">
              <span>IP family</span>
              <select v-model="ipFamily" :disabled="hardwareBusy">
                <option value="auto">自动</option>
                <option value="ipv4">IPv4</option>
                <option value="ipv6">IPv6</option>
                <option value="ipv4v6">IPv4 + IPv6</option>
              </select>
            </label>
            <button
              class="configuration-action"
              type="button"
              :disabled="hardwareBusy"
              @click="applyDataConnection"
            >
              <LoaderCircle
                v-if="savingOperation === 'connect_data'"
                class="spin"
                :size="16"
              />
              {{ savingOperation === 'connect_data' ? '正在连接' : '建立数据连接' }}
            </button>
            <button
              v-if="hardware.network_enabled"
              class="configuration-action configuration-action--secondary"
              type="button"
              :disabled="hardwareBusy"
              @click="stopDataConnection"
            >
              <LoaderCircle
                v-if="savingOperation === 'disconnect_data'"
                class="spin"
                :size="16"
              />
              {{ savingOperation === 'disconnect_data' ? '正在断开' : '断开全部' }}
            </button>
          </div>
          <div v-else class="configuration-readonly">
            <strong>{{ hardware.network_enabled ? '数据已连接' : '数据未连接' }}</strong>
            <small>{{ readOnlyReason(hardware.capabilities.data_connection) }}</small>
          </div>

          <div v-if="hardware.data_connections.length" class="bearer-list">
            <article
              v-for="connection in hardware.data_connections"
              :key="connection.id"
              class="bearer-row"
            >
              <header>
                <strong>{{ connection.apn || '自动 APN' }}</strong>
                <span>{{ connection.connected ? '已连接' : '未连接' }}</span>
              </header>
              <dl class="configuration-facts">
                <div>
                  <dt>IP family</dt>
                  <dd>{{ connection.ip_family || '未报告' }}</dd>
                </div>
                <div>
                  <dt>接口</dt>
                  <dd>{{ connection.interface || '未报告' }}</dd>
                </div>
                <div v-if="connection.ipv4.address">
                  <dt>IPv4</dt>
                  <dd>{{ connection.ipv4.address }}/{{ connection.ipv4.prefix }}</dd>
                </div>
                <div v-if="connection.ipv6.address">
                  <dt>IPv6</dt>
                  <dd>{{ connection.ipv6.address }}/{{ connection.ipv6.prefix }}</dd>
                </div>
              </dl>
            </article>
          </div>
          <p v-else class="configuration-empty-value">当前没有数据 bearer。</p>
        </section>

        <section class="configuration-section" aria-labelledby="volte-title">
          <header>
            <span class="configuration-section__icon"><RadioTower :size="18" /></span>
            <div>
              <h4 id="volte-title">VoLTE 策略</h4>
              <p>仅在精确厂商、型号和固件 profile 可读写时开放</p>
            </div>
          </header>
          <div
            v-if="
              hardware.capabilities.volte.writable &&
              hardware.volte.policy_known
            "
            class="configuration-control-row"
          >
            <label class="configuration-field">
              <span>策略</span>
              <select v-model="voltePolicyDraft" :disabled="hardwareBusy">
                <option value="enabled">开启</option>
                <option value="disabled">关闭</option>
              </select>
            </label>
            <button
              class="configuration-action"
              type="button"
              :disabled="
                hardwareBusy || voltePolicyDraft === hardware.volte.policy
              "
              @click="applyVoLTE"
            >
              <LoaderCircle
                v-if="savingOperation === 'set_volte_policy'"
                class="spin"
                :size="16"
              />
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
                  : '策略未知'
              }}
            </strong>
            <small>{{ readOnlyReason(hardware.capabilities.volte) }}</small>
          </div>
          <p v-if="hardware.volte.profile_id" class="configuration-profile">
            Profile: {{ hardware.volte.profile_id }}
          </p>
        </section>

        <section class="configuration-section" aria-labelledby="other-capabilities-title">
          <header>
            <span class="configuration-section__icon"><ShieldAlert :size="18" /></span>
            <div>
              <h4 id="other-capabilities-title">其他设备能力</h4>
              <p>未开放能力只显示服务端原因，不生成无效控件</p>
            </div>
          </header>
          <dl class="capability-table">
            <div v-for="item in otherCapabilities" :key="item.id">
              <dt>
                <strong>{{ item.label }}</strong>
                <small>{{ item.capability.backend || '后端未报告' }}</small>
              </dt>
              <dd>
                <strong>{{ capabilityStatus(item.capability) }}</strong>
                <small>{{ readOnlyReason(item.capability) }}</small>
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.device-configuration {
  width: min(100%, 880px);
}

.device-configuration__header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.device-configuration__icon,
.configuration-section__icon {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: 50%;
}

.device-configuration__icon {
  width: 36px;
  height: 36px;
}

.device-configuration__header h3,
.configuration-section h4 {
  font-size: 14px;
}

.device-configuration__header p,
.configuration-section header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.device-configuration__selector {
  display: grid;
  gap: 6px;
  padding: 18px 0;
  border-bottom: 1px solid var(--border);
}

.device-configuration__selector > span,
.configuration-field > span {
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
}

.device-configuration__selector select,
.configuration-field select,
.configuration-field input {
  width: 100%;
  min-height: 38px;
  padding: 0 10px;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
}

.device-configuration__selector small {
  color: var(--muted);
  font-size: 10px;
}

.device-configuration__body {
  min-width: 0;
}

.device-configuration__error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 14px 0 0;
  padding: 10px 12px;
  color: var(--danger);
  font-size: 11px;
  line-height: 1.45;
  background: var(--danger-soft);
  border-radius: 5px;
}

.configuration-section {
  padding: 18px 0;
  border-bottom: 1px solid var(--border);
}

.configuration-section > header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}

.configuration-section__icon {
  width: 32px;
  height: 32px;
}

.configuration-summary > header {
  justify-content: space-between;
}

.configuration-summary > header > div {
  min-width: 0;
}

.configuration-summary > header > span {
  flex: 0 0 auto;
  padding: 3px 7px;
  color: var(--accent);
  font-size: 10px;
  background: var(--accent-soft);
  border-radius: 4px;
}

.configuration-summary dl,
.configuration-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 18px;
}

.configuration-summary dl > div,
.configuration-facts > div {
  min-width: 0;
}

.configuration-summary dt,
.configuration-facts dt {
  color: var(--muted);
  font-size: 10px;
}

.configuration-summary dd,
.configuration-facts dd {
  overflow-wrap: anywhere;
  margin: 2px 0 0;
  font-size: 11px;
}

.configuration-control-row,
.data-connection-form {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto;
  align-items: end;
  gap: 10px;
}

.data-connection-form {
  grid-template-columns: minmax(180px, 1fr) minmax(140px, 180px) auto auto;
}

.configuration-field {
  display: grid;
  gap: 5px;
}

.configuration-action {
  display: inline-flex;
  min-height: 38px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 13px;
  color: #ffffff;
  font-size: 11px;
  font-weight: 650;
  background: var(--accent);
  border-radius: 5px;
}

.configuration-action--secondary {
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
}

.configuration-action:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.configuration-control-row + .configuration-facts {
  margin-top: 13px;
}

.configuration-notice {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  margin: 12px 0 0;
  padding: 9px 10px;
  color: var(--accent-strong);
  font-size: 11px;
  line-height: 1.45;
  background: var(--accent-soft);
  border-radius: 5px;
}

.configuration-notice--warning {
  color: #8a4b10;
  background: #fff3df;
}

.configuration-last-action,
.configuration-profile,
.configuration-empty-value {
  margin: 9px 0 0;
  color: var(--muted);
  font-size: 10px;
}

.configuration-toggle,
.configuration-readonly {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}

.configuration-toggle > span:first-child,
.configuration-readonly {
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
}

.configuration-toggle small,
.configuration-readonly small {
  margin-top: 3px;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.45;
}

.configuration-toggle__control {
  display: flex;
  align-items: center;
  gap: 8px;
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
  background: #ffffff;
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

.configuration-toggle input:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.bearer-list {
  display: grid;
  gap: 9px;
  margin-top: 14px;
}

.bearer-row {
  padding: 10px 11px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.bearer-row > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  font-size: 11px;
}

.bearer-row > header span {
  color: var(--accent);
  font-size: 10px;
}

.capability-table {
  display: grid;
}

.capability-table > div {
  display: grid;
  min-height: 54px;
  grid-template-columns: minmax(130px, 0.35fr) minmax(0, 0.65fr);
  align-items: center;
  gap: 20px;
  border-top: 1px solid var(--border);
}

.capability-table dt,
.capability-table dd {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
  margin: 0;
}

.capability-table strong {
  font-size: 11px;
}

.capability-table small {
  overflow-wrap: anywhere;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.4;
}

@media (max-width: 760px) {
  .configuration-summary dl,
  .configuration-facts,
  .configuration-control-row,
  .data-connection-form,
  .capability-table > div {
    grid-template-columns: 1fr;
  }

  .configuration-action {
    width: 100%;
  }
}
</style>
