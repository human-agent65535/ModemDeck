<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  Circle,
  CircleDot,
  Grip,
  Headphones,
  LoaderCircle,
  MicOff,
  PhoneCall,
  PhoneOff,
  X
} from '@lucide/vue'
import {
  answerCall as answerRealCall,
  callState,
  dismissCall as dismissRealCall,
  hangupCall as hangupRealCall,
  isLiveCallSession,
  rejectCall as rejectRealCall,
  retryActiveCallMedia,
  sendDTMF,
  showActiveCallForLine
} from '../state/call'
import {
  callMediaState,
  resumeCallAudio,
  toggleCallMute
} from '../state/callMedia'
import { callRecordingState, setCallRecordingEnabled } from '../state/recording'
import type { CallSession } from '../api/types'
import { playDTMFTone } from '../state/dtmfAudio'
import {
  bootstrapResource,
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineSupports
} from '../state/workspace'
import { knownCallBearerLabel } from '../callBearer'
import { formatCallClock } from '../utils/format'
import { lineTone } from '../utils/lineTone'
import { phoneKeypad } from '../utils/phoneKeypad'
import AudioDeviceControls from './AudioDeviceControls.vue'
import CommunicationAvatar from './CommunicationAvatar.vue'
import CallTestFeedback from './CallTestFeedback.vue'
import { answerCallTest, callTestSession, callTestState, callTestStatusLabel, dismissCallTest, stopCallTest } from '../state/callTestRuntime'

const props = defineProps<{ session: CallSession }>()
const { t } = useI18n()
const now = ref(Date.now())
const dtmfOpen = ref(false)
const devicesOpen = ref(false)
const dtmfDisplay = ref<HTMLOutputElement>()
const answerButton = ref<HTMLButtonElement>()
const incomingAnnouncement = ref('')
let timer: number | undefined

