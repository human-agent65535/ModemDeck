<script setup lang="ts">
import {
  Activity,
  AudioLines,
  AlertTriangle,
  CheckCircle2,
  ChevronsDown,
  Database,
  Download,
  LoaderCircle,
  MessageSquare,
  Pause,
  PhoneCall,
  Play,
  RadioTower,
  Search,
  Server,
  Trash2,
  Volume2,
  XCircle
} from '@lucide/vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fixtureMode, gateway } from '../api/client'
import type {
  DiagnosticActiveCall,
  DiagnosticLogEntry,
  DiagnosticLogLevel,
  DiagnosticLogQuery,
  DiagnosticsSnapshot,
  LineSummary
} from '../api/types'
import { ApiError } from '../api/types'
import { audioState, refreshAudioDevices } from '../state/audio'
import { lineLabel } from '../state/workspace'
import StatePanel from './StatePanel.vue'

type SnapshotState = 'idle' | 'loading' | 'ready' | 'forbidden' | 'error'
type LogConnectionState =
  | 'connecting'
  | 'live'
  | 'reconnecting'
  | 'paused'
  | 'fixture'
  | 'error'

const MAX_LOCAL_LOGS = 1000
const HISTORY_LIMIT = 500
const SNAPSHOT_INTERVAL_MS = 10_000

const snapshot = ref<DiagnosticsSnapshot | null>(null)
const snapshotState = ref<SnapshotState>('idle')
const snapshotError = ref('')
const logs = ref<DiagnosticLogEntry[]>([])
const logsLoading = ref(false)
const logsTruncated = ref(false)
const logError = ref('')
const logLevel = ref<DiagnosticLogLevel | ''>('')
const logComponent = ref('')
const logSearch = ref('')
const connectionState = ref<LogConnectionState>(
  fixtureMode ? 'fixture' : 'connecting'
)
const paused = ref(false)
const autoFollow = ref(true)
const downloading = ref(false)
const logViewport = ref<HTMLElement | null>(null)
const lastSeenID = ref(0)

let closeLogStream: (() => void) | null = null
let reconnectTimer: number | undefined
let filterTimer: number | undefined
let snapshotTimer: number | undefined
let reconnectAttempt = 0
let streamGeneration = 0
let disposed = false

const fullTimestampFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false
})
const timeTimestampFormatter = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false
})

const statusLabel = computed(() => {
  switch (snapshot.value?.status) {
    case 'ok':
      return '正常'
    case 'degraded':
      return '部分可用'
    case 'unavailable':
      return '不可用'
    default:
      return '读取中'
  }
})

const browserAudioSupported = computed(
  () =>
    typeof RTCPeerConnection !== 'undefined' &&
    typeof navigator !== 'undefined' &&
    typeof navigator.mediaDevices?.getUserMedia === 'function'
)

const browserAudioAvailable = computed(
  () => browserAudioSupported.value && audioState.devicesStatus !== 'error'
)

const browserAudioDetail = computed(() => {
  if (!browserAudioSupported.value) return 'WebRTC 音频不可用'
  if (audioState.devicesStatus === 'idle' || audioState.devicesStatus === 'loading') {
    return '正在读取音频设备'
  }
  if (audioState.devicesStatus === 'error') {
    return audioState.devicesError || '无法读取音频设备'
  }
  return `输入 ${audioState.inputs.length} · 输出 ${audioState.outputs.length}`
})

const connectionLabel = computed(() => {
  switch (connectionState.value) {
    case 'live':
      return '实时'
    case 'connecting':
      return '连接中'
    case 'reconnecting':
      return '重连中'
    case 'paused':
      return '已暂停'
    case 'fixture':
      return '静态数据'
    default:
      return '已断开'
  }
})

const runtimeErrors = computed(() => {
  const current = snapshot.value
  if (!current) return []
  return [
    { scope: '数据库', message: current.database.error },
    { scope: 'Host agent', message: current.host_agent.last_error },
    { scope: '通话运行时', message: current.call_runtime.error }
  ].filter((item): item is { scope: string; message: string } => Boolean(item.message))
})

const knownComponents = computed(() =>
  Array.from(new Set(logs.value.map(entry => entry.component))).sort((left, right) =>
    left.localeCompare(right)
  )
)

