import { reactive, watch } from 'vue'
import { gateway } from '../api/client'
import type { CallSession } from '../api/types'
import { translate } from '../i18n'
import {
  applySelectedAudioOutput,
  audioState,
  cancelSelectedAudioOutputApplication,
  markAudioInputActive,
  markAudioInputError,
  markAudioInputInactive,
  markAudioInputSwitching,
  markAudioOutputInactive,
  refreshAudioDevices,
  selectedAudioInputConstraints
} from './audio'

export type CallMediaStatus =
  | 'idle'
  | 'unavailable'
  | 'requesting'
  | 'connecting'
  | 'active'
  | 'error'

const ICE_GATHERING_TIMEOUT_MS = 5000

export const callMediaState = reactive<{
  callID: string
  status: CallMediaStatus
  error: string
  muted: boolean
  playbackBlocked: boolean
}>({
  callID: '',
  status: 'idle',
  error: '',
  muted: false,
  playbackBlocked: false
})

let generation = 0
let attemptedCallID = ''
let currentCallID = ''
let peer: RTCPeerConnection | undefined
let localStream: MediaStream | undefined
let remoteStream: MediaStream | undefined
let remoteAudio: HTMLAudioElement | undefined
let inputReplaceGeneration = 0
let queuedInputDeviceID: string | undefined
let inputSwitchPromise: Promise<void> | undefined

type MicrophonePipeline = {
  capture: MediaStream
  context: AudioContext
  source: MediaStreamAudioSourceNode
  gain: GainNode
  limiter: DynamicsCompressorNode
  destination: MediaStreamAudioDestinationNode
  track: MediaStreamTrack
}

let microphonePipeline: MicrophonePipeline | undefined

function stopMicrophonePipeline(pipeline: MicrophonePipeline): void {
  for (const track of pipeline.capture.getTracks()) track.stop()
  for (const track of pipeline.destination.stream.getTracks()) track.stop()
  pipeline.source.disconnect()
  pipeline.gain.disconnect()
  pipeline.limiter.disconnect()
  pipeline.destination.disconnect()
  void pipeline.context.close()
}

async function createMicrophonePipeline(
  capture: MediaStream
): Promise<MicrophonePipeline> {
  const context = new AudioContext()
  try {
    await context.resume()
    const source = context.createMediaStreamSource(capture)
    const gain = context.createGain()
    gain.gain.value = audioState.microphoneGain / 100

    const limiter = context.createDynamicsCompressor()
    limiter.threshold.value = -3
    limiter.knee.value = 0
    limiter.ratio.value = 20
    limiter.attack.value = 0.003
    limiter.release.value = 0.1

    const destination = context.createMediaStreamDestination()
    source.connect(gain).connect(limiter).connect(destination)
    const track = destination.stream.getAudioTracks()[0]
    if (!track) throw new Error(translate('runtime.microphoneTrackMissing'))

    return { capture, context, source, gain, limiter, destination, track }
  } catch (error) {
    for (const track of capture.getTracks()) track.stop()
    void context.close()
    throw error
  }
}

function stopResources(): void {
  generation += 1
  inputReplaceGeneration += 1
  queuedInputDeviceID = undefined
  currentCallID = ''

  if (peer) {
    peer.onconnectionstatechange = null
    peer.ontrack = null
    peer.close()
    peer = undefined
  }
  if (microphonePipeline) {
    stopMicrophonePipeline(microphonePipeline)
    microphonePipeline = undefined
  } else {
    for (const track of localStream?.getTracks() || []) track.stop()
  }
  for (const track of remoteStream?.getTracks() || []) track.stop()
  localStream = undefined
  remoteStream = undefined

  if (remoteAudio) {
    cancelSelectedAudioOutputApplication(remoteAudio)
    remoteAudio.pause()
    remoteAudio.srcObject = null
    remoteAudio.remove()
    remoteAudio = undefined
  }
  markAudioInputInactive()
  markAudioOutputInactive()
}

function setIdle(status: 'idle' | 'unavailable', callID = ''): void {
  stopResources()
  callMediaState.callID = callID
  callMediaState.status = status
  callMediaState.error = ''
  callMediaState.muted = false
  callMediaState.playbackBlocked = false
}

function mediaError(error: unknown): string {
  if (error instanceof DOMException) {
    if (error.name === 'NotAllowedError') return translate('runtime.microphoneUnauthorized')
    if (error.name === 'NotFoundError' || error.name === 'OverconstrainedError') {
      return audioState.selectedInputID
        ? translate('runtime.microphoneUnavailable')
        : translate('runtime.noMicrophone')
    }
    if (error.name === 'NotReadableError') return translate('runtime.microphoneBusy')
  }
  return error instanceof Error ? error.message : translate('runtime.callAudioFailed')
}

