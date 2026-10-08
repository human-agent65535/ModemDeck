import { reactive, watch } from 'vue'
import { loadAudioCore } from './audioCore.ts'
import { fixtureCallMediaPreview, gateway } from '../api/client'
import type { CallSession } from '../api/types'
import { translate } from '../i18n'
import { CALL_AUDIO_FORMAT, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError, validateCallAudioProgress } from './callAudioProtocol'
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
  | 'recovering'
  | 'active'
  | 'error'

const MEDIA_CONNECT_TIMEOUT_MS = 5000
const MEDIA_RECONNECT_DELAY_MS = 500
const MEDIA_STABLE_WINDOW_MS = 1000
const MEDIA_STABLE_FRAMES = 40
// Transport resource bounds, not an audio freshness/deadline policy.
const MEDIA_WRITE_TIMEOUT_MS = 2000
const MEDIA_HEALTH_INTERVAL_MS = 1000
const MEDIA_RECOVERY_TIMEOUT_MS = 15_000
const MEDIA_LOCK_PREFIX = 'modemdeck-call-media:'

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
let socket: WebSocket | undefined
let audioRuntime: AudioRuntime | undefined
let reconnectTimeoutID: number | undefined
let connectTimeoutID: number | undefined
let socketWatchdogID: number | undefined
let localStream: MediaStream | undefined
let remoteStream: MediaStream | undefined
let remoteAudio: HTMLAudioElement | undefined
let inputReplaceGeneration = 0
let queuedInputDeviceID: string | undefined
let inputSwitchPromise: Promise<void> | undefined
let recoveryTimeoutID: number | undefined
let mediaLockAbort: AbortController | undefined

type MediaOwnership = {
  ownerToken: string
  claimed: boolean
  release: () => void
  cleanup: Promise<void>
}

let mediaOwnership: MediaOwnership | undefined

type MicrophonePipeline = {
  capture: MediaStream
  context: AudioContext
  source: MediaStreamAudioSourceNode
  gain: GainNode
  limiter: DynamicsCompressorNode
}

type AudioRuntime = {
  context: AudioContext
  node: AudioWorkletNode
  destination: MediaStreamAudioDestinationNode
  contextEpoch: number
  cancelInitialization?: () => void
  receivePending: number
  lastPacket?: { sequence: number; timestamp: number }
  sentBytes: number
  pendingWrites: { endOffset: number; sentAt: number }[]
  realOutputSamples: number
  lastRealRenderUs: number
  ingressCapacity: number
  sendCapacity: number
  resetSequence: boolean
  ready: boolean
  lastActivity: number
  recoveryAttempts: number
  stableSince?: number
  lastSentAt: number
  lastReceivedAt: number
  sentFrames: number
  receivedFrames: number
}

let microphonePipeline: MicrophonePipeline | undefined

function clearRecoveryWindow(): void {
  if (recoveryTimeoutID === undefined) return
  window.clearTimeout(recoveryTimeoutID)
  recoveryTimeoutID = undefined
}

function stopMicrophonePipeline(pipeline: MicrophonePipeline): void {
  for (const track of pipeline.capture.getTracks()) track.stop()
  pipeline.source.disconnect()
  pipeline.gain.disconnect()
  pipeline.limiter.disconnect()
}

async function createMicrophonePipeline(capture: MediaStream): Promise<MicrophonePipeline> {
  const runtime = audioRuntime
  if (!runtime) throw new Error(translate('runtime.callAudioFailed'))
  const context = runtime.context
  try {
    const source = context.createMediaStreamSource(capture)
    const gain = context.createGain()
    gain.gain.value = audioState.microphoneGain / 100
    const limiter = context.createDynamicsCompressor()
    limiter.threshold.value = -3
    limiter.knee.value = 0
    limiter.ratio.value = 20
    limiter.attack.value = 0.003
    limiter.release.value = 0.1
    source.connect(gain).connect(limiter).connect(runtime.node)
    return { capture, context, source, gain, limiter }
  } catch (error) {
    for (const track of capture.getTracks()) track.stop()
    throw error
  }
}