function lineCapabilities(line: LineSummary) {
  return [
    { name: '模组控制', available: line.capabilities?.modem === true },
    { name: '语音通话', available: line.capabilities?.voice === true },
    { name: 'SIM 卡', available: line.capabilities?.sim === true },
    { name: '短信', available: line.capabilities?.messaging === true }
  ]
}

function lineStateLabel(state?: string): string {
  switch (state?.toLowerCase()) {
    case 'connected':
      return '已连接'
    case 'registered':
      return '已驻网'
    case 'enabled':
      return '已启用'
    case 'searching':
      return '正在搜网'
    case 'connecting':
      return '正在连接'
    case 'disconnecting':
      return '正在断开'
    case 'disabled':
      return '已停用'
    case 'disabling':
      return '正在停用'
    case 'enabling':
      return '正在启用'
    case 'locked':
      return 'SIM 已锁定'
    case 'failed':
      return '异常'
    case 'initializing':
      return '正在初始化'
    default:
      return state || '状态未知'
  }
}

function lineStateTone(state?: string): 'positive' | 'warning' | 'negative' | 'neutral' {
  switch (state?.toLowerCase()) {
    case 'connected':
    case 'registered':
      return 'positive'
    case 'enabled':
    case 'searching':
    case 'connecting':
    case 'disconnecting':
    case 'disabling':
    case 'enabling':
    case 'initializing':
      return 'warning'
    case 'failed':
    case 'locked':
      return 'negative'
    default:
      return 'neutral'
  }
}

function lineSignalLabel(line: LineSummary): string {
  return line.signal_quality === undefined ? '未报告' : `${line.signal_quality}%`
}

function agentCapabilities() {
  const capabilities = snapshot.value?.host_agent.capabilities
  if (!capabilities) return []
  return [
    { name: '发现', available: capabilities.discovery },
    { name: '快照', available: capabilities.snapshot },
    { name: '设备配置', available: capabilities.device_configuration },
    { name: 'SIM / PIN', available: capabilities.sim_management },
    { name: '连接配置', available: capabilities.connection_profiles },
    { name: '网络状态', available: capabilities.network },
    { name: '代理', available: capabilities.proxy },
    { name: 'USSD', available: capabilities.ussd },
    { name: '拨号', available: capabilities.dial },
    { name: '短信', available: capabilities.send_message },
    { name: '浏览器音频', available: capabilities.media }
  ]
}

function callLineLabel(call: DiagnosticActiveCall): string {
  const line = snapshot.value?.lines.find(candidate => candidate.id === call.line_id)
  return line ? lineLabel(line) : call.line_id
}

function callDirectionLabel(direction: string): string {
  if (direction === 'incoming') return '来电'
  if (direction === 'outgoing') return '呼出'
  return direction || '方向未知'
}

function callPhaseLabel(phase: string): string {
  switch (phase) {
    case 'dialing':
      return '正在拨号'
    case 'ringing':
      return '正在响铃'
    case 'connecting':
      return '正在接通'
    case 'active':
      return '通话中'
    case 'ending':
      return '正在结束'
    case 'ended':
      return '已结束'
    case 'failed':
      return '失败'
    default:
      return phase || '状态未知'
  }
}

function callBearerLabel(bearer: string): string {
  switch (bearer.toLowerCase()) {
    case 'volte':
      return 'VoLTE'
    case 'vowifi':
      return 'VoWiFi'
    case 'gsm':
    case 'cs':
    case 'circuit-switched':
      return 'GSM / CS'
    default:
      return bearer || '承载未确认'
  }
}

function audioDescription(call: DiagnosticActiveCall): string {
  if (!call.media_available) return '音频通道未建立'
  const values = [
    call.audio_encoding,
    call.audio_resolution,
    call.audio_rate ? `${call.audio_rate / 1000} kHz` : ''
  ].filter(Boolean)
  return values.length ? values.join(' · ') : '音频可用'
}

function formatTimestamp(value: string, timeOnly = false): string {
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return value || '—'
  return (timeOnly ? timeTimestampFormatter : fullTimestampFormatter).format(
    new Date(parsed)
  )
}

function formatFields(fields?: Record<string, unknown>): string {
  if (!fields) return ''
  return Object.entries(fields)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => {
      if (typeof value === 'string') return `${key}=${value}`
      try {
        return `${key}=${JSON.stringify(value)}`
      } catch {
        return `${key}=${String(value)}`
      }
    })
    .join(' ')
}