function waitForICEGathering(connection: RTCPeerConnection): Promise<void> {
  if (connection.iceGatheringState === 'complete') return Promise.resolve()

  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      connection.removeEventListener('icegatheringstatechange', onStateChange)
      reject(new Error(translate('runtime.audioNegotiationTimeout')))
    }, ICE_GATHERING_TIMEOUT_MS)
    const onStateChange = () => {
      if (connection.iceGatheringState !== 'complete') return
      window.clearTimeout(timeout)
      connection.removeEventListener('icegatheringstatechange', onStateChange)
      resolve()
    }
    connection.addEventListener('icegatheringstatechange', onStateChange)
  })
}

async function playRemoteAudio(): Promise<void> {
  const element = remoteAudio
  if (!element) return
  element.volume = audioState.callVolume / 100
  if (!(await applySelectedAudioOutput(element))) {
    element.pause()
    callMediaState.playbackBlocked = false
    return
  }
  if (remoteAudio !== element) return
  await element.play().then(
    () => {
      callMediaState.playbackBlocked = false
    },
    () => {
      callMediaState.playbackBlocked = true
    }
  )
}

function attachRemoteAudio(stream: MediaStream): void {
  if (!remoteAudio) {
    remoteAudio = document.createElement('audio')
    remoteAudio.autoplay = true
    remoteAudio.setAttribute('playsinline', '')
    remoteAudio.hidden = true
    document.body.append(remoteAudio)
  }
  remoteAudio.srcObject = stream
  void playRemoteAudio()
}

function failConnection(callID: string, token: number, error: unknown): void {
  if (generation !== token || currentCallID !== callID) return
  stopResources()
  callMediaState.callID = callID
  callMediaState.status = 'error'
  callMediaState.error = mediaError(error)
  callMediaState.muted = false
  markAudioInputError(callMediaState.error)
}

async function connect(callID: string, token: number): Promise<void> {
  let pendingMicrophone: MediaStream | undefined
  let pendingPipeline: MicrophonePipeline | undefined
  try {
    callMediaState.status = 'requesting'
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error(translate('runtime.microphoneHTTPSRequired'))
    }
    pendingMicrophone = await navigator.mediaDevices.getUserMedia({
      audio: selectedAudioInputConstraints(),
      video: false
    })
    pendingPipeline = await createMicrophonePipeline(pendingMicrophone)
    if (generation !== token || currentCallID !== callID) {
      stopMicrophonePipeline(pendingPipeline)
      return
    }
    const microphone = pendingMicrophone
    const pipeline = pendingPipeline
    localStream = microphone
    microphonePipeline = pipeline
    pendingMicrophone = undefined
    pendingPipeline = undefined
    markAudioInputActive()

    const connection = new RTCPeerConnection()
    peer = connection
    remoteStream = new MediaStream()
    attachRemoteAudio(remoteStream)

    connection.ontrack = event => {
      if (generation !== token || currentCallID !== callID || !remoteStream) return
      const tracks = event.streams[0]?.getTracks() || [event.track]
      for (const track of tracks) {
        if (!remoteStream.getTracks().some(existing => existing.id === track.id)) {
          remoteStream.addTrack(track)
        }
      }
      attachRemoteAudio(remoteStream)
    }
    connection.onconnectionstatechange = () => {
      if (generation !== token || currentCallID !== callID) return
      if (connection.connectionState === 'connected') {
        callMediaState.status = 'active'
        callMediaState.error = ''
      } else if (connection.connectionState === 'connecting') {
        callMediaState.status = 'connecting'
      } else if (connection.connectionState === 'disconnected') {
        failConnection(callID, token, new Error(translate('runtime.callAudioDisconnected')))
      } else if (connection.connectionState === 'failed') {
        failConnection(callID, token, new Error(translate('runtime.callAudioConnectionFailed')))
      }
    }

    for (const track of pipeline.destination.stream.getAudioTracks()) {
      connection.addTrack(track, pipeline.destination.stream)
    }
    const offer = await connection.createOffer()
    await connection.setLocalDescription(offer)
    await waitForICEGathering(connection)
    if (generation !== token || currentCallID !== callID) return

    const offerSDP = connection.localDescription?.sdp
    if (!offerSDP) throw new Error(translate('runtime.audioOfferMissing'))
    callMediaState.status = 'connecting'
    const answerSDP = await gateway.exchangeCallMedia(callID, offerSDP)
    if (generation !== token || currentCallID !== callID) return
    await connection.setRemoteDescription({ type: 'answer', sdp: answerSDP })
  } catch (error) {
    if (pendingPipeline) stopMicrophonePipeline(pendingPipeline)
    else for (const track of pendingMicrophone?.getTracks() || []) track.stop()
    failConnection(callID, token, error)
  }
}