const session = computed(() => props.session)
const isTestCall = computed(() => callTestSession.value?.id === session.value.id)
const owned = computed(() => isTestCall.value ? session.value?.control_state === 'owned' : callState.owned)
const busy = computed(() => !isTestCall.value && callState.busy)
const answerCall = () => isTestCall.value ? answerCallTest() : answerRealCall()
const rejectCall = () => isTestCall.value ? stopCallTest() : rejectRealCall()
const hangupCall = () => isTestCall.value ? stopCallTest() : hangupRealCall()
const dismissCall = () => isTestCall.value ? dismissCallTest() : dismissRealCall()
const dtmfDigits = computed(() => callState.dtmfDigits)
const line = computed(() => (session.value ? lineForKey(session.value.line_id) : undefined))
const contact = computed(() =>
  session.value ? contactForNumber(session.value.remote_number) : undefined
)
const presentedNumber = computed(() =>
  session.value
    ? displayPhoneNumber(session.value.remote_number, session.value.line_id)
    : ''
)
const contactName = computed(
  () =>
    session.value?.display_name ||
    contact.value?.display_name ||
    presentedNumber.value ||
    t('calls.unknownNumber')
)
const contactNumber = computed(() => {
  const number = presentedNumber.value
  return number && number !== contactName.value ? number : ''
})
const lineLabel = computed(() =>
  line.value ? line.value.line_label.trim() || session.value.line_id || t('lines.line') : ''
)
const lineDot = computed(() => (line.value ? lineTone(line.value).dot : ''))
const phaseLabel = computed(() => {
  if (isTestCall.value && session.value.phase === 'active') return callTestStatusLabel.value
  if (!isTestCall.value) {
    if (callState.pendingAction === 'dial') return t('runtime.dialPreparing')
    if (callState.pendingAction === 'hangup' || callState.pendingAction === 'reject') return t('calls.ending')
    if (callState.endReason) return t(`calls.${callState.endReason}`)
  }
  const labels = {
    unknown: t('calls.phaseUnknown'),
    dialing: t('calls.dialing'),
    'ringing-out': t('calls.waitingAnswer'),
    ringing:
      session.value?.direction === 'incoming'
        ? session.value.control_state === 'owned'
          ? t('calls.connectingCall')
          : t('calls.incoming')
        : t('calls.waitingAnswer'),
    connecting: t('calls.connectingCall'),
    active: t('calls.inCall'),
    ending: t('calls.ending'),
    ended: t('calls.ended'),
    failed: t('calls.failed')
  }
  return session.value ? labels[session.value.phase] : ''
})
function callClock(call: CallSession | undefined): string {
  const start = call?.active_at
  if (!start) return ''
  const end = call?.ended_at ? Date.parse(call.ended_at) : now.value
  return formatCallClock(Math.max(0, Math.floor((end - Date.parse(start)) / 1000)))
}
const duration = computed(() => callClock(session.value))
const incoming = computed(
  () =>
    session.value?.direction === 'incoming' &&
    (session.value.phase === 'ringing' || callState.pendingAction === 'reject') &&
    session.value.control_state === 'available'
)
const terminal = computed(
  () => session.value?.phase === 'ended' || session.value?.phase === 'failed'
)
const occupied = computed(
  () => Boolean(session.value?.control_state === 'occupied' && !terminal.value)
)
const occupiedLabel = computed(() =>
  session.value?.direction === 'incoming'
    ? t('calls.answeredElsewhere')
    : t('calls.lineInUse')
)
const occupiedDescription = computed(() =>
  session.value?.direction === 'incoming'
    ? t('calls.answeredElsewhereDescription')
    : t('calls.lineInUseDescription')
)
const active = computed(() => session.value?.phase === 'active')
const recordingAdjustable = computed(() => !isTestCall.value && active.value && owned.value && !occupied.value)
const recordingControlReady = computed(() =>
  callRecordingState.callID === session.value.id &&
  callRecordingState.status === 'ready' &&
  callRecordingState.segmentsStatus !== 'loading'
)
const recordingLabel = computed(() => {
  if (isTestCall.value || terminal.value || occupied.value || !owned.value) return ''
  if (callState.pendingAction === 'dial') {
    const enabled = callState.outgoingRecordingEnabled
    return enabled === undefined ? '' : t(enabled ? 'dialer.recordingEnabled' : 'dialer.recordingDisabled')
  }
  if (callRecordingState.callID !== session.value.id) return ''
  if (!active.value) {
    return callRecordingState.status === 'ready'
      ? t(callRecordingState.enabled ? 'dialer.recordingEnabled' : 'dialer.recordingDisabled')
      : ''
  }
  if (callRecordingState.busy || callRecordingState.status === 'initializing') return t('calls.applyingRecording')
  if (callRecordingState.recordingStatus === 'failed') return t('calls.recordingFailed')
  if (callRecordingState.recordingStatus === 'recording') return t('calls.recordingActive')
  if (callRecordingState.enabled) return t('calls.recordingPending')
  return ''
})
const canHangup = computed(() => {
  const phase = session.value?.phase
  return (
    callState.pendingAction === 'hangup' ||
    phase === 'unknown' ||
    phase === 'dialing' ||
    phase === 'ringing-out' ||
    phase === 'ringing' ||
    phase === 'connecting' ||
    phase === 'active'
  )
})
const bearerLabel = computed(() => {
  const bearer = session.value?.bearer?.trim().toLocaleLowerCase()
  if (!bearer) return ''
  const knownLabel = knownCallBearerLabel(bearer)
  if (knownLabel) return knownLabel
  if (bearer === 'unknown') return t('calls.bearerUnknown')
  return t('calls.otherBearer')
})
const answerUnavailable = computed(() => {
  if (isTestCall.value) return ''
  if (lineSupports(line.value, 'answer') === false) return t('calls.answerUnsupported')
  if (
    bootstrapResource.data?.capabilities.wss_audio !== true ||
    lineSupports(line.value, 'media') !== true
  ) {
    return t('calls.answerAudioUnavailable')
  }
  return ''
})
const rejectUnavailable = computed(() =>
  !isTestCall.value && lineSupports(line.value, 'reject') === false ? t('calls.rejectUnsupported') : ''
)
const hangupUnavailable = computed(() =>
  !isTestCall.value && callState.pendingAction !== 'dial' && lineSupports(line.value, 'hangup') === false ? t('calls.hangupUnsupported') : ''
)
const dtmfUnavailable = computed(() =>
  lineSupports(line.value, 'dtmf') === false ? t('calls.dtmfUnsupported') : ''
)
const mediaLabel = computed(() => {
  if (isTestCall.value || terminal.value || !owned.value) return ''
  if (callMediaState.status === 'requesting') return t('calls.requestingMicrophone')
  if (callMediaState.status === 'connecting') return t('calls.connectingBrowserAudio')
  if (callMediaState.status === 'recovering') return t('calls.reconnectingBrowserAudio')
  if (callMediaState.status === 'active') return t('calls.browserAudioConnected')
  return ''
})
const mediaControllable = computed(
  () =>
    callMediaState.status === 'connecting' ||
    callMediaState.status === 'recovering' ||
    callMediaState.status === 'active'
)
const keypadAvailable = computed(() => active.value && !isTestCall.value)
// One fixed set of controls from dialing to connected: unavailable ones are disabled, not
// removed, so nothing moves under the pointer as the call progresses.
const showMediaControls = computed(
  () =>
    !terminal.value &&
    owned.value &&
    !incoming.value &&
    !occupied.value &&
    !dtmfOpen.value &&
    !devicesOpen.value
)
// Audio problems decide whether the call can be heard, so they are a banner with the fix.
const audioNotice = computed<{ message: string; action?: 'resume' | 'retry' } | null>(() => {
  if (isTestCall.value || terminal.value || !owned.value) return null
  if (callMediaState.playbackBlocked) return { message: t('calls.audioBlockedNotice'), action: 'resume' }
  if (callMediaState.status === 'error') return { message: t('calls.audioFailedNotice'), action: 'retry' }
  if (callMediaState.status === 'unavailable') return { message: t('calls.serverAudioUnavailable') }
  return null
})
const statusTone = computed(() => {
  if (terminal.value || occupied.value) return 'is-idle'
  if (incoming.value) return 'is-incoming'
  return active.value ? 'is-live' : 'is-pending'
})
// Concurrent calls are listed instead of reusing the dialer line picker.
const concurrentCalls = computed(() =>
  isTestCall.value ? [] : callState.sessions.filter(isLiveCallSession)
)
function callPeer(call: CallSession): string {
  return (
    call.display_name ||
    contactForNumber(call.remote_number)?.display_name ||
    displayPhoneNumber(call.remote_number, call.line_id) ||
    t('calls.unknownNumber')
  )
}
function callLineLabel(call: CallSession): string {
  const callLine = lineForKey(call.line_id)
  return callLine?.line_label.trim() || call.line_id
}
function callLineDot(call: CallSession): string {
  const callLine = lineForKey(call.line_id)
  return callLine ? lineTone(callLine).dot : 'var(--muted)'
}
function callPhaseLabel(call: CallSession): string {
  if (call.phase === 'active') return t('calls.inCall')
  if (call.phase === 'ringing') return call.direction === 'incoming' ? t('calls.incoming') : t('calls.waitingAnswer')
  if (call.phase === 'ringing-out') return t('calls.waitingAnswer')
  if (call.phase === 'dialing') return t('calls.dialing')
  return t('calls.connectingCall')
}
function closeCallPanels(): void {
  dtmfOpen.value = false
  devicesOpen.value = false
}
const statusError = computed(
  () => isTestCall.value ? callTestState.error :
    callState.error ||
    callState.syncError ||
    (callRecordingState.callID === session.value.id ? callRecordingState.error : '') ||
    callMediaState.error ||
    session.value?.failure_reason ||
    ''
)
const capabilityNotice = computed(() => {
  if (occupied.value) return ''
  const reasons = incoming.value
    ? [answerUnavailable.value, rejectUnavailable.value]
    : canHangup.value
      ? [hangupUnavailable.value]
      : []
  return reasons
    .filter((reason, index) => reason && reasons.indexOf(reason) === index)
    .join(t('common.listSeparator'))
})