function currentLogQuery(after?: number): DiagnosticLogQuery {
  return {
    limit: HISTORY_LIMIT,
    ...(after && after > 0 ? { after } : {}),
    ...(logLevel.value ? { level: logLevel.value } : {}),
    ...(logComponent.value.trim() ? { component: logComponent.value.trim() } : {}),
    ...(logSearch.value.trim() ? { search: logSearch.value.trim() } : {})
  }
}

function closeStream(): void {
  closeLogStream?.()
  closeLogStream = null
  if (reconnectTimer !== undefined) {
    window.clearTimeout(reconnectTimer)
    reconnectTimer = undefined
  }
}

async function scrollToLatest(): Promise<void> {
  if (!autoFollow.value) return
  await nextTick()
  const viewport = logViewport.value
  if (viewport) viewport.scrollTop = viewport.scrollHeight
}

function appendLog(entry: DiagnosticLogEntry): void {
  const next = logs.value.filter(current => current.id !== entry.id)
  next.push(entry)
  next.sort((left, right) => left.id - right.id)
  logs.value = next.slice(-MAX_LOCAL_LOGS)
  lastSeenID.value = Math.max(lastSeenID.value, entry.id)
  logError.value = ''
  void scrollToLatest()
}

function scheduleReconnect(generation: number): void {
  if (disposed || paused.value || fixtureMode || generation !== streamGeneration) return
  connectionState.value = 'reconnecting'
  const delay = Math.min(10_000, 1000 * 2 ** reconnectAttempt)
  reconnectAttempt += 1
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = undefined
    connectLogStream(generation)
  }, delay)
}

function connectLogStream(generation = streamGeneration): void {
  if (disposed || paused.value || fixtureMode || generation !== streamGeneration) return
  closeStream()
  connectionState.value = reconnectAttempt > 0 ? 'reconnecting' : 'connecting'
  closeLogStream = gateway.subscribeDiagnosticLogs(currentLogQuery(lastSeenID.value), {
    onOpen() {
      if (generation !== streamGeneration || disposed) return
      reconnectAttempt = 0
      connectionState.value = 'live'
      logError.value = ''
    },
    onEntry(entry) {
      if (generation !== streamGeneration || disposed || paused.value) return
      appendLog(entry)
    },
    onReset(oldestID, _newestID) {
      if (generation !== streamGeneration || disposed) return
      logs.value = []
      logsTruncated.value = true
      lastSeenID.value = oldestID > 0 ? oldestID - 1 : 0
    },
    onError(error) {
      if (generation !== streamGeneration || disposed || paused.value) return
      closeLogStream = null
      logError.value = error?.message || '实时日志连接中断'
      scheduleReconnect(generation)
    }
  })
}

async function loadSnapshot(): Promise<void> {
  if (snapshotState.value === 'loading') return
  if (!snapshot.value) snapshotState.value = 'loading'
  try {
    snapshot.value = await gateway.getDiagnostics()
    snapshotState.value = 'ready'
    snapshotError.value = ''
  } catch (error) {
    snapshotError.value = error instanceof Error ? error.message : '无法读取诊断状态'
    if (snapshot.value) {
      snapshotState.value = 'ready'
      return
    }
    snapshotState.value =
      error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
  }
}

async function loadLogs(): Promise<void> {
  const generation = ++streamGeneration
  closeStream()
  reconnectAttempt = 0
  logsLoading.value = true
  logError.value = ''
  if (!fixtureMode && !paused.value) connectionState.value = 'connecting'
  try {
    const page = await gateway.listDiagnosticLogs(currentLogQuery())
    if (generation !== streamGeneration || disposed) return
    logs.value = page.entries.slice(-MAX_LOCAL_LOGS)
    logsTruncated.value = page.truncated || page.entries.length > MAX_LOCAL_LOGS
    lastSeenID.value = Math.max(
      page.newest_id,
      ...page.entries.map(entry => entry.id),
      0
    )
    await scrollToLatest()
    if (fixtureMode) {
      connectionState.value = 'fixture'
    } else if (paused.value) {
      connectionState.value = 'paused'
    } else {
      connectLogStream(generation)
    }
  } catch (error) {
    if (generation !== streamGeneration || disposed) return
    logError.value = error instanceof Error ? error.message : '无法读取运行日志'
    connectionState.value = 'error'
    scheduleReconnect(generation)
  } finally {
    if (generation === streamGeneration) logsLoading.value = false
  }
}

