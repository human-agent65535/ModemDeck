<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { PhoneCall, PhoneOff, X } from '@lucide/vue'
import {
  answerCall,
  callState,
  dismissCall,
  hangupCall,
  rejectCall
} from '../state/call'
import { contactForNumber, lineName } from '../state/workspace'
import { formatDuration } from '../utils/format'

const now = ref(Date.now())
let timer: number | undefined

const session = computed(() => callState.session)
const contactName = computed(
  () =>
    session.value?.display_name ||
    (session.value ? contactForNumber(session.value.remote_number)?.display_name : '') ||
    session.value?.remote_number ||
    ''
)
const phaseLabel = computed(() => {
  const labels = {
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

onMounted(() => {
  timer = window.setInterval(() => {
    now.value = Date.now()
  }, 1000)
})
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer)
})
</script>

<template>
  <Transition name="call-surface">
    <section v-if="session" class="call-surface" aria-live="polite">
      <div class="call-surface__identity">
        <strong>{{ contactName }}</strong>
        <span v-if="session.display_name">{{ session.remote_number }}</span>
        <small>{{ lineName(session.line_key) }}</small>
      </div>
      <div class="call-surface__status">
        <span>{{ phaseLabel }}</span>
        <strong v-if="duration">{{ duration }}</strong>
        <small v-if="callState.error">{{ callState.error }}</small>
        <small v-else-if="session.failure_reason">{{ session.failure_reason }}</small>
      </div>
      <div class="call-surface__actions">
        <template v-if="incoming">
          <button class="call-button call-button--answer" type="button" title="接听" :disabled="callState.busy" @click="answerCall">
            <PhoneCall :size="20" />
          </button>
          <button class="call-button call-button--hangup" type="button" title="拒接" :disabled="callState.busy" @click="rejectCall">
            <PhoneOff :size="20" />
          </button>
        </template>
        <button
          v-else-if="!terminal"
          class="call-button call-button--hangup"
          type="button"
          title="挂断"
          :disabled="callState.busy"
          @click="hangupCall"
        >
          <PhoneOff :size="20" />
        </button>
        <button v-else class="icon-button" type="button" title="关闭" @click="dismissCall">
          <X :size="19" />
        </button>
      </div>
    </section>
  </Transition>
</template>