watch(
  () => [session.value?.id, session.value?.phase] as const,
  ([, phase]) => {
    if (phase !== 'active') dtmfOpen.value = false
    if (phase === 'ended' || phase === 'failed') devicesOpen.value = false
  }
)

// Desktop keeps the incoming panel non-modal, so announce it assertively and
// offer a shortcut to Answer instead of moving focus while someone types.
watch(
  incoming,
  value => {
    incomingAnnouncement.value = ''
    if (!value) return
    void nextTick(() => {
      incomingAnnouncement.value = [
        t('calls.incomingFrom', { name: contactName.value, line: lineLabel.value || session.value.line_id }),
        t('calls.answerShortcutHint')
      ].join(' ')
    })
  },
  { immediate: true }
)

function focusAnswerShortcut(event: KeyboardEvent): void {
  if (!incoming.value || !event.altKey || !event.shiftKey || event.code !== 'KeyA') return
  event.preventDefault()
  answerButton.value?.focus()
}

function tone(digit: string): void {
  void playDTMFTone(digit)
  void nextTick(() => {
    if (dtmfDisplay.value) dtmfDisplay.value.scrollLeft = dtmfDisplay.value.scrollWidth
  })
  void sendDTMF(digit)
}

function toggleRecording(): void {
  if (!recordingAdjustable.value || busy.value || callRecordingState.busy || !recordingControlReady.value) return
  void setCallRecordingEnabled(!callRecordingState.enabled)
}