function togglePause(): void {
  if (fixtureMode) return
  paused.value = !paused.value
  if (paused.value) {
    closeStream()
    connectionState.value = 'paused'
    return
  }
  reconnectAttempt = 0
  connectLogStream()
}

function clearLocalLogs(): void {
  logs.value = []
  logsTruncated.value = false
}

async function downloadLogs(): Promise<void> {
  if (downloading.value) return
  downloading.value = true
  logError.value = ''
  try {
    const blob = await gateway.downloadDiagnosticLogs(currentLogQuery())
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `modemdeck-logs-${new Date().toISOString().replace(/[:.]/g, '-')}.ndjson`
    document.body.append(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
  } catch (error) {
    logError.value = error instanceof Error ? error.message : '日志下载失败'
  } finally {
    downloading.value = false
  }
}

function handleLogScroll(): void {
  const viewport = logViewport.value
  if (!viewport || !autoFollow.value) return
  if (viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight > 72) {
    autoFollow.value = false
  }
}

watch([logLevel, logComponent, logSearch], () => {
  if (filterTimer !== undefined) window.clearTimeout(filterTimer)
  filterTimer = window.setTimeout(() => {
    filterTimer = undefined
    void loadLogs()
  }, 300)
})

watch(autoFollow, enabled => {
  if (enabled) void scrollToLatest()
})

onMounted(() => {
  void loadSnapshot()
  void loadLogs()
  void refreshAudioDevices()
  snapshotTimer = window.setInterval(() => {
    void loadSnapshot()
  }, SNAPSHOT_INTERVAL_MS)
})

onBeforeUnmount(() => {
  disposed = true
  streamGeneration += 1
  closeStream()
  if (filterTimer !== undefined) window.clearTimeout(filterTimer)
  if (snapshotTimer !== undefined) window.clearInterval(snapshotTimer)
})
</script>

<template>
  <section class="diagnostics-panel" aria-labelledby="diagnostics-title">
    <StatePanel
      v-if="snapshotState === 'idle' || snapshotState === 'loading'"
      state="loading"
      title="正在读取运行状态"
    />
    <StatePanel
      v-else-if="snapshotState === 'forbidden'"
      state="forbidden"
      title="无权查看诊断信息"
      :detail="snapshotError"
    />
    <StatePanel
      v-else-if="snapshotState === 'error'"
      state="error"
      title="无法读取诊断状态"
      :detail="snapshotError"
      retryable
      @retry="loadSnapshot"
    />

    <template v-if="snapshot">
      <section class="diagnostics-section">
        <header class="section-heading">
          <div>
            <h3 id="diagnostics-title">运行状态</h3>
            <span>{{ formatTimestamp(snapshot.observed_at) }}</span>
          </div>
          <span class="overall-status" :class="`is-${snapshot.status}`">
            <Activity :size="15" />
            {{ statusLabel }}
          </span>
        </header>

        <p v-if="snapshotError" class="inline-error" role="alert">
          <AlertTriangle :size="15" />
          {{ snapshotError }}
        </p>

        <div class="service-grid">
          <article class="service-status" :class="{ 'is-unavailable': !snapshot.database.available }">
            <span class="service-status__icon"><Database :size="19" /></span>
            <span>
              <strong>数据库</strong>
              <small>{{ snapshot.database.available ? '可用' : '不可用' }}</small>
            </span>
            <CheckCircle2 v-if="snapshot.database.available" :size="18" />
            <XCircle v-else :size="18" />
          </article>
          <article class="service-status" :class="{ 'is-unavailable': !snapshot.host_agent.connected }">
            <span class="service-status__icon"><Server :size="19" /></span>
            <span>
              <strong>Host agent</strong>
              <small>{{ snapshot.host_agent.connected ? '已连接' : '未连接' }}</small>
            </span>
            <CheckCircle2 v-if="snapshot.host_agent.connected" :size="18" />
            <XCircle v-else :size="18" />
          </article>
          <article class="service-status" :class="{ 'is-unavailable': !snapshot.call_runtime.available }">
            <span class="service-status__icon"><PhoneCall :size="19" /></span>
            <span>
              <strong>通话运行时</strong>
              <small>{{ snapshot.call_runtime.available ? '可用' : '不可用' }}</small>
            </span>
            <CheckCircle2 v-if="snapshot.call_runtime.available" :size="18" />
            <XCircle v-else :size="18" />
          </article>
          <article class="service-status" :class="{ 'is-unavailable': !browserAudioAvailable }">
            <span class="service-status__icon"><AudioLines :size="19" /></span>
            <span>
              <strong>浏览器音频</strong>
              <small>{{ browserAudioDetail }}</small>
            </span>
            <CheckCircle2 v-if="browserAudioAvailable" :size="18" />
            <XCircle v-else :size="18" />
          </article>
        </div>

        <dl class="agent-facts">
          <div><dt>硬件服务</dt><dd>{{ snapshot.host_agent.provider || '—' }}</dd></div>
          <div><dt>Agent 版本</dt><dd>{{ snapshot.host_agent.agent_version || '—' }}</dd></div>
          <div><dt>运行时</dt><dd>{{ snapshot.host_agent.runtime_version || '—' }}</dd></div>
          <div><dt>构建版本</dt><dd>{{ snapshot.host_agent.revision || '—' }}</dd></div>
        </dl>
        <div class="agent-capabilities">
          <span
            v-for="capability in agentCapabilities()"
            :key="capability.name"
            class="capability-status"
            :class="{ 'is-available': capability.available }"
          >
            <CheckCircle2 v-if="capability.available" :size="13" />
            <XCircle v-else :size="13" />
            {{ capability.name }}
          </span>
        </div>

        <div v-if="runtimeErrors.length" class="runtime-errors">
          <p v-for="error in runtimeErrors" :key="error.scope">
            <AlertTriangle :size="15" />
            <strong>{{ error.scope }}</strong>
            <span>{{ error.message }}</span>
          </p>
        </div>
      </section>

      <section class="diagnostics-section">
        <header class="section-heading">
          <div>
            <h3>模组能力</h3>
            <span>{{ snapshot.lines.length }} 条线路</span>
          </div>
        </header>

        <div v-if="snapshot.lines.length" class="line-grid">
          <article
            v-for="line in snapshot.lines"
            :key="line.id || line.device_imei || line.iccid"
            class="line-status"
          >
            <header>
              <span class="line-status__icon"><RadioTower :size="18" /></span>
              <span class="line-status__identity">
                <strong>{{ lineLabel(line) }}</strong>
                <small>{{ line.model || '型号未报告' }}</small>
              </span>
              <span
                class="line-state"
                :class="`is-${lineStateTone(line.state)}`"
              >
                {{ lineStateLabel(line.state) }}
              </span>
            </header>
            <dl class="line-facts">
              <div>
                <dt>运营商</dt>
                <dd>{{ line.operator || '未识别' }}</dd>
              </div>
              <div>
                <dt>网络状态</dt>
                <dd>{{ lineStateLabel(line.state) }}</dd>
              </div>
              <div>
                <dt>信号强度</dt>
                <dd>{{ lineSignalLabel(line) }}</dd>
              </div>
              <div>
                <dt>SIM ICCID</dt>
                <dd>{{ line.iccid || '未报告' }}</dd>
              </div>
              <div>
                <dt>模组 IMEI</dt>
                <dd>{{ line.device_imei || '未报告' }}</dd>
              </div>
              <div>
                <dt>固件版本</dt>
                <dd>{{ line.firmware || '未报告' }}</dd>
              </div>
            </dl>
            <div class="capability-row">
              <span
                v-for="capability in lineCapabilities(line)"
                :key="capability.name"
                class="capability-status"
                :class="{ 'is-available': capability.available }"
              >
                <CheckCircle2 v-if="capability.available" :size="13" />
                <XCircle v-else :size="13" />
                {{ capability.name }}
              </span>
            </div>
          </article>
        </div>
        <p v-else class="empty-row">Host agent 未返回线路</p>
      </section>

      <section class="diagnostics-section">
        <header class="section-heading">
          <div>
            <h3>当前通话</h3>
            <span>{{ snapshot.active_calls.length }} 路</span>
          </div>
        </header>
        <div v-if="snapshot.active_calls.length" class="active-call-list">
          <article v-for="call in snapshot.active_calls" :key="call.id" class="active-call">
            <span class="active-call__icon"><PhoneCall :size="18" /></span>
            <span class="active-call__identity">
              <strong>{{ callLineLabel(call) }}</strong>
              <small>
                {{ callDirectionLabel(call.direction) }} ·
                {{ callPhaseLabel(call.phase) }} ·
                {{ callBearerLabel(call.bearer) }}
              </small>
            </span>
            <span class="active-call__audio" :class="{ 'is-unavailable': !call.media_available }">
              <Volume2 :size="15" />
              {{ audioDescription(call) }}
            </span>
          </article>
        </div>
        <p v-else class="empty-row">没有进行中的通话</p>
      </section>
    </template>

    <section class="diagnostics-section log-section">
      <header class="section-heading log-heading">
        <div>
          <h3>运行日志</h3>
          <span class="connection-state" :class="`is-${connectionState}`">
            <span />
            {{ connectionLabel }}
          </span>
        </div>
        <div class="log-actions">
          <label class="follow-control">
            <input v-model="autoFollow" type="checkbox" />
            <ChevronsDown :size="15" />
            <span>自动追尾</span>
          </label>
          <button
            class="diagnostic-action"
            type="button"
            :title="paused ? '继续实时日志' : '暂停实时日志'"
            :aria-label="paused ? '继续实时日志' : '暂停实时日志'"
            :disabled="fixtureMode"
            @click="togglePause"
          >
            <Play v-if="paused" :size="17" />
            <Pause v-else :size="17" />
          </button>
          <button
            class="diagnostic-action"
            type="button"
            title="清空本地日志"
            aria-label="清空本地日志"
            :disabled="logs.length === 0"
            @click="clearLocalLogs"
          >
            <Trash2 :size="17" />
          </button>
          <button
            class="diagnostic-action"
            type="button"
            title="下载日志"
            aria-label="下载日志"
            :disabled="downloading"
            @click="downloadLogs"
          >
            <LoaderCircle v-if="downloading" class="spin" :size="17" />
            <Download v-else :size="17" />
          </button>
        </div>
      </header>

      <div class="log-filters">
        <label>
          <span class="sr-only">日志级别</span>
          <select v-model="logLevel" aria-label="日志级别">
            <option value="">全部级别</option>
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
        </label>
        <label class="filter-input">
          <Server :size="15" />
          <span class="sr-only">组件</span>
          <input
            v-model="logComponent"
            type="search"
            list="diagnostic-components"
            maxlength="64"
            placeholder="组件"
            aria-label="组件"
          />
          <datalist id="diagnostic-components">
            <option v-for="component in knownComponents" :key="component" :value="component" />
          </datalist>
        </label>
        <label class="filter-input filter-input--search">
          <Search :size="15" />
          <span class="sr-only">搜索日志</span>
          <input
            v-model="logSearch"
            type="search"
            maxlength="200"
            placeholder="搜索日志"
            aria-label="搜索日志"
          />
        </label>
      </div>

      <p v-if="logError" class="inline-error" role="alert">
        <AlertTriangle :size="15" />
        {{ logError }}
      </p>
      <p v-if="logsTruncated" class="log-notice">仅显示当前缓冲区内的最新日志</p>

      <div
        ref="logViewport"
        class="log-viewport"
        role="log"
        aria-live="polite"
        @scroll.passive="handleLogScroll"
      >
        <div v-if="logsLoading && logs.length === 0" class="log-empty">
          <LoaderCircle class="spin" :size="18" />
          正在读取日志
        </div>
        <div v-else-if="logs.length === 0" class="log-empty">
          <MessageSquare :size="18" />
          暂无日志
        </div>
        <article
          v-for="entry in logs"
          v-else
          :key="entry.id"
          class="log-entry"
          :class="`is-${entry.level}`"
        >
          <time :datetime="entry.timestamp">{{ formatTimestamp(entry.timestamp, true) }}</time>
          <span class="log-level">{{ entry.level }}</span>
          <span class="log-component">{{ entry.component }}</span>
          <span class="log-message">{{ entry.message }}</span>
          <code v-if="entry.fields && Object.keys(entry.fields).length">
            {{ formatFields(entry.fields) }}
          </code>
          <small v-if="entry.caller">{{ entry.caller }}</small>
        </article>
      </div>
    </section>
  </section>
</template>

<style scoped>
.diagnostics-panel {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 26px;
}

.diagnostics-section {
  min-width: 0;
}

.section-heading {
  display: flex;
  min-height: 40px;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 11px;
  border-bottom: 1px solid var(--border);
}

.section-heading > div:first-child {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.section-heading h3 {
  margin: 0;
  color: var(--text);
  font-size: 16px;
  text-transform: none;
}

.section-heading span {
  color: var(--muted);
  font-size: 12px;
}

.overall-status,
.connection-state {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
}

.overall-status.is-degraded,
.connection-state.is-reconnecting,
.connection-state.is-connecting {
  color: #946200;
}

.overall-status.is-unavailable,
.connection-state.is-error {
  color: var(--danger);
}

.connection-state > span {
  width: 7px;
  height: 7px;
  background: currentColor;
  border-radius: 50%;
}

.connection-state.is-paused,
.connection-state.is-fixture {
  color: var(--muted);
}

.service-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border-bottom: 1px solid var(--border);
}

.service-status {
  display: grid;
  min-width: 0;
  min-height: 76px;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  color: var(--accent-strong);
  border-right: 1px solid var(--border);
}

.service-status:nth-child(2n) {
  border-right: 0;
}

.service-status:nth-child(n + 3) {
  border-top: 1px solid var(--border);
}

.service-status.is-unavailable {
  color: var(--danger);
}

.service-status__icon {
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  color: currentColor;
  background: var(--surface-hover);
  border-radius: 50%;
}

.service-status > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.service-status strong {
  overflow: hidden;
  color: var(--text);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.service-status small {
  color: currentColor;
  font-size: 12px;
}

.agent-facts {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
  border-bottom: 1px solid var(--border);
}

.agent-facts > div {
  min-width: 0;
  padding: 11px 12px;
}

.agent-facts dt {
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}

.agent-facts dd {
  margin: 4px 0 0;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.runtime-errors {
  display: grid;
  gap: 7px;
  padding-top: 12px;
}

.agent-capabilities {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding-top: 12px;
}

.runtime-errors p,
.inline-error {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 7px;
  color: var(--danger);
  font-size: 12px;
  line-height: 1.45;
}

.runtime-errors svg,
.inline-error svg {
  flex: 0 0 auto;
  margin-top: 1px;
}

.runtime-errors strong {
  flex: 0 0 auto;
}

.runtime-errors span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.inline-error {
  margin-top: 10px;
}

.line-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 440px), 1fr));
  gap: 12px;
  padding-top: 14px;
}