export function syncCallMedia(session: CallSession | null): void {
  if (!session || session.phase !== 'active') {
    attemptedCallID = ''
    setIdle('idle')
    return
  }
  if (!session.media_available) {
    attemptedCallID = ''
    setIdle('unavailable', session.id)
    return
  }
  if (currentCallID === session.id || attemptedCallID === session.id) return

  stopResources()
  attemptedCallID = session.id
  currentCallID = session.id
  callMediaState.callID = session.id
  callMediaState.status = 'requesting'
  callMediaState.error = ''
  callMediaState.muted = false
  callMediaState.playbackBlocked = false
  const token = generation
  void connect(session.id, token)
}

export function retryCallMedia(session: CallSession | null): void {
  if (!session || session.phase !== 'active' || !session.media_available) return
  attemptedCallID = ''
  syncCallMedia(session)
}

export function toggleCallMute(): void {
  if (!localStream) return
  const muted = !callMediaState.muted
  for (const track of localStream.getAudioTracks()) track.enabled = !muted
  for (const track of microphonePipeline?.destination.stream.getAudioTracks() || []) {
    track.enabled = !muted
  }
  callMediaState.muted = muted
}

export function resumeCallAudio(): void {
  if (!remoteAudio) return
  void playRemoteAudio()
}

async function replaceCallInput(deviceID: string): Promise<void> {
  if (!peer || !localStream || !microphonePipeline || !currentCallID) return
  const connection = peer
  const stream = localStream
  const pipeline = microphonePipeline
  const callID = currentCallID
  const token = ++inputReplaceGeneration
  let replacement: MediaStream | undefined
  let replacementPipeline: MicrophonePipeline | undefined

  markAudioInputSwitching()
  try {
    replacement = await navigator.mediaDevices.getUserMedia({
      audio: selectedAudioInputConstraints(deviceID),
      video: false
    })
    replacementPipeline = await createMicrophonePipeline(replacement)
    if (
      token !== inputReplaceGeneration ||
      connection !== peer ||
      stream !== localStream ||
      callID !== currentCallID
    ) {
      stopMicrophonePipeline(replacementPipeline)
      return
    }

    const newTrack = replacementPipeline.track
    const sender = connection
      .getSenders()
      .find(candidate => candidate.track?.kind === 'audio')
    if (!newTrack || !sender) throw new Error(translate('runtime.microphoneTrackMissing'))

    await sender.replaceTrack(newTrack)
    if (
      token !== inputReplaceGeneration ||
      connection !== peer ||
      stream !== localStream ||
      callID !== currentCallID
    ) {
      stopMicrophonePipeline(replacementPipeline)
      return
    }

    localStream = replacement
    microphonePipeline = replacementPipeline
    replacement = undefined
    replacementPipeline = undefined
    stopMicrophonePipeline(pipeline)
    callMediaState.muted = false
    markAudioInputActive()
    void refreshAudioDevices()
  } catch (error) {
    if (replacementPipeline) stopMicrophonePipeline(replacementPipeline)
    else for (const track of replacement?.getTracks() || []) track.stop()
    if (token !== inputReplaceGeneration || callID !== currentCallID) return
    markAudioInputError(mediaError(error))
  }
}

function queueCallInputReplacement(deviceID: string): void {
  queuedInputDeviceID = deviceID
  if (inputSwitchPromise) return

  inputSwitchPromise = (async () => {
    while (queuedInputDeviceID !== undefined) {
      const nextDeviceID = queuedInputDeviceID
      queuedInputDeviceID = undefined
      await replaceCallInput(nextDeviceID)
    }
  })().finally(() => {
    inputSwitchPromise = undefined
  })
}

export function shutdownCallMedia(): void {
  attemptedCallID = ''
  setIdle('idle')
}

watch(
  () => [audioState.selectedOutputID, audioState.devicesRevision] as const,
  () => {
    if (remoteAudio) void playRemoteAudio()
  }
)

watch(
  () => audioState.callVolume,
  volume => {
    if (remoteAudio) remoteAudio.volume = volume / 100
  }
)

watch(
  () => audioState.microphoneGain,
  gain => {
    const pipeline = microphonePipeline
    if (!pipeline) return
    pipeline.gain.gain.setTargetAtTime(
      gain / 100,
      pipeline.context.currentTime,
      0.015
    )
  }
)

watch(
  () => audioState.selectedInputID,
  deviceID => {
    if (peer && localStream && currentCallID) queueCallInputReplacement(deviceID)
  }
)