onMounted(() => {
  timer = window.setInterval(() => {
    now.value = Date.now()
  }, 1000)
  window.addEventListener('keydown', focusAnswerShortcut)
})
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer)
  window.removeEventListener('keydown', focusAnswerShortcut)
})
</script>


<template>
    <section class="call-surface" :class="{ 'is-incoming': incoming }">
      <p class="sr-only" aria-live="assertive" aria-atomic="true">{{ incomingAnnouncement }}</p>
      <div
        class="call-surface__content"
        :class="{ 'is-dtmf-open': dtmfOpen, 'is-devices-open': devicesOpen }"
      >
        <ul
          v-if="concurrentCalls.length > 1"
          class="call-surface__calls"
          :aria-label="t('calls.concurrentCalls')"
        >
          <li v-for="call in concurrentCalls" :key="call.id">
            <button
              class="call-card"
              :class="{ 'is-current': call.id === session.id }"
              type="button"
              :aria-current="call.id === session.id ? 'true' : undefined"
              @click="showActiveCallForLine(call.line_id)"
            >
              <span class="call-card__copy">
                <strong>{{ callPeer(call) }}</strong>
                <small>
                  <span class="call-line-dot" :style="{ backgroundColor: callLineDot(call) }" aria-hidden="true" />
                  {{ callLineLabel(call) }} · {{ callPhaseLabel(call) }}
                </small>
              </span>
              <span class="call-card__meta">
                <strong v-if="callClock(call)">{{ callClock(call) }}</strong>
                <small :class="{ 'is-owned': call.control_state === 'owned' }">
                  {{ call.control_state === 'owned' ? t('calls.controlledHere') : t('calls.controlledElsewhere') }}
                </small>
              </span>
            </button>
          </li>
        </ul>

        <div class="call-surface__identity">
          <span class="call-surface__avatar" :class="{ 'is-ringing': incoming }">
            <span v-if="incoming" class="call-surface__ring call-surface__ring--inner" aria-hidden="true" />
            <span v-if="incoming" class="call-surface__ring call-surface__ring--outer" aria-hidden="true" />
            <CommunicationAvatar
              channel="call"
              :name="contactName"
              :address="presentedNumber"
              :src="contact?.avatar"
              :contact-bound="Boolean(contact)"
              size="large"
            />
          </span>
          <strong class="call-surface__name">{{ contactName }}</strong>
          <div class="call-surface__meta">
            <span v-if="contactNumber" class="call-surface__number">{{ contactNumber }}</span>
            <span v-if="contactNumber && (line || isTestCall)" aria-hidden="true">·</span>
            <span v-if="line" class="call-surface__line">
              <span class="call-line-dot" :style="{ backgroundColor: lineDot }" aria-hidden="true" />
              {{ lineLabel }}
            </span>
            <span v-else-if="isTestCall">{{ t('callTest.testOnly') }}</span>
            <span v-else-if="session.line_id" class="call-surface__line">{{ session.line_id }}</span>
            <small v-if="bearerLabel" class="call-surface__bearer">{{ bearerLabel }}</small>
          </div>
        </div>

        <div class="call-surface__status">
          <span class="call-surface__phase" role="status" aria-live="polite" aria-atomic="true">
            <span class="call-surface__status-dot" :class="statusTone" aria-hidden="true" />
            {{ occupied ? occupiedLabel : phaseLabel }}
          </span>
          <strong v-if="duration" class="call-surface__timer" aria-live="off">{{ duration }}</strong>
        </div>

        <div v-if="audioNotice" class="call-surface__audio-notice" role="alert">
          <AlertTriangle :size="18" aria-hidden="true" />
          <span>{{ audioNotice.message }}</span>
          <button
            v-if="audioNotice.action === 'resume'"
            class="secondary-button"
            type="button"
            @click="resumeCallAudio"
          >
            {{ t('calls.enableCallAudio') }}
          </button>
          <button
            v-else-if="audioNotice.action === 'retry'"
            class="secondary-button"
            type="button"
            @click="retryActiveCallMedia(session)"
          >
            {{ t('calls.retryAudio') }}
          </button>
        </div>

        <div
          v-if="
            occupied ||
            statusError ||
            capabilityNotice ||
            mediaLabel ||
            recordingLabel
          "
          class="call-surface__notices"
        >
          <p v-if="occupied" class="call-surface__media">
            {{ occupiedDescription }}
          </p>
          <p v-if="statusError" class="call-surface__error" role="alert">
            {{ statusError }}
          </p>
          <p
            v-if="capabilityNotice"
            id="call-capability-notice"
            class="call-surface__error"
          >
            {{ capabilityNotice }}
          </p>
          <p v-if="mediaLabel" class="call-surface__media">{{ mediaLabel }}</p>
          <p v-if="recordingLabel" class="call-surface__recording" role="status">{{ recordingLabel }}</p>
        </div>

        <CallTestFeedback v-if="isTestCall && active" />

        <div
          v-if="showMediaControls"
          class="call-surface__controls"
          role="group"
          :aria-label="t('calls.controls')"
        >
          <button
            class="call-control"
            :class="{ 'is-active': callMediaState.muted }"
            type="button"
            :title="callMediaState.muted ? t('calls.unmute') : t('calls.mute')"
            :aria-label="t('calls.muteMicrophone')"
            :aria-pressed="callMediaState.muted"
            :disabled="!mediaControllable"
            @click="toggleCallMute"
          >
            <span class="call-control__icon"><MicOff :size="22" /></span>
            <span class="call-control__label">{{ t('calls.mute') }}</span>
          </button>
          <button
            v-if="!isTestCall"
            class="call-control"
            type="button"
            :title="dtmfUnavailable || t('calls.keypad')"
            :aria-label="t('calls.keypad')"
            :aria-expanded="dtmfOpen"
            :disabled="!keypadAvailable || callState.busy || Boolean(dtmfUnavailable)"
            @click="devicesOpen = false; dtmfOpen = true"
          >
            <span class="call-control__icon"><Grip :size="22" /></span>
            <span class="call-control__label">{{ t('calls.keypadShort') }}</span>
          </button>
          <button
            class="call-control"
            type="button"
            :title="t('calls.audioDevices')"
            :aria-label="t('calls.audioDevices')"
            :aria-expanded="devicesOpen"
            :disabled="!mediaControllable"
            @click="dtmfOpen = false; devicesOpen = true"
          >
            <span class="call-control__icon"><Headphones :size="22" /></span>
            <span class="call-control__label">{{ t('calls.audioDevices') }}</span>
          </button>
          <button
            v-if="!isTestCall"
            class="call-control call-control--recording"
            :class="{ 'is-active': callRecordingState.enabled }"
            type="button"
            :title="
              callRecordingState.enabled
                ? t('calls.stopRecording')
                : t('calls.startRecording')
            "
            :aria-label="t('calls.record')"
            :aria-pressed="callRecordingState.enabled"
            :disabled="
              !recordingAdjustable ||
              busy ||
              callRecordingState.busy ||
              !recordingControlReady
            "
            @click="toggleRecording"
          >
            <span class="call-control__icon" aria-hidden="true">
              <CircleDot v-if="callRecordingState.enabled" :size="22" />
              <Circle v-else :size="20" fill="currentColor" />
            </span>
            <span class="call-control__label">{{ t('calls.record') }}</span>
          </button>
        </div>

        <div
          v-if="devicesOpen && owned && !terminal"
          class="call-surface__devices"
          role="group"
          :aria-label="t('calls.audioDevices')"
        >
          <AudioDeviceControls compact />
        </div>

        <Transition name="dtmf">
          <div
            v-if="active && !isTestCall && dtmfOpen"
            class="call-surface__dtmf"
            role="group"
            :aria-label="t('calls.keypad')"
          >
            <output
              ref="dtmfDisplay"
              class="call-surface__dtmf-display"
              :aria-label="t('calls.pressedKeys')"
              :title="dtmfDigits"
            >
              {{ dtmfDigits }}
            </output>
            <button
              v-for="key in phoneKeypad"
              :key="key.digit"
              class="keypad__key"
              :class="{ 'keypad__key--zero': key.digit === '0' }"
              type="button"
              :aria-label="t('calls.sendDigit', { digit: key.digit })"
              :disabled="busy"
              @click="tone(key.digit)"
            >
              <strong>{{ key.digit }}</strong>
              <small v-if="key.letters">{{ key.letters }}</small>
            </button>
          </div>
        </Transition>
      </div>

      <div class="call-surface__primary-actions keypad-action-grid">
        <template v-if="incoming">
          <span class="call-primary-action call-primary-action--start keypad-action-item">
            <button
              class="call-button call-button--hangup"
              type="button"
              :title="rejectUnavailable || t('calls.reject')"
              :aria-label="t('calls.reject')"
              :aria-describedby="rejectUnavailable ? 'call-capability-notice' : undefined"
              :disabled="busy || Boolean(rejectUnavailable)"
              @click="rejectCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'reject'" class="spin" :size="25" />
              <PhoneOff v-else :size="25" />
            </button>
            <small>{{ t('calls.reject') }}</small>
          </span>
          <span class="call-primary-action call-primary-action--end keypad-action-item">
            <button
              ref="answerButton"
              class="call-button call-button--answer"
              type="button"
              :title="answerUnavailable || t('calls.answer')"
              :aria-label="t('calls.answer')"
              aria-keyshortcuts="Alt+Shift+A"
              :aria-describedby="answerUnavailable ? 'call-capability-notice' : undefined"
              :disabled="busy || Boolean(answerUnavailable)"
              @click="answerCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'answer'" class="spin" :size="25" />
              <PhoneCall v-else :size="25" />
            </button>
            <small>{{ t('calls.answer') }}</small>
          </span>
        </template>
        <template v-else-if="canHangup && owned">
          <span class="call-primary-action call-primary-action--center keypad-action-item">
            <button
              class="call-button call-button--hangup"
              type="button"
              :title="hangupUnavailable || t('calls.hangup')"
              :aria-label="t('calls.hangup')"
              :aria-describedby="hangupUnavailable ? 'call-capability-notice' : undefined"
              :disabled="(callState.busy && callState.pendingAction !== 'dial') || Boolean(hangupUnavailable)"
              @click="hangupCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'hangup'" class="spin" :size="25" />
              <PhoneOff v-else :size="25" />
            </button>
            <small>{{ t('calls.hangup') }}</small>
          </span>
          <button
            v-if="dtmfOpen || devicesOpen"
            class="call-footer-action call-footer-action--keypad keypad-action-item"
            type="button"
            :aria-label="dtmfOpen ? t('calls.hideKeypad') : t('calls.hideAudioDevices')"
            @click="closeCallPanels"
          >
            <span class="call-footer-action__text">{{ t('calls.hide') }}</span>
          </button>
        </template>
        <span
          v-else-if="terminal"
          class="call-primary-action call-primary-action--center keypad-action-item"
        >
          <button
            class="call-button call-button--dismiss"
            type="button"
            :title="t('common.close')"
            :aria-label="t('calls.closeStatus')"
            @click="dismissCall"
          >
            <X :size="25" />
          </button>
          <small>{{ t('common.close') }}</small>
        </span>
      </div>
    </section>