.line-status {
  min-width: 0;
  padding: 15px 16px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  border-radius: 6px;
}

.line-status header {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
}

.line-status__icon {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 32px;
  place-items: center;
  color: var(--blue);
  background: var(--blue-soft);
  border-radius: 50%;
}

.line-status__identity {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 2px;
}

.line-status__identity strong,
.line-status__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-status__identity strong {
  font-size: 15px;
}

.line-status__identity small,
.line-state {
  color: var(--muted);
  font-size: 12px;
}

.line-state {
  flex: 0 0 auto;
  padding: 4px 8px;
  color: var(--muted);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-weight: 650;
}

.line-state.is-positive {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: #c8e5de;
}

.line-state.is-warning {
  color: #7a5700;
  background: #fff5d8;
  border-color: #ead9a7;
}

.line-state.is-negative {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: #f0d2d6;
}

.line-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 14px 0 0;
  border-top: 1px solid var(--border);
}

.line-facts > div {
  min-width: 0;
  padding: 11px 10px 10px 0;
  border-bottom: 1px solid var(--border);
}

.line-facts > div:nth-child(even) {
  padding-right: 0;
  padding-left: 12px;
  border-left: 1px solid var(--border);
}

.line-facts dt {
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}

.line-facts dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  color: var(--text);
  font-size: 13px;
  line-height: 1.35;
}

