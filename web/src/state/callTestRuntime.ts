import { computed, ref, watch, type WatchStopHandle } from 'vue'
import { gateway } from '../api/client'
import type { CallSession } from '../api/types'
import { resolvedLocale, translate } from '../i18n'
import { stopMicrophoneTest } from './audio'
import { callState, isLiveCallSession } from './call'
import { callMediaState, stopCallTestMedia, syncCallMedia } from './callMedia'
import { createCallTestController } from './callTest'
import { showBrowserNotification } from './browserNotifications'
import { stopSoundPreview, syncCallTestSounds } from './browserSounds'
import { showCallSurface } from './ui'

const visible = ref(false)
const acceptedAt = ref('')
const endedAt = ref('')
export const callTestUnavailable = computed(() => callState.busy ||
  callState.sessions.some(isLiveCallSession) || callState.reservations.length > 0 ||
  Boolean(callState.session && isLiveCallSession(callState.session)))

const controller = createCallTestController({
  gateway,
  unavailable: () => callTestUnavailable.value,
  failureMessage: () => translate('callTest.failed'),
  startMedia: id => syncCallMedia({ id, phase: 'active', media_available: true }, 'call-tests'),
  stopMedia: stopCallTestMedia,
  notify: () => {
    visible.value = true
    showCallSurface()
    const notification = new AbortController()
    showBrowserNotification({
      title: translate('callTest.incoming'), body: translate('callTest.notificationBody'),
      tag: 'modemdeck-call-test', signal: notification.signal,
      onClick: () => { window.focus(); showCallSurface() }
    })
    return () => notification.abort()
  }
})

export const callTestState = controller.state
export const callTestInProgress = computed(() =>
  ['scheduled', 'ringing', 'starting', 'running'].includes(callTestState.status))
export const callTestSession = computed<CallSession | null>(() => {
  const status = callTestState.status
  if (!visible.value || status === 'idle' || status === 'scheduled') return null
  const phase = status === 'ringing' ? 'ringing' : status === 'starting' ? 'connecting' :
    status === 'running' ? 'active' : status === 'error' ? 'failed' : 'ended'
  return {
    id: callTestState.session?.id || `test-browser-${acceptedAt.value}`,
    line_id: '', direction: 'incoming', remote_number: '',
    display_name: translate('callTest.incoming'), phase,
    control_state: phase === 'ringing' ? 'available' : 'owned',
    media_available: phase === 'active', created_at: acceptedAt.value,
    active_at: callTestState.session?.accepted_at,
    ended_at: endedAt.value || undefined,
    failure_reason: callTestState.error || undefined
  }
})

// This presentation never enters the real-call collection, recording store or
// SIM controls. The shell and controls display it through their usual surface.
export const presentedCallSession = computed(() => callTestSession.value || callState.session)
const phaseKeys: Record<string, string> = {
  connecting: 'connecting', guide: 'guide', tone: 'tone', speak: 'speak',
  playback_prompt: 'playbackPrompt', playback: 'playback', pause: 'pause'
}
export const callTestStatusLabel = computed(() => {
  const status = callTestState.status
  if (status === 'scheduled') return translate('callTest.scheduled')
  if (status === 'ringing') return translate('callTest.incoming')
  if (status === 'starting') return translate('callTest.starting')
  if (status === 'error') return translate('callTest.failed')
  if (status !== 'running') return translate(callTestState.connected ? 'callTest.finishedConnected' : 'callTest.finished')
  if (callMediaState.status === 'requesting') return translate('calls.requestingMicrophone')
  if (callMediaState.status === 'recovering') return translate('calls.reconnectingBrowserAudio')
  const phase = callTestState.session?.test_audio?.phase || 'connecting'
  return translate(`callTest.${phaseKeys[phase] || 'connecting'}`)
})

export function scheduleCallTest(): void {
  if (callTestUnavailable.value || callTestInProgress.value) return
  visible.value = false
  acceptedAt.value = new Date().toISOString()
  endedAt.value = ''
  stopMicrophoneTest()
  stopSoundPreview()
  controller.schedule(resolvedLocale())
}

export const answerCallTest = controller.answer
export const stopCallTest = controller.stop
export function dismissCallTest(): void { visible.value = false }

let stops: WatchStopHandle[] = []
export function initializeCallTestRuntime(): void {
  if (stops.length) return
  stops = [
    watch(callTestUnavailable, value => {
      if (!value) return
      visible.value = false
      void controller.stop(translate('callTest.interrupted'))
    }, { flush: 'sync' }),
    watch(() => [callMediaState.callID, callMediaState.status] as const, ([id, status]) => {
      if (callTestState.status !== 'running') return
      if (id !== callTestState.session?.id || status === 'idle') {
        void controller.stop(translate('callTest.interrupted'))
      } else if (status === 'error') {
        void controller.stop(callMediaState.error || translate('callTest.failed'))
      } else if (status === 'active') {
        callTestState.connected = true
      }
    }),
    watch(() => callTestState.status, status => {
      if (status === 'ended' || status === 'error') endedAt.value = new Date().toISOString()
    }, { flush: 'sync' }),
    watch(callTestSession, session => syncCallTestSounds(session), { flush: 'sync' })
  ]
}

export function shutdownCallTestRuntime(): void {
  visible.value = false
  void controller.stop()
  for (const stop of stops) stop()
  stops = []
  syncCallTestSounds(null)
}