</template>

<style scoped>
.call-surface {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
  background: transparent;
}

.call-surface__content {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  align-items: center;
  padding: var(--space-6) var(--space-5) var(--space-4);
  overflow-y: auto;
}

.call-surface__calls {
  display: grid;
  width: 100%;
  gap: var(--space-2);
  margin: 0 0 var(--space-5);
  padding: 0;
  list-style: none;
}

.call-card {
  display: flex;
  width: 100%;
  min-height: 52px;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  color: var(--text);
  text-align: left;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-1);
}

.call-card:hover {
  background: var(--surface-hover);
}

.call-card.is-current {
  border-color: var(--accent);
  box-shadow: inset 3px 0 0 var(--accent), var(--elev-1);
}

.call-card__copy,
.call-card__meta {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.call-card__copy {
  flex: 1;
}

.call-card__meta {
  align-items: flex-end;
}

.call-card strong {
  overflow: hidden;
  font-size: var(--text-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-card__meta strong {
  font-variant-numeric: tabular-nums;
}

.call-card small {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  color: var(--muted);
  font-size: var(--text-2xs);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-card small.is-owned {
  color: var(--accent-strong);
  font-weight: 650;
}

.call-line-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  flex: 0 0 8px;
  border-radius: 50%;
}

.call-surface__identity {
  display: flex;
  width: 100%;
  min-width: 0;
  flex-direction: column;
  align-items: center;
  gap: var(--space-1);
  text-align: center;
}

.call-surface__avatar {
  position: relative;
  display: inline-grid;
  margin-bottom: var(--space-3);
  place-items: center;
}

.call-surface__avatar :deep(.avatar--large) {
  position: relative;
  width: 88px;
  height: 88px;
  font-size: 24px;
}

.call-surface__ring {
  position: absolute;
  inset: -14px;
  border: 2px solid var(--call-answer);
  border-radius: 50%;
  opacity: 0;
  animation: mobile-call-wave 1.6s ease-out infinite;
  pointer-events: none;
}

.call-surface__ring--outer {
  inset: -28px;
  animation-delay: 320ms;
}

.call-surface__name,
.call-surface__meta > span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-surface__name {
  font-size: var(--text-lg);
  font-weight: 650;
  line-height: 1.25;
}

.call-surface__meta {
  display: flex;
  max-width: 100%;
  min-height: 22px;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: var(--space-1) 6px;
  color: var(--muted);
  font-size: var(--text-sm);
}

.call-surface__number {
  font-variant-numeric: tabular-nums;
}

.call-surface__line {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.call-surface__bearer {
  color: var(--accent-strong);
  font-size: var(--text-2xs);
  font-weight: 650;
}

.call-surface__status {
  display: flex;
  min-height: 64px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-1);
  margin-top: var(--space-3);
}

.call-surface__phase {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted-strong);
  font-size: var(--text-sm);
  font-weight: 550;
}

.call-surface__status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--muted);
}