.line-facts > div:nth-child(n + 4) dd {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.capability-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}

.capability-status {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  gap: 5px;
  padding: 3px 7px;
  color: var(--danger);
  font-size: 12px;
  font-weight: 650;
  background: var(--danger-soft);
  border: 1px solid #f0d2d6;
  border-radius: 5px;
}

.capability-status.is-available {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: #c8e5de;
}

.active-call-list {
  display: grid;
}

.active-call {
  display: grid;
  min-width: 0;
  min-height: 64px;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}

.active-call__icon {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.active-call__identity {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.active-call__identity strong,
.active-call__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.active-call__identity strong {
  font-size: 13px;
}

.active-call__identity small {
  color: var(--muted);
  font-size: 12px;
}

.active-call__audio {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
}

.active-call__audio.is-unavailable {
  color: var(--danger);
}

.empty-row {
  min-height: 56px;
  padding: 20px 0;
  color: var(--muted);
  font-size: 12px;
}

.log-heading {
  align-items: center;
}

.log-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
}

.diagnostic-action {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--muted);
  background: transparent;
  border-radius: 50%;
}

.diagnostic-action:hover:not(:disabled) {
  color: var(--text);
  background: var(--surface-hover);
}

.follow-control {
  display: inline-flex;
  min-height: 30px;
  align-items: center;
  gap: 5px;
  padding: 0 7px;
  color: var(--muted);
  font-size: 12px;
  cursor: pointer;
}

