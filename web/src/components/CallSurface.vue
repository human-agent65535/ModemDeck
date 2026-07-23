<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Circle,
  Grid3X3,
  LoaderCircle,
  Mic,
  MicOff,
  PhoneCall,
  PhoneOff,
  RefreshCw,
  Square,
  Volume2,
  X
} from '@lucide/vue'
import {
  answerCall,
  callState,
  dismissCall,
  hangupCall,
  rejectCall,
  sendDTMF
} from '../state/call'
import {
  callMediaState,
  resumeCallAudio,
  retryCallMedia,
  toggleCallMute
} from '../state/callMedia'
import {
  callRecordingState,
  setActiveCallRecording,
  syncCallRecording
} from '../state/recording'
import { contactForNumber, lineForKey, lineName, lineSupports } from '../state/workspace'
import { formatDuration } from '../utils/format'

const now = ref(Date.now())
const dtmfOpen = ref(false)
let timer: number | undefined
const dtmfKeys = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '0', '#']

const session = computed(() => callState.session)
const line = computed(() => (session.value ? lineForKey(session.value.line_key) : undefined))
const contactName = computed(
  () =>
    session.value?.display_name ||
    (session.value ? contactForNumber(session.value.remote_number)?.display_name : '') ||
    session.value?.remote_number ||
    '未知号码'
)
const phaseLabel = computed(() => {
  const labels = {
    unknown: '状态未知',
    dialing: '正在拨号',
    ringing: session.value?.direction === 'incoming' ? '来电' : '等待接听',
    connecting: '正在连接',
    active: '通话中',
    ending: '正在挂断',
    ended: '通话已结束',
    failed: '通话失败'
  }
  return session.value ? labels[session.value.phase] : ''
})
const duration = computed(() => {
  const start = session.value?.active_at
  if (!start) return ''
  const end = session.value?.ended_at ? Date.parse(session.value.ended_at) : now.value
  return formatDuration(Math.max(0, Math.floor((end - Date.parse(start)) / 1000)))
})
const incoming = computed(
  () => session.value?.direction === 'incoming' && session.value.phase === 'ringing'
)
const terminal = computed(
  () => session.value?.phase === 'ended' || session.value?.phase === 'failed'
)
const active = computed(() => session.value?.phase === 'active')
const canHangup = computed(() => {
  const phase = session.value?.phase
  return (
    phase === 'unknown' ||
    phase === 'dialing' ||
    phase === 'ringing' ||
    phase === 'connecting' ||
    phase === 'active'
  )
})
const bearerLabel = computed(() => {
  const bearer = session.value?.bearer?.trim().toLocaleLowerCase()
  if (!bearer) return ''
  if (bearer === 'unknown') return '语音承载未知'
  if (bearer === 'volte') return 'VoLTE'
  if (bearer === 'vowifi') return 'VoWiFi'
  return '其他语音承载'
})
const answerUnavailable = computed(() =>
  lineSupports(line.value, 'answer') === false ? '线路不支持接听' : ''
)
const rejectUnavailable = computed(() =>
  lineSupports(line.value, 'reject') === false ? '线路不支持拒接' : ''
)
const hangupUnavailable = computed(() =>
  lineSupports(line.value, 'hangup') === false ? '线路不支持挂断' : ''
)
const dtmfUnavailable = computed(() =>
  lineSupports(line.value, 'dtmf') === false ? '线路不支持 DTMF' : ''
)
const mediaLabel = computed(() => {
  if (!active.value) return ''
  if (callMediaState.status === 'unavailable') return '服务器音频未接入'
  if (callMediaState.status === 'requesting') return '正在请求麦克风'
  if (callMediaState.status === 'connecting') return '正在连接浏览器音频'
  if (callMediaState.status === 'active') return '浏览器音频已连接'
  return ''
})
const mediaControllable = computed(
  () => callMediaState.status === 'connecting' || callMediaState.status === 'active'
)
const recordingLabel = computed(() => {
  if (!active.value) return ''
  if (callRecordingState.status === 'initializing') return '正在应用录音设置'
  if (callRecordingState.active) return '录音中'
  if (callRecordingState.enabled) return '录音已开启'
  return ''
})

watch(
  () => [session.value?.id, session.value?.phase] as const,
  ([, phase]) => {
    if (phase !== 'active') dtmfOpen.value = false
  }
)

watch(
  session,
  value => {
    syncCallRecording(value)
  },
  { immediate: true }
)

function tone(digit: string): void {
  void sendDTMF(digit)
}

function toggleRecording(): void {
  void setActiveCallRecording(!callRecordingState.enabled)
}

onMounted(() => {
  timer = window.setInterval(() => {
    now.value = Date.now()
  }, 1000)
})
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer)
  syncCallRecording(null)
})
</script>