.call-surface__status-dot.is-live {
  background: var(--success);
}

.call-surface__status-dot.is-incoming {
  background: var(--call-answer);
}

.call-surface__status-dot.is-pending {
  background: var(--warning);
}

.call-surface__timer {
  color: var(--text);
  font-size: var(--text-2xl);
  font-variant-numeric: tabular-nums;
  font-weight: 650;
  line-height: 1.15;
}

.call-surface__audio-notice {
  display: flex;
  width: min(100%, 368px);
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
  margin-top: var(--space-3);
  padding: var(--space-3);
  color: var(--warning-strong);
  font-size: var(--text-sm);
  font-weight: 500;
  background: var(--warning-soft);
  border: 1px solid var(--warning-border);
  border-radius: var(--radius-md);
}

.call-surface__audio-notice > span {
  flex: 1 1 160px;
  min-width: 0;
}

.call-surface__notices {
  display: flex;
  min-height: 24px;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 5px 10px;
  margin-top: var(--space-1);
}

.call-surface__notices p {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin: 0;
  color: var(--muted);
  font-size: var(--text-2xs);
  text-align: center;
}

.call-surface__notices .call-surface__error {
  width: 100%;
  justify-content: center;
  color: var(--danger);
}

/* A fixed grid: Mute, Keypad, Audio and Record keep their places for the whole call. */
.call-surface__controls {
  display: grid;
  width: min(100%, 368px);
  grid-template-columns: repeat(auto-fit, minmax(80px, 1fr));
  justify-items: center;
  gap: var(--space-4) var(--space-2);
  margin-top: auto;
  padding-top: var(--space-6);
}