.follow-control input {
  width: 14px;
  height: 14px;
  accent-color: var(--accent);
}

.log-filters {
  display: grid;
  grid-template-columns: minmax(104px, 0.5fr) minmax(0, 0.8fr) minmax(0, 1.2fr);
  gap: 8px;
  padding: 11px 0;
}

.log-filters label {
  min-width: 0;
}

.log-filters select,
.filter-input {
  width: 100%;
  min-width: 0;
  height: 34px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
}

.log-filters select {
  padding: 0 9px;
  font-size: 12px;
}

.filter-input {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  color: var(--muted);
}

.filter-input input {
  min-width: 0;
  flex: 1;
  padding: 0;
  font-size: 12px;
  background: transparent;
  border: 0;
}

.log-notice {
  padding-bottom: 8px;
  color: #946200;
  font-size: 12px;
}

.log-viewport {
  height: clamp(280px, 42vh, 520px);
  min-width: 0;
  overflow: auto;
  background: #151a21;
  border: 1px solid #252d38;
  border-radius: 6px;
  scrollbar-color: #4d5968 #151a21;
}

.log-empty {
  display: flex;
  min-height: 160px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: #8d99a8;
  font-size: 12px;
}

.log-entry {
  display: grid;
  min-width: 0;
  grid-template-columns: 72px 48px minmax(90px, 140px) minmax(180px, 1fr);
  gap: 7px;
  padding: 7px 10px;
  color: #d8dee7;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.45;
  border-bottom: 1px solid #252d38;
}

