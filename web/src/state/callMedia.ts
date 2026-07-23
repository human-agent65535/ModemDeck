import { reactive, watch } from 'vue'
import { gateway } from '../api/client'
import type { CallSession } from '../api/types'
import {
  applySelectedAudioOutput,
  audioState,
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
  for (const track of localStream?.getTracks() || []) track.stop()
  for (const track of remoteStream?.getTracks() || []) track.stop()
  localStream = undefined
  remoteStream = undefined

  if (remoteAudio) {
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
    if (error.name === 'NotAllowedError') return '未授权浏览器使用麦克风'
    if (error.name === 'NotFoundError' || error.name === 'OverconstrainedError') {
      return audioState.selectedInputID ? '所选麦克风当前不可用' : '未找到可用麦克风'
    }
    if (error.name === 'NotReadableError') return '麦克风无法读取，可能正被其他应用占用'
  }
  return error instanceof Error ? error.message : '无法建立通话音频'
}

function waitForICEGathering(connection: RTCPeerConnection): Promise<void> {
  if (connection.iceGatheringState === 'complete') return Promise.resolve()

  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      connection.removeEventListener('icegatheringstatechange', onStateChange)
      reject(new Error('浏览器音频协商超时'))
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
  if (!remoteAudio) return
  if (!(await applySelectedAudioOutput(remoteAudio))) {
    remoteAudio.pause()
    callMediaState.playbackBlocked = false
    return
  }
  await remoteAudio.play().then(
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
  try {
    callMediaState.status = 'requesting'
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error('浏览器需要通过 HTTPS 才能使用麦克风')
    }
    const microphone = await navigator.mediaDevices.getUserMedia({
      audio: selectedAudioInputConstraints(),
      video: false
    })
    if (generation !== token || currentCallID !== callID) {
      for (const track of microphone.getTracks()) track.stop()
      return
    }
    localStream = microphone
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
        failConnection(callID, token, new Error('通话音频连接已断开'))
      } else if (connection.connectionState === 'failed') {
        failConnection(callID, token, new Error('通话音频连接失败'))
      }
    }

    for (const track of microphone.getAudioTracks()) {
      connection.addTrack(track, microphone)
    }
    const offer = await connection.createOffer()
    await connection.setLocalDescription(offer)
    await waitForICEGathering(connection)
    if (generation !== token || currentCallID !== callID) return

    const offerSDP = connection.localDescription?.sdp
    if (!offerSDP) throw new Error('浏览器没有生成音频协商信息')
    callMediaState.status = 'connecting'
    const answerSDP = await gateway.exchangeCallMedia(callID, offerSDP)
    if (generation !== token || currentCallID !== callID) return
    await connection.setRemoteDescription({ type: 'answer', sdp: answerSDP })
  } catch (error) {
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
  callMediaState.muted = muted
}

export function resumeCallAudio(): void {
  if (!remoteAudio) return
  void playRemoteAudio()
}

async function replaceCallInput(deviceID: string): Promise<void> {
  if (!peer || !localStream || !currentCallID) return
  const connection = peer
  const stream = localStream
  const callID = currentCallID
  const token = ++inputReplaceGeneration
  let replacement: MediaStream | undefined

  markAudioInputSwitching()
  try {
    replacement = await navigator.mediaDevices.getUserMedia({
      audio: selectedAudioInputConstraints(deviceID),
      video: false
    })
    if (
      token !== inputReplaceGeneration ||
      connection !== peer ||
      stream !== localStream ||
      callID !== currentCallID
    ) {
      for (const track of replacement.getTracks()) track.stop()
      return
    }

    const newTrack = replacement.getAudioTracks()[0]
    const sender = connection
      .getSenders()
      .find(candidate => candidate.track?.kind === 'audio')
    if (!newTrack || !sender) throw new Error('当前通话没有可替换的麦克风轨道')

    await sender.replaceTrack(newTrack)
    if (
      token !== inputReplaceGeneration ||
      connection !== peer ||
      stream !== localStream ||
      callID !== currentCallID
    ) {
      newTrack.stop()
      return
    }

    localStream = replacement
    replacement = undefined
    for (const track of stream.getTracks()) track.stop()
    callMediaState.muted = false
    markAudioInputActive()
    void refreshAudioDevices()
  } catch (error) {
    for (const track of replacement?.getTracks() || []) track.stop()
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
  () => audioState.selectedInputID,
  deviceID => {
    if (peer && localStream && currentCallID) queueCallInputReplacement(deviceID)
  }
)