function clearSocketResources(): void {
  if (socketWatchdogID !== undefined) window.clearInterval(socketWatchdogID)
  socketWatchdogID = undefined
  if (connectTimeoutID !== undefined) window.clearTimeout(connectTimeoutID)
  connectTimeoutID = undefined
  if (reconnectTimeoutID !== undefined) window.clearTimeout(reconnectTimeoutID)
  reconnectTimeoutID = undefined
  const old = socket
  socket = undefined
  if (old) {
    old.onopen = old.onmessage = old.onerror = old.onclose = null
    old.close()
  }
  if (audioRuntime) {
    audioRuntime.ready = false
    audioRuntime.lastPacket = undefined
    audioRuntime.sentBytes = 0
    audioRuntime.pendingWrites.length = 0
    audioRuntime.resetSequence = true
    updateAudioRuntime(audioRuntime)
  }
}

function stopResources(): void {
  generation += 1
  inputReplaceGeneration += 1
  queuedInputDeviceID = undefined
  currentCallID = ''
  clearRecoveryWindow()
  mediaLockAbort?.abort()
  mediaLockAbort = undefined
  mediaOwnership?.release()

  audioRuntime?.cancelInitialization?.()
  clearSocketResources()
  if (audioRuntime) {
    audioRuntime.node.port.onmessage = null
    audioRuntime.node.disconnect()
    audioRuntime.destination.disconnect()
    void audioRuntime.context.close()
    audioRuntime = undefined
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
  const context = audioRuntime?.context
  if (context && context.state !== 'running' && context.state !== 'closed') await context.resume().catch(() => undefined)
  await element.play().then(
    () => {
      callMediaState.playbackBlocked = Boolean(context && context.state !== 'running')
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

function beginRecoveryWindow(callID: string, token: number): void {
  if (recoveryTimeoutID !== undefined) return
  recoveryTimeoutID = window.setTimeout(() => {
    recoveryTimeoutID = undefined
    failConnection(
      callID,
      token,
      new Error(translate('runtime.callAudioConnectionFailed'))
    )
  }, MEDIA_RECOVERY_TIMEOUT_MS)
}

function isCurrent(callID: string, token: number): boolean {
  return generation === token && currentCallID === callID
}

function settleMediaRecovery(runtime: AudioRuntime): void {
  if (recoveryTimeoutID === undefined) return
  const now = performance.now()
  if (now - runtime.lastSentAt > MEDIA_HEALTH_INTERVAL_MS || now - runtime.lastReceivedAt > MEDIA_HEALTH_INTERVAL_MS ||
      now - runtime.lastRealRenderUs / 1000 > MEDIA_HEALTH_INTERVAL_MS || runtime.realOutputSamples === 0 ||
      (runtime.pendingWrites[0] && now - runtime.pendingWrites[0].sentAt > MEDIA_HEALTH_INTERVAL_MS)) {
    runtime.stableSince = undefined
    return
  }
  runtime.stableSince ??= now
  if (now - runtime.stableSince < MEDIA_STABLE_WINDOW_MS || runtime.sentFrames < MEDIA_STABLE_FRAMES ||
      runtime.receivedFrames < MEDIA_STABLE_FRAMES || runtime.realOutputSamples < 48000) return
  // A ready handshake alone does not prove useful media. Keep the original
  // deadline across repeated early failures; reset only after healthy duplex.
  clearRecoveryWindow()
  runtime.recoveryAttempts = 0
}

function recoverConnection(callID: string, token: number, ownership: MediaOwnership): void {
  if (!isCurrent(callID, token)) return
  clearSocketResources()
  beginRecoveryWindow(callID, token)
  callMediaState.status = 'recovering'
  callMediaState.error = ''
  const attempt = audioRuntime ? audioRuntime.recoveryAttempts++ : 0
  const delay = MEDIA_RECONNECT_DELAY_MS * 2 ** Math.min(attempt, 2)
  // Release immediately: WebSocket.close itself may flush buffered frames.
  // Wait for the old owner to release before the next upgrade. DELETE is
  // owner scoped, so stale tabs cannot release a replacement owner's media.
  void gateway.releaseCallMedia(callID, ownership.ownerToken).then(() => {
    if (!isCurrent(callID, token)) return
    reconnectTimeoutID = window.setTimeout(() => {
      reconnectTimeoutID = undefined
      if (isCurrent(callID, token)) void openAudioSocket(callID, token, ownership)
    }, delay)
  }).catch(() => {
    if (!isCurrent(callID, token)) return
    reconnectTimeoutID = window.setTimeout(() => {
      reconnectTimeoutID = undefined
      if (isCurrent(callID, token)) recoverConnection(callID, token, ownership)
    }, delay)
  })
}

function updateAudioRuntime(runtime: AudioRuntime): void {
  // A new device/connection epoch invalidates packets already in MessagePort.
  // NetEq alone decides network lateness while this epoch is running.
  runtime.contextEpoch += 1
  runtime.receivePending = 0
  runtime.realOutputSamples = 0
  runtime.lastRealRenderUs = -Infinity
  runtime.sentFrames = runtime.receivedFrames = 0
  runtime.stableSince = undefined
  runtime.lastSentAt = runtime.lastReceivedAt = -Infinity
  if (runtime.ready && runtime.context.state === 'running') {
    runtime.node.port.postMessage({ type: 'activate', epoch: runtime.contextEpoch,
      nowUs: performance.now() * 1000, contextTime: runtime.context.currentTime,
      sequence: runtime.resetSequence ? 0 : undefined })
    runtime.resetSequence = false
  } else {
    runtime.node.port.postMessage({ type: 'deactivate', epoch: runtime.contextEpoch })
  }
}

type WorkletAudioMessage = { type: string; epoch: number; sequence: number; payload: Uint8Array; message?: string; realOutputSamples?: number; lastRealRenderUs?: number; renderErrors?: number; ingressCapacity?: number; sendCapacity?: number }
function receiveAudioMessage(callID: string, token: number, ownership: MediaOwnership, runtime: AudioRuntime, data: WorkletAudioMessage): void {
  if (!isCurrent(callID, token) || audioRuntime !== runtime || data.epoch !== runtime.contextEpoch) return
  if (data.type === 'error') {
    if (data.message?.includes('backpressure')) recoverConnection(callID, token, ownership)
    else failConnection(callID, token, new Error(data.message || translate('runtime.callAudioFailed')))
    return
  }
  if (!runtime.ready || runtime.context.state !== 'running') return
  if (data.type === 'health') {
    runtime.realOutputSamples = data.realOutputSamples ?? 0
    runtime.lastRealRenderUs = data.lastRealRenderUs ?? -Infinity
  } else if (data.type === 'received') {
    runtime.receivePending = Math.max(0, runtime.receivePending - 1)
    runtime.receivedFrames += 1
    runtime.lastReceivedAt = performance.now()
  } else if (data.type === 'encoded') {
    runtime.node.port.postMessage({ type: 'ack', epoch: runtime.contextEpoch })
    const connection = socket
    if (connection?.readyState !== WebSocket.OPEN) return
    if (updateSocketProgress(runtime, connection) || runtime.pendingWrites.length >= runtime.sendCapacity) {
      recoverConnection(callID, token, ownership)
      return
    }
    try {
      const packet = encodeCallAudioPacket(data.sequence, data.payload)
      connection.send(packet)
      runtime.sentBytes += packet.byteLength
      runtime.pendingWrites.push({ endOffset: runtime.sentBytes, sentAt: performance.now() })
    } catch (error) {
      failConnection(callID, token, error)
      return
    }
    runtime.sentFrames += 1
    runtime.lastSentAt = performance.now()
  }
}

function updateSocketProgress(runtime: AudioRuntime, connection: WebSocket): boolean {
  // WebSocket exposes bytes, not completed frames. Keep only frame boundaries
  // and timestamps so a steadily draining nonempty queue never looks stalled.
  const drainedBytes = runtime.sentBytes - connection.bufferedAmount
  while (runtime.pendingWrites[0] && runtime.pendingWrites[0].endOffset <= drainedBytes) runtime.pendingWrites.shift()
  return Boolean(runtime.pendingWrites[0] && performance.now() - runtime.pendingWrites[0].sentAt > MEDIA_WRITE_TIMEOUT_MS)
}

async function openAudioSocket(callID: string, token: number, ownership: MediaOwnership): Promise<void> {
  const runtime = audioRuntime
  if (!runtime || !isCurrent(callID, token)) return
  connectTimeoutID = window.setTimeout(() => recoverConnection(callID, token, ownership), MEDIA_CONNECT_TIMEOUT_MS)
  const connection = new WebSocket(callAudioWebSocketURL(callID, window.location))
  socket = connection
  connection.binaryType = 'arraybuffer'
  connection.onopen = () => {
    if (!isCurrent(callID, token) || socket !== connection) return
    ownership.claimed = true
    connection.send(JSON.stringify({ type: 'start', ...CALL_AUDIO_FORMAT, owner_token: ownership.ownerToken }))
    runtime.sentBytes = connection.bufferedAmount
    runtime.pendingWrites.length = 0
  }
  connection.onmessage = event => {
    if (!isCurrent(callID, token) || socket !== connection) return
    try {
      runtime.lastActivity = Date.now()
      if (typeof event.data === 'string') {
        const message = JSON.parse(event.data) as Record<string, unknown>
        if (message.type === 'error') {
          if (isRecoverableCallAudioError(message.code)) {
            recoverConnection(callID, token, ownership)
            return
          }
          throw new Error(typeof message.message === 'string' ? message.message : translate('runtime.callAudioFailed'))
        }
        if (message.type === 'ready') {
          if (runtime.ready || !isCallAudioReady(message)) throw new Error(translate('runtime.callAudioFailed'))
          runtime.ready = true
          updateAudioRuntime(runtime)
          if (connectTimeoutID !== undefined) window.clearTimeout(connectTimeoutID)
          connectTimeoutID = undefined
          callMediaState.status = 'active'
          callMediaState.error = ''
          socketWatchdogID = window.setInterval(() => {
            if (!isCurrent(callID, token) || socket !== connection) return
            const stalled = updateSocketProgress(runtime, connection)
            settleMediaRecovery(runtime)
            if (Date.now() - runtime.lastActivity > 15_000 || stalled) {
              recoverConnection(callID, token, ownership)
            }
          }, 50)
          void playRemoteAudio()
        }
        return
      }
      if (!runtime.ready || !(event.data instanceof ArrayBuffer)) throw new Error(translate('runtime.callAudioFailed'))
      const packet = decodeCallAudioPacket(event.data)
      validateCallAudioProgress(runtime.lastPacket, packet)
      runtime.lastPacket = { sequence: packet.sequence, timestamp: packet.timestamp }
      if (runtime.context.state !== 'running') return
      if (runtime.receivePending >= runtime.ingressCapacity) {
        recoverConnection(callID, token, ownership)
        return
      }
      runtime.receivePending += 1
      runtime.node.port.postMessage({ type: 'packet', payload: packet.payload, sequence: packet.sequence,
        timestamp: packet.timestamp, nowUs: performance.now() * 1000, epoch: runtime.contextEpoch }, [packet.payload.buffer])
    } catch (error) {
      failConnection(callID, token, error)
    }
  }
  connection.onerror = () => { /* onclose owns the bounded recovery */ }
  connection.onclose = event => {
    if (!isCurrent(callID, token) || socket !== connection) return
    if (event.code === 1008 || event.code === 1002 || event.code === 1003) {
      failConnection(callID, token, new Error(translate('runtime.callAudioConnectionFailed')))
    } else recoverConnection(callID, token, ownership)
  }
}

async function connect(callID: string, token: number, ownership: MediaOwnership): Promise<void> {
  let pendingMicrophone: MediaStream | undefined
  let pendingContext: AudioContext | undefined
  try {
    callMediaState.status = 'requesting'
    if (!navigator.mediaDevices?.getUserMedia || typeof AudioWorkletNode === 'undefined' || typeof WebAssembly === 'undefined') {
      throw new Error(translate('runtime.microphoneHTTPSRequired'))
    }
    const wasmBinary = await loadAudioCore()
    if (!isCurrent(callID, token)) return
    pendingMicrophone = await navigator.mediaDevices.getUserMedia({ audio: selectedAudioInputConstraints(), video: false })
    if (!isCurrent(callID, token)) {
      for (const track of pendingMicrophone.getTracks()) track.stop()
      return
    }
    pendingContext = new AudioContext({ latencyHint: 'interactive', sampleRate: 48000 })
    // Autoplay may need the existing Resume button; capture can still connect.
    void pendingContext.resume().catch(() => undefined)
    const { default: workletURL } = await import('./callAudio.worklet.ts?worker&url')
    await pendingContext.audioWorklet.addModule(workletURL)
    if (!isCurrent(callID, token)) {
      for (const track of pendingMicrophone.getTracks()) track.stop()
      void pendingContext.close()
      return
    }
    const context = pendingContext
    const node = new AudioWorkletNode(context, 'modemdeck-call-audio', { numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1], processorOptions: { wasmBinary } })
    const destination = context.createMediaStreamDestination()
    node.connect(destination)
    const runtime: AudioRuntime = { context, node, destination, contextEpoch: 0, receivePending: 0, sentBytes: 0, pendingWrites: [], realOutputSamples: 0, lastRealRenderUs: -Infinity, ingressCapacity: 0, sendCapacity: 0,
      resetSequence: true, ready: false, lastActivity: Date.now(), recoveryAttempts: 0,
      lastSentAt: -Infinity, lastReceivedAt: -Infinity, sentFrames: 0, receivedFrames: 0 }
    audioRuntime = runtime
    const initialized = new Promise<void>((resolve, reject) => {
      const timeout = window.setTimeout(() => reject(new Error(translate('runtime.callAudioFailed'))), MEDIA_CONNECT_TIMEOUT_MS)
      runtime.cancelInitialization = () => {
        window.clearTimeout(timeout)
        runtime.cancelInitialization = undefined
        resolve()
      }
      node.onprocessorerror = () => {
        runtime.cancelInitialization = undefined
        window.clearTimeout(timeout)
        reject(new Error(translate('runtime.callAudioFailed')))
        failConnection(callID, token, new Error(translate('runtime.callAudioFailed')))
      }
      node.port.onmessage = ({ data }: MessageEvent<WorkletAudioMessage>) => {
        if (data.type === 'ready') {
          if (!data.ingressCapacity || !data.sendCapacity) { reject(new Error('Invalid audio resource limits')); return }
          runtime.ingressCapacity = data.ingressCapacity
          runtime.sendCapacity = data.sendCapacity
          runtime.cancelInitialization = undefined
          window.clearTimeout(timeout)
          resolve()
        } else receiveAudioMessage(callID, token, ownership, runtime, data)
      }
    })
    context.onstatechange = () => {
      if (!isCurrent(callID, token) || audioRuntime !== runtime) return
      callMediaState.playbackBlocked = context.state !== 'running'
      updateAudioRuntime(runtime)
    }
    pendingContext = undefined
    localStream = pendingMicrophone
    pendingMicrophone = undefined
    const pipeline = await createMicrophonePipeline(localStream)
    if (!isCurrent(callID, token) || audioRuntime !== runtime) {
      stopMicrophonePipeline(pipeline)
      return
    }
    microphonePipeline = pipeline
    markAudioInputActive()
    remoteStream = destination.stream
    attachRemoteAudio(remoteStream)
    callMediaState.status = 'connecting'
    // Expose Resume before waiting: a suspended WebKit renderer may defer its
    // processor ready message until the existing user gesture resumes audio.
    await initialized
    if (!isCurrent(callID, token) || audioRuntime !== runtime) return
    await openAudioSocket(callID, token, ownership)
  } catch (error) {
    for (const track of pendingMicrophone?.getTracks() || []) track.stop()
    if (pendingContext) void pendingContext.close()
    failConnection(callID, token, error)
  }
}

async function ownAndConnect(
  callID: string,
  token: number,
  controller: AbortController
): Promise<void> {
  try {
    if (!navigator.locks?.request) {
      throw new Error(translate('runtime.callAudioFailed'))
    }
    await navigator.locks.request(
      `${MEDIA_LOCK_PREFIX}${callID}`,
      { mode: 'exclusive', signal: controller.signal },
      async lock => {
        if (!lock || generation !== token || currentCallID !== callID) return

        let releaseLock: () => void = () => undefined
        const released = new Promise<void>(resolve => {
          releaseLock = resolve
        })
        let completeCleanup: () => void = () => undefined
        const cleanup = new Promise<void>(resolve => {
          completeCleanup = resolve
        })
        const ownership: MediaOwnership = {
          ownerToken: globalThis.crypto.randomUUID(),
          claimed: false,
          release: releaseLock,
          cleanup
        }
        mediaOwnership = ownership
        try {
          await connect(callID, token, ownership)
          await released
        } finally {
          try {
            if (ownership.claimed) {
              await gateway
                .releaseCallMedia(callID, ownership.ownerToken)
                .catch(() => undefined)
            }
          } finally {
            if (mediaOwnership === ownership) mediaOwnership = undefined
            completeCleanup()
          }
        }
      }
    )
  } catch (error) {
    if (controller.signal.aborted || generation !== token || currentCallID !== callID) {
      return
    }
    failConnection(callID, token, error)
  } finally {
    if (mediaLockAbort === controller) mediaLockAbort = undefined
  }
}

export function syncCallMedia(session: CallSession | null): void {
  if (fixtureCallMediaPreview) {
    if (!session || session.phase !== 'active') {
      attemptedCallID = ''
      setIdle('idle')
      return
    }
    if (callMediaState.callID === session.id && callMediaState.status === 'active') return
    setIdle('idle')
    callMediaState.callID = session.id
    callMediaState.status = 'active'
    return
  }
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
  const controller = new AbortController()
  mediaLockAbort = controller
  void ownAndConnect(session.id, token, controller)
}

export function retryCallMedia(session: CallSession | null): void {
  if (!session || session.phase !== 'active' || !session.media_available) return
  attemptedCallID = ''
  syncCallMedia(session)
}

export function toggleCallMute(): void {
  if (fixtureCallMediaPreview) {
    if (callMediaState.status === 'active') {
      callMediaState.muted = !callMediaState.muted
    }
    return
  }
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
  if (!audioRuntime || !localStream || !microphonePipeline || !currentCallID) return
  const runtime = audioRuntime
  const stream = localStream
  const pipeline = microphonePipeline
  const callID = currentCallID
  const token = ++inputReplaceGeneration
  let replacement: MediaStream | undefined
  let replacementPipeline: MicrophonePipeline | undefined
  markAudioInputSwitching()
  try {
    replacement = await navigator.mediaDevices.getUserMedia({ audio: selectedAudioInputConstraints(deviceID), video: false })
    // Disable capture before connecting the new graph. Read the latest mute
    // value only when installed, so an in-flight switch cannot unmute audio.
    for (const track of replacement.getAudioTracks()) track.enabled = false
    if (token !== inputReplaceGeneration || runtime !== audioRuntime || stream !== localStream || callID !== currentCallID) {
      for (const track of replacement.getTracks()) track.stop()
      return
    }
    replacementPipeline = await createMicrophonePipeline(replacement)
    if (token !== inputReplaceGeneration || runtime !== audioRuntime || stream !== localStream || callID !== currentCallID) {
      stopMicrophonePipeline(replacementPipeline)
      return
    }
    localStream = replacement
    microphonePipeline = replacementPipeline
    stopMicrophonePipeline(pipeline)
    for (const track of localStream.getAudioTracks()) track.enabled = !callMediaState.muted
    replacement = undefined
    replacementPipeline = undefined
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

export async function releaseCallMediaForSessionEnd(): Promise<void> {
  const ownership = mediaOwnership
  shutdownCallMedia()
  if (ownership?.claimed) await ownership.cleanup
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
    if (audioRuntime && localStream && currentCallID) queueCallInputReplacement(deviceID)
  }
)