<template>
  <Transition name="call-surface">
    <section v-if="session" class="call-surface">
      <div class="call-surface__identity">
        <strong>{{ contactName }}</strong>
        <span v-if="session.display_name">{{ session.remote_number }}</span>
        <small>{{ lineName(session.line_key) }}</small>
        <small v-if="bearerLabel" class="call-surface__bearer">{{ bearerLabel }}</small>
      </div>
      <div class="call-surface__status">
        <span role="status" aria-live="polite" aria-atomic="true">{{ phaseLabel }}</span>
        <strong v-if="duration" aria-live="off">{{ duration }}</strong>
        <small v-if="callState.error" role="alert">{{ callState.error }}</small>
        <small v-else-if="callState.syncError" role="alert">{{ callState.syncError }}</small>
        <small v-else-if="callRecordingState.error" class="call-surface__error" role="alert">
          {{ callRecordingState.error }}
        </small>
        <small v-else-if="callMediaState.error" role="alert">{{ callMediaState.error }}</small>
        <small v-else-if="session.failure_reason" role="alert">{{ session.failure_reason }}</small>
        <small v-if="mediaLabel" class="call-surface__media">{{ mediaLabel }}</small>
        <small
          v-if="recordingLabel"
          class="call-surface__recording"
          :class="{ 'is-active': callRecordingState.active }"
        >
          <i v-if="callRecordingState.active" aria-hidden="true" />
          {{ recordingLabel }}
        </small>
      </div>
      <div class="call-surface__actions">
        <template v-if="incoming">
          <button
            class="call-button call-button--answer"
            type="button"
            :title="answerUnavailable || '接听'"
            aria-label="接听"
            :disabled="callState.busy || Boolean(answerUnavailable)"
            @click="answerCall"
          >
            <LoaderCircle v-if="callState.pendingAction === 'answer'" class="spin" :size="20" />
            <PhoneCall v-else :size="20" />
          </button>
          <button
            class="call-button call-button--hangup"
            type="button"
            :title="rejectUnavailable || '拒接'"
            aria-label="拒接"
            :disabled="callState.busy || Boolean(rejectUnavailable)"
            @click="rejectCall"
          >
            <LoaderCircle v-if="callState.pendingAction === 'reject'" class="spin" :size="20" />
            <PhoneOff v-else :size="20" />
          </button>
        </template>
        <button
          v-if="active"
          class="icon-button"
          :class="{ 'is-active': dtmfOpen }"
          type="button"
          :title="dtmfUnavailable || '按键盘'"
          aria-label="通话按键盘"
          :aria-expanded="dtmfOpen"
          :disabled="Boolean(dtmfUnavailable)"
          @click="dtmfOpen = !dtmfOpen"
        >
          <Grid3X3 :size="19" />
        </button>
        <button
          v-if="active && mediaControllable"
          class="icon-button"
          :class="{ 'is-active': callMediaState.muted }"
          type="button"
          :title="callMediaState.muted ? '取消静音' : '静音'"
          :aria-label="callMediaState.muted ? '取消麦克风静音' : '麦克风静音'"
          :aria-pressed="callMediaState.muted"
          @click="toggleCallMute"
        >
          <MicOff v-if="callMediaState.muted" :size="19" />
          <Mic v-else :size="19" />
        </button>
        <button
          v-if="active"
          class="icon-button call-recording-button"
          :class="{ 'is-recording': callRecordingState.active }"
          type="button"
          :title="callRecordingState.enabled ? '停止录音' : '开始录音'"
          :aria-label="callRecordingState.enabled ? '停止通话录音' : '开始通话录音'"
          :aria-pressed="callRecordingState.enabled"
          :disabled="
            callRecordingState.busy ||
            callRecordingState.status === 'initializing'
          "
          @click="toggleRecording"
        >
          <LoaderCircle
            v-if="
              callRecordingState.busy ||
              callRecordingState.status === 'initializing'
            "
            class="spin"
            :size="18"
          />
          <Square
            v-else-if="callRecordingState.enabled"
            :size="17"
            fill="currentColor"
          />
          <Circle v-else :size="18" fill="currentColor" />
        </button>
        <button
          v-if="active && callMediaState.playbackBlocked"
          class="icon-button"
          type="button"
          title="启用扬声器"
          aria-label="启用通话声音"
          @click="resumeCallAudio"
        >
          <Volume2 :size="19" />
        </button>
        <button
          v-if="active && callMediaState.status === 'error'"
          class="icon-button"
          type="button"
          title="重试浏览器音频"
          aria-label="重试浏览器音频"
          @click="retryCallMedia(session)"
        >
          <RefreshCw :size="19" />
        </button>
        <button
          v-if="!incoming && canHangup"
          class="call-button call-button--hangup"
          type="button"
          :title="hangupUnavailable || '挂断'"
          aria-label="挂断"
          :disabled="callState.busy || Boolean(hangupUnavailable)"
          @click="hangupCall"
        >
          <LoaderCircle v-if="callState.pendingAction === 'hangup'" class="spin" :size="20" />
          <PhoneOff v-else :size="20" />
        </button>
        <button
          v-else-if="terminal"
          class="icon-button"
          type="button"
          title="关闭"
          aria-label="关闭通话状态"
          @click="dismissCall"
        >
          <X :size="19" />
        </button>
      </div>
      <div v-if="active && dtmfOpen" class="call-surface__dtmf" aria-label="通话按键盘">
        <button
          v-for="digit in dtmfKeys"
          :key="digit"
          type="button"
          :aria-label="`发送 ${digit}`"
          :disabled="callState.busy"
          @click="tone(digit)"
        >
          {{ digit }}
        </button>
      </div>
    </section>
  </Transition>
</template>

<style scoped>
.call-surface__recording {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
}

.call-surface__recording.is-active {
  color: var(--danger);
  font-weight: 650;
}

.call-surface__recording i {
  width: 7px;
  height: 7px;
  flex: 0 0 7px;
  background: var(--danger);
  border-radius: 50%;
  box-shadow: 0 0 0 3px var(--danger-soft);
}

.call-surface__error {
  color: var(--danger);
}

.call-recording-button {
  color: var(--danger);
}

.call-recording-button.is-recording {
  color: #ffffff;
  background: var(--danger);
}

.call-recording-button.is-recording:hover:not(:disabled) {
  background: #b72f40;
}

</style>