.log-entry:last-child {
  border-bottom: 0;
}

.log-entry time,
.log-entry small {
  color: #8491a1;
}

.log-level {
  color: #87b9ff;
  font-weight: 700;
  text-transform: uppercase;
}

.log-entry.is-warn .log-level {
  color: #e8bd66;
}

.log-entry.is-error .log-level {
  color: #ff8896;
}

.log-entry.is-debug .log-level {
  color: #8d99a8;
}

.log-component {
  overflow: hidden;
  color: #84d4c2;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-message,
.log-entry code,
.log-entry small {
  min-width: 0;
  overflow-wrap: anywhere;
}

.log-entry code,
.log-entry small {
  grid-column: 4;
}

.log-entry code {
  color: #9eabb9;
  white-space: pre-wrap;
}

@media (max-width: 900px) {
  .service-grid {
    grid-template-columns: 1fr;
  }

  .service-status {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    border-top: 0;
  }

  .service-status:last-child {
    border-bottom: 0;
  }

  .agent-facts {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .log-filters {
    grid-template-columns: 120px minmax(130px, 0.8fr) minmax(180px, 1.2fr);
  }
}

@media (max-width: 640px) {
  .section-heading,
  .log-heading {
    align-items: flex-start;
  }

  .log-heading {
    flex-direction: column;
  }

  .log-actions {
    width: 100%;
    justify-content: flex-end;
  }

  .follow-control {
    margin-right: auto;
  }

  .agent-facts,
  .log-filters {
    grid-template-columns: 1fr;
  }

  .line-facts {
    grid-template-columns: 1fr;
  }

  .line-facts > div:nth-child(even) {
    padding-left: 0;
    border-left: 0;
  }

  .active-call {
    grid-template-columns: 34px minmax(0, 1fr);
    padding: 10px 0;
  }

  .active-call__audio {
    grid-column: 2;
  }

  .log-entry {
    grid-template-columns: 66px 44px minmax(0, 1fr);
  }

  .log-message,
  .log-entry code,
  .log-entry small {
    grid-column: 1 / -1;
  }
}
</style>