.call-control {
  display: flex;
  width: 80px;
  min-height: 80px;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  color: var(--text);
  font-size: var(--text-xs);
  background: transparent;
}

.call-control:disabled {
  color: var(--muted);
}

.call-control__icon {
  display: inline-grid;
  width: 56px;
  height: 56px;
  place-items: center;
  color: var(--text);
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 50%;
  transition:
    color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard),
    border-color var(--motion-base) var(--ease-standard);
}

.call-control__label {
  max-width: 100%;
  overflow: hidden;
  line-height: 1.25;
  text-align: center;
  text-overflow: ellipsis;
}

.call-control:not(.is-active):hover:not(:disabled) .call-control__icon {
  background: var(--control-muted);
}

.call-control:disabled .call-control__icon {
  color: var(--muted);
  background: transparent;
  border-style: dashed;
}

/* On is inverted, like the system phone app and the native client. */
.call-control.is-active .call-control__icon {
  color: var(--surface);
  background: var(--text);
  border-color: var(--text);
}

.call-control--recording.is-active .call-control__icon {
  color: var(--call-recording-on);
}

.call-surface__devices {
  width: min(100%, 320px);
  margin-top: auto;
  padding: var(--space-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.call-surface__dtmf {
  display: grid;
  width: 238px;
  justify-content: space-between;
  gap: 9px;
  margin-top: 18px;
  grid-template-columns: repeat(3, 62px);
  grid-template-rows: 38px repeat(4, 62px);
}

.call-surface__dtmf-display {
  display: block;
  width: 100%;
  min-height: 38px;
  grid-column: 1 / -1;
  overflow-x: auto;
  color: var(--text);
  font-size: var(--text-xl);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  line-height: 38px;
  text-align: center;
  white-space: nowrap;
  scrollbar-width: none;
}

.call-surface__dtmf-display::-webkit-scrollbar {
  display: none;
}

.call-surface__content.is-dtmf-open,
.call-surface__content.is-devices-open {
  padding-top: 18px;
  padding-bottom: 26px;
}

.call-surface__content.is-dtmf-open .call-surface__avatar,
.call-surface__content.is-devices-open .call-surface__avatar {
  display: none;
}

.call-surface__content.is-dtmf-open .call-surface__name {
  font-size: var(--text-md);
}

.call-surface__content.is-dtmf-open .call-surface__timer {
  font-size: var(--text-xl);
}

.call-surface__content.is-dtmf-open .call-surface__status {
  min-height: 42px;
  margin-top: 5px;
}

.call-surface__content.is-dtmf-open .call-surface__dtmf {
  margin-top: auto;
}

.call-surface__primary-actions {
  min-width: 0;
}

.call-primary-action {
  color: var(--muted);
}

.call-primary-action--start {
  grid-column: 1;
}

.call-primary-action--center {
  grid-column: 2;
}

.call-primary-action--end {
  grid-column: 3;
}

.call-primary-action .call-button {
  width: var(--keypad-action-primary-size);
  height: var(--keypad-action-primary-size);
  flex-basis: var(--keypad-action-primary-size);
}

.call-button--answer:disabled {
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid color-mix(in srgb, var(--accent) 24%, transparent);
  opacity: 1;
}

.call-primary-action small {
  color: var(--muted);
  font-size: var(--text-2xs);
}

.call-footer-action {
  color: var(--text);
  font-size: var(--text-base);
  font-weight: 550;
  background: transparent;
}

.call-footer-action--keypad {
  grid-column: 3;
  grid-template-rows: var(--keypad-action-primary-size);
}

.call-footer-action:not(.is-active):hover:not(:disabled) {
  color: var(--accent-strong);
}

.call-button--dismiss {
  color: var(--text);
  background: var(--surface-hover);
}

.call-button--dismiss:hover:not(:disabled) {
  color: var(--text);
  background: var(--control-muted);
}

.dtmf-enter-active,
.dtmf-leave-active {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-base) var(--ease-standard);
}

.dtmf-enter-from,
.dtmf-leave-to {
  opacity: 0;
  transform: translateY(calc(var(--space-2) * -1));
}

@media (max-width: 640px) {
  .call-surface__content {
    padding-top: max(32px, env(safe-area-inset-top));
  }

  .call-surface__avatar :deep(.avatar--large) {
    width: 96px;
    height: 96px;
    font-size: 26px;
  }

  .call-surface__primary-actions {
    padding-bottom: max(20px, env(safe-area-inset-bottom));
  }
}

@media (max-height: 760px) {
  .call-surface__content {
    padding-top: max(20px, env(safe-area-inset-top));
  }

  .call-surface__avatar :deep(.avatar--large) {
    width: 68px;
    height: 68px;
    font-size: 19px;
  }

  .call-surface__avatar {
    margin-bottom: var(--space-2);
  }

  .call-surface__timer {
    font-size: var(--text-xl);
  }

  .call-surface__status {
    min-height: 48px;
    margin-top: 6px;
  }

  .call-surface__controls {
    padding-top: 12px;
  }

  .call-surface__content.is-dtmf-open {
    padding-bottom: 18px;
  }

  .call-surface__content.is-dtmf-open .call-surface__meta {
    display: none;
  }

  .call-surface__dtmf {
    grid-template-rows: 34px repeat(4, 62px);
  }

  .call-surface__dtmf .keypad__key {
    width: 56px;
    height: 56px;
    align-self: center;
    justify-self: center;
  }

  .call-surface__primary-actions {
    --keypad-action-primary-size: 56px;
    --keypad-action-secondary-size: 48px;

    min-height: 96px;
    padding-block: 8px 10px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dtmf-enter-active,
  .dtmf-leave-active,
  .call-control__icon,
  .call-surface__ring {
    transition: none;
    animation: none;
  }

  .call-surface__ring {
    display: none;
  }
}
</style>
