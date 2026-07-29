<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CassetteTape,
  Grip,
  LoaderCircle,
  Mic,
  MicOff,
  PhoneCall,
  PhoneOff,
  RefreshCw,
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
  rememberCallRecordingPreference,
  setActiveCallRecording
} from '../state/recording'
import { playDTMFTone } from '../state/dtmfAudio'
import {
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineSupports
} from '../state/workspace'
import { knownCallBearerLabel } from '../callBearer'
import { formatDuration } from '../utils/format'
import { phoneKeypad } from '../utils/phoneKeypad'
import BaseAvatar from './BaseAvatar.vue'
import LineTag from './LineTag.vue'

const { t } = useI18n()
const now = ref(Date.now())
const dtmfOpen = ref(false)
const dtmfDisplay = ref<HTMLOutputElement>()
let timer: number | undefined

const session = computed(() => callState.session)
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
const phaseLabel = computed(() => {
  const labels = {
    unknown: t('calls.phaseUnknown'),
    dialing: t('calls.dialing'),
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
const duration = computed(() => {
  const start = session.value?.active_at
  if (!start) return ''
  const end = session.value?.ended_at ? Date.parse(session.value.ended_at) : now.value
  return formatDuration(Math.max(0, Math.floor((end - Date.parse(start)) / 1000)))
})
const incoming = computed(
  () =>
    session.value?.direction === 'incoming' &&
    session.value.phase === 'ringing' &&
    session.value.control_state === 'available'
)
const terminal = computed(
  () => session.value?.phase === 'ended' || session.value?.phase === 'failed'
)
const occupied = computed(
  () => Boolean(session.value?.control_state === 'occupied' && !terminal.value)
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
  const knownLabel = knownCallBearerLabel(bearer)
  if (knownLabel) return knownLabel
  if (bearer === 'unknown') return t('calls.bearerUnknown')
  return t('calls.otherBearer')
})
const answerUnavailable = computed(() =>
  lineSupports(line.value, 'answer') === false ? t('calls.answerUnsupported') : ''
)
const rejectUnavailable = computed(() =>
  lineSupports(line.value, 'reject') === false ? t('calls.rejectUnsupported') : ''
)
const hangupUnavailable = computed(() =>
  lineSupports(line.value, 'hangup') === false ? t('calls.hangupUnsupported') : ''
)
const dtmfUnavailable = computed(() =>
  lineSupports(line.value, 'dtmf') === false ? t('calls.dtmfUnsupported') : ''
)
const mediaLabel = computed(() => {
  if (!active.value || !callState.owned) return ''
  if (callMediaState.status === 'unavailable') return t('calls.serverAudioUnavailable')
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
const showMediaControls = computed(
  () =>
    active.value &&
    callState.owned &&
    !dtmfOpen.value &&
    (mediaControllable.value ||
      callMediaState.playbackBlocked ||
      callMediaState.status === 'error')
)
const recordingLabel = computed(() => {
  if (!active.value || !callState.owned) return ''
  if (callRecordingState.status === 'initializing') return t('calls.applyingRecording')
  if (callRecordingState.active) return t('calls.recordingActive')
  if (callRecordingState.enabled) return t('calls.recordingEnabled')
  return ''
})
const statusError = computed(
  () =>
    callState.error ||
    callState.syncError ||
    callRecordingState.error ||
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
  }
)

function tone(digit: string): void {
  void playDTMFTone(digit)
  void nextTick(() => {
    if (dtmfDisplay.value) dtmfDisplay.value.scrollLeft = dtmfDisplay.value.scrollWidth
  })
  void sendDTMF(digit)
}

function toggleRecording(): void {
  if (incoming.value && !callState.owned && session.value) {
    rememberCallRecordingPreference(
      session.value.id,
      !callRecordingState.enabled
    )
    return
  }
  void setActiveCallRecording(!callRecordingState.enabled)
}

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
    <section v-if="session" class="call-surface">
      <div class="call-surface__content" :class="{ 'is-dtmf-open': dtmfOpen }">
        <div class="call-surface__identity">
          <BaseAvatar
            :name="contactName"
            :src="contact?.avatar"
            size="large"
          />
          <strong>{{ contactName }}</strong>
          <span v-if="contactNumber">{{ contactNumber }}</span>
          <div class="call-surface__line">
            <LineTag
              v-if="line"
              :line="line"
              :fallback="session.line_id"
            />
            <small v-else>{{ session.line_id }}</small>
            <small v-if="bearerLabel" class="call-surface__bearer">{{ bearerLabel }}</small>
          </div>
        </div>

        <div class="call-surface__status">
          <span role="status" aria-live="polite" aria-atomic="true">
            {{ occupied ? t('calls.lineInUse') : phaseLabel }}
          </span>
          <strong v-if="duration" aria-live="off">{{ duration }}</strong>
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
            {{ t('calls.lineInUseDescription') }}
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
          <p
            v-if="recordingLabel"
            class="call-surface__recording"
            :class="{ 'is-active': callRecordingState.active }"
          >
            <i v-if="callRecordingState.active" aria-hidden="true" />
            {{ recordingLabel }}
          </p>
        </div>

        <div
          v-if="showMediaControls"
          class="call-surface__controls"
          role="group"
          :aria-label="t('calls.controls')"
        >
          <button
            v-if="mediaControllable"
            class="call-control"
            :class="{ 'is-active': callMediaState.muted }"
            type="button"
            :title="callMediaState.muted ? t('calls.unmute') : t('calls.mute')"
            :aria-label="
              callMediaState.muted
                ? t('calls.unmuteMicrophone')
                : t('calls.muteMicrophone')
            "
            :aria-pressed="callMediaState.muted"
            @click="toggleCallMute"
          >
            <span class="call-control__icon">
              <MicOff v-if="callMediaState.muted" :size="24" />
              <Mic v-else :size="24" />
            </span>
            <span>{{ callMediaState.muted ? t('calls.unmute') : t('calls.mute') }}</span>
          </button>
          <button
            v-if="callMediaState.playbackBlocked"
            class="call-control"
            type="button"
            :title="t('calls.enableSpeaker')"
            :aria-label="t('calls.enableCallAudio')"
            @click="resumeCallAudio"
          >
            <span class="call-control__icon"><Volume2 :size="24" /></span>
            <span>{{ t('calls.speaker') }}</span>
          </button>
          <button
            v-if="callMediaState.status === 'error'"
            class="call-control"
            type="button"
            :title="t('calls.retryBrowserAudio')"
            :aria-label="t('calls.retryBrowserAudio')"
            @click="retryCallMedia(session)"
          >
            <span class="call-control__icon"><RefreshCw :size="23" /></span>
            <span>{{ t('calls.retryAudio') }}</span>
          </button>
        </div>

        <Transition name="dtmf">
          <div
            v-if="active && dtmfOpen"
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
              :disabled="callState.busy"
              @click="tone(key.digit)"
            >
              <strong>{{ key.digit }}</strong>
              <small v-if="key.letters">{{ key.letters }}</small>
            </button>
          </div>
        </Transition>
      </div>

      <div class="call-surface__primary-actions">
        <button
          v-if="(incoming || active) && !occupied"
          class="call-footer-action call-footer-action--recording"
          :class="{ 'is-active': callRecordingState.enabled }"
          type="button"
          :title="
            callRecordingState.enabled
              ? t('calls.stopRecording')
              : t('calls.startRecording')
          "
          :aria-label="
            callRecordingState.enabled
              ? t('calls.stopCallRecording')
              : t('calls.startCallRecording')
          "
          :aria-pressed="callRecordingState.enabled"
          :disabled="
            callRecordingState.busy ||
            callRecordingState.status === 'initializing'
          "
          @click="toggleRecording"
        >
          <span class="call-footer-action__icon" aria-hidden="true">
            <LoaderCircle
              v-if="
                callRecordingState.busy ||
                callRecordingState.status === 'initializing'
              "
              class="spin"
              :size="20"
            />
            <CassetteTape v-else :size="21" />
            <span class="call-footer-action__state">
              {{
                callRecordingState.enabled
                  ? t('recordingSettings.enabled')
                  : t('recordingSettings.disabled')
              }}
            </span>
          </span>
          <small>{{ t('calls.record') }}</small>
        </button>
        <template v-if="incoming">
          <span class="call-primary-action call-primary-action--center">
            <button
              class="call-button call-button--hangup"
              type="button"
              :title="rejectUnavailable || t('calls.reject')"
              :aria-label="t('calls.reject')"
              :aria-describedby="rejectUnavailable ? 'call-capability-notice' : undefined"
              :disabled="callState.busy || Boolean(rejectUnavailable)"
              @click="rejectCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'reject'" class="spin" :size="25" />
              <PhoneOff v-else :size="25" />
            </button>
            <small>{{ t('calls.reject') }}</small>
          </span>
          <span class="call-primary-action call-primary-action--end">
            <button
              class="call-button call-button--answer"
              type="button"
              :title="answerUnavailable || t('calls.answer')"
              :aria-label="t('calls.answer')"
              :aria-describedby="answerUnavailable ? 'call-capability-notice' : undefined"
              :disabled="callState.busy || Boolean(answerUnavailable)"
              @click="answerCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'answer'" class="spin" :size="25" />
              <PhoneCall v-else :size="25" />
            </button>
            <small>{{ t('calls.answer') }}</small>
          </span>
        </template>
        <template v-else-if="canHangup && callState.owned">
          <span class="call-primary-action call-primary-action--center">
            <button
              class="call-button call-button--hangup"
              type="button"
              :title="hangupUnavailable || t('calls.hangup')"
              :aria-label="t('calls.hangup')"
              :aria-describedby="hangupUnavailable ? 'call-capability-notice' : undefined"
              :disabled="callState.busy || Boolean(hangupUnavailable)"
              @click="hangupCall"
            >
              <LoaderCircle v-if="callState.pendingAction === 'hangup'" class="spin" :size="25" />
              <PhoneOff v-else :size="25" />
            </button>
            <small>{{ t('calls.hangup') }}</small>
          </span>
          <button
            v-if="active"
            class="call-footer-action call-footer-action--keypad"
            :class="{ 'is-active': dtmfOpen }"
            type="button"
            :title="dtmfUnavailable || t('calls.keypad')"
            :aria-label="dtmfOpen ? t('calls.hideKeypad') : t('calls.keypad')"
            :aria-expanded="dtmfOpen"
            :disabled="Boolean(dtmfUnavailable)"
            @click="dtmfOpen = !dtmfOpen"
          >
            <span class="call-footer-action__icon" aria-hidden="true">
              <Grip :size="24" />
            </span>
            <small>{{ t('calls.keypadShort') }}</small>
          </button>
        </template>
        <span v-else-if="terminal" class="call-primary-action call-primary-action--center">
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
  </Transition>
</template>

<style scoped>
.call-surface {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
}

.call-surface__content {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  align-items: center;
  padding: 28px 20px 18px;
  overflow-y: auto;
}

.call-surface__identity {
  display: flex;
  width: 100%;
  min-width: 0;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  text-align: center;
}

.call-surface__identity :deep(.avatar--large) {
  width: 84px;
  height: 84px;
  margin-bottom: 9px;
  font-size: 23px;
}

.call-surface__identity > strong,
.call-surface__identity > span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-surface__identity > strong {
  font-size: 22px;
  line-height: 1.2;
}

.call-surface__identity > span {
  color: var(--muted);
  font-size: 14px;
}

.call-surface__line {
  display: flex;
  min-height: 24px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  margin-top: 5px;
}

.call-surface__line > small {
  max-width: 140px;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-surface__line .call-surface__bearer {
  color: var(--accent-strong);
  font-weight: 650;
}

.call-surface__status {
  display: flex;
  min-height: 54px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  margin-top: 12px;
}

.call-surface__status span {
  color: var(--muted);
  font-size: 14px;
}

.call-surface__status strong {
  font-variant-numeric: tabular-nums;
  font-size: 18px;
}

.call-surface__notices {
  display: flex;
  min-height: 24px;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 5px 10px;
  margin-top: 4px;
}

.call-surface__notices p {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin: 0;
  color: var(--muted);
  font-size: 11px;
  text-align: center;
}

.call-surface__notices .call-surface__error {
  width: 100%;
  justify-content: center;
  color: var(--danger);
}

.call-surface__recording {
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

.call-surface__controls {
  display: flex;
  width: min(100%, 264px);
  min-height: 82px;
  flex-wrap: wrap;
  align-items: start;
  justify-content: center;
  gap: 16px;
  margin-top: auto;
  padding-top: 22px;
}

.call-control {
  display: flex;
  width: 72px;
  min-height: 80px;
  flex-direction: column;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 11px;
  background: transparent;
}

.call-control__icon {
  display: inline-grid;
  width: 58px;
  height: 58px;
  place-items: center;
  color: var(--text);
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 50%;
  transition:
    color 150ms ease,
    background 150ms ease,
    border-color 150ms ease,
    transform 150ms ease;
}

.call-control:hover:not(:disabled) .call-control__icon {
  background: #e7ebee;
  transform: translateY(-1px);
}

.call-control.is-active .call-control__icon {
  color: #ffffff;
  background: var(--accent);
  border-color: var(--accent);
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
  font-size: 24px;
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

.call-surface__content.is-dtmf-open {
  padding-top: 18px;
  padding-bottom: 26px;
}

.call-surface__content.is-dtmf-open .call-surface__identity :deep(.avatar--large) {
  display: none;
}

.call-surface__content.is-dtmf-open .call-surface__identity > strong {
  font-size: 18px;
}

.call-surface__content.is-dtmf-open .call-surface__status {
  min-height: 42px;
  margin-top: 5px;
}

.call-surface__content.is-dtmf-open .call-surface__dtmf {
  margin-top: auto;
}

.call-surface__primary-actions {
  display: grid;
  min-height: 112px;
  flex: 0 0 auto;
  align-items: center;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 14px 28px 18px;
  border-top: 1px solid var(--border);
}

.call-primary-action {
  display: flex;
  min-width: 72px;
  flex-direction: column;
  align-items: center;
  gap: 6px;
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
  width: 64px;
  height: 64px;
  flex-basis: 64px;
}

.call-primary-action small {
  color: var(--muted);
  font-size: 11px;
}

.call-footer-action {
  display: flex;
  width: 72px;
  min-height: 80px;
  flex-direction: column;
  align-items: center;
  justify-self: center;
  gap: 7px;
  color: var(--muted);
  font-size: 11px;
  background: transparent;
}

.call-footer-action--recording {
  grid-column: 1;
}

.call-footer-action--keypad {
  grid-column: 3;
}

.call-footer-action__icon {
  display: inline-flex;
  width: 58px;
  height: 58px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  color: var(--text);
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 50%;
  transition:
    color 150ms ease,
    background 150ms ease,
    border-color 150ms ease,
    transform 150ms ease;
}

.call-footer-action:hover:not(:disabled) .call-footer-action__icon {
  background: #e7ebee;
  transform: translateY(-1px);
}

.call-footer-action--recording.is-active .call-footer-action__icon {
  color: #ffffff;
  background: var(--danger);
  border-color: var(--danger);
}

.call-footer-action--keypad.is-active .call-footer-action__icon {
  color: #ffffff;
  background: var(--accent);
  border-color: var(--accent);
}

.call-footer-action__state {
  color: inherit;
  font-size: 8px;
  font-weight: 800;
  line-height: 1;
}

.call-footer-action > small {
  color: inherit;
  font-size: 11px;
  white-space: nowrap;
}

.call-button--dismiss {
  color: var(--text);
  background: var(--surface-hover);
}

.call-button--dismiss:hover:not(:disabled) {
  color: var(--text);
  background: #e2e6e9;
}

.call-surface-enter-active,
.call-surface-leave-active,
.dtmf-enter-active,
.dtmf-leave-active {
  transition: opacity 150ms ease, transform 150ms ease;
}

.call-surface-enter-from,
.call-surface-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

.dtmf-enter-from,
.dtmf-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

@media (max-width: 860px) {
  .call-surface__content {
    padding-top: max(38px, env(safe-area-inset-top));
  }

  .call-surface__identity :deep(.avatar--large) {
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

  .call-surface__identity :deep(.avatar--large) {
    width: 68px;
    height: 68px;
    margin-bottom: 4px;
    font-size: 19px;
  }

  .call-surface__status {
    min-height: 42px;
    margin-top: 6px;
  }

  .call-surface__controls {
    padding-top: 12px;
  }

  .call-surface__content.is-dtmf-open {
    padding-bottom: 18px;
  }

  .call-surface__content.is-dtmf-open .call-surface__identity > span,
  .call-surface__content.is-dtmf-open .call-surface__line {
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
    min-height: 96px;
    padding-block: 10px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .call-surface-enter-active,
  .call-surface-leave-active,
  .dtmf-enter-active,
  .dtmf-leave-active,
  .call-control__icon,
  .call-footer-action__icon {
    transition: none;
  }
}

</style>
