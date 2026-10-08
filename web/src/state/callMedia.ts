import { reactive, watch } from 'vue'
import { loadAudioCore } from './audioCore'
import { fixtureCallMediaPreview, gateway } from '../api/client'
import type { CallSession } from '../api/types'
import { translate } from '../i18n'
import { CALL_AUDIO_FORMAT, CALL_AUDIO_MAX_AGE_MS, CallAudioReceiveClock, callAudioWebSocketURL, decodeCallAudioPacket, encodeCallAudioPacket, isCallAudioReady, isRecoverableCallAudioError } from './callAudioProtocol'
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
const MAX_SOCKET_BUFFER_BYTES = 5 * (12 + 1275)
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
let socketBufferedSince: number | undefined
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
  filter: BiquadFilterNode
}

type AudioRuntime = {
  coreModule: WebAssembly.Module
  context: AudioContext
  node: AudioWorkletNode
  destination: MediaStreamAudioDestinationNode
  worker?: Worker
  // Pending belongs to the current device epoch; in-flight belongs to this
  // worker and bounds its real queue across interruptions.
  contextEpoch: number
  captureCutoff: number
  encodePending: number
  decodePending: number
  encodeInFlight: number
  decodeInFlight: number
  captureBase?: number
  ready: boolean
  clock: CallAudioReceiveClock
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
  pipeline.filter.disconnect()
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
    // Low-pass before native-rate -> 16 kHz conversion prevents aliasing.
    const filter = context.createBiquadFilter()
    filter.type = 'lowpass'
    filter.frequency.value = Math.min(7000, context.sampleRate * 0.45)
    filter.Q.value = 0.707
    source.connect(gain).connect(limiter).connect(filter).connect(runtime.node)
    return { capture, context, source, gain, limiter, filter }
  } catch (error) {
    for (const track of capture.getTracks()) track.stop()
    throw error
  }
}

function clearSocketResources(): void {
  if (socketWatchdogID !== undefined) window.clearInterval(socketWatchdogID)
  socketWatchdogID = undefined
  socketBufferedSince = undefined
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
    audioRuntime.worker?.terminate()
    audioRuntime.worker = undefined
    audioRuntime.encodePending = audioRuntime.decodePending = 0
    audioRuntime.encodeInFlight = audioRuntime.decodeInFlight = 0
    audioRuntime.captureBase = undefined
    audioRuntime.sentFrames = audioRuntime.receivedFrames = 0
    audioRuntime.stableSince = undefined
    audioRuntime.lastSentAt = audioRuntime.lastReceivedAt = -Infinity
    audioRuntime.clock = new CallAudioReceiveClock(audioRuntime.coreModule)
    audioRuntime.node.port.postMessage({ type: 'clear' })
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
  if (now - runtime.lastSentAt > CALL_AUDIO_MAX_AGE_MS || now - runtime.lastReceivedAt > CALL_AUDIO_MAX_AGE_MS ||
      socketBufferedSince !== undefined) {
    runtime.stableSince = undefined
    return
  }
  runtime.stableSince ??= now
  if (now - runtime.stableSince < MEDIA_STABLE_WINDOW_MS || runtime.sentFrames < MEDIA_STABLE_FRAMES ||
      runtime.receivedFrames < MEDIA_STABLE_FRAMES) return
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

async function openAudioSocket(callID: string, token: number, ownership: MediaOwnership): Promise<void> {
  const runtime = audioRuntime
  if (!runtime || !isCurrent(callID, token)) return
  const worker = new Worker(new URL('./callOpus.worker.ts', import.meta.url), { type: 'module' })
  runtime.worker = worker
  connectTimeoutID = window.setTimeout(() => recoverConnection(callID, token, ownership), MEDIA_CONNECT_TIMEOUT_MS)
  worker.onerror = () => failConnection(callID, token, new Error(translate('runtime.callAudioFailed')))
  worker.onmessage = ({ data }: MessageEvent<{ type: string; message?: string; payload: Uint8Array; pcm: Float32Array; sequence: number; time: number; playAt: number; generation: number; sourceSamples: number; contextEpoch?: number; requestType?: 'encode' | 'decode' }>) => {
    if (!isCurrent(callID, token) || audioRuntime !== runtime || runtime.worker !== worker) return
    if (data.type === 'ready') {
      const connection = new WebSocket(callAudioWebSocketURL(callID, window.location))
      socket = connection
      connection.binaryType = 'arraybuffer'
      connection.onopen = () => {
        if (!isCurrent(callID, token) || socket !== connection) return
        ownership.claimed = true
        connection.send(JSON.stringify({ type: 'start', ...CALL_AUDIO_FORMAT, owner_token: ownership.ownerToken }))
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
              if (connectTimeoutID !== undefined) window.clearTimeout(connectTimeoutID)
              connectTimeoutID = undefined
              callMediaState.status = 'active'
              callMediaState.error = ''
              socketWatchdogID = window.setInterval(() => {
                if (!isCurrent(callID, token) || socket !== connection) return
                settleMediaRecovery(runtime)
                if (Date.now() - runtime.lastActivity > 15_000 ||
                    (socketBufferedSince !== undefined && performance.now() - socketBufferedSince > CALL_AUDIO_MAX_AGE_MS)) {
                  recoverConnection(callID, token, ownership)
                }
              }, 50)
              void playRemoteAudio()
            }
            return
          }
          if (!runtime.ready || !(event.data instanceof ArrayBuffer)) throw new Error(translate('runtime.callAudioFailed'))
          const packet = decodeCallAudioPacket(event.data)
          const now = performance.now()
          const oldGeneration = runtime.clock.generation
          if (!runtime.clock.accept(packet.sequence, packet.timestamp, now)) return
          if (runtime.clock.generation !== oldGeneration) {
            runtime.node.port.postMessage({ type: 'clear', generation: runtime.clock.generation })
          }
          if (runtime.context.state !== 'running' || runtime.decodeInFlight >= runtime.clock.queueCapacity) return
          runtime.decodePending += 1
          runtime.decodeInFlight += 1
          const playAt = runtime.context.currentTime + (runtime.clock.playAt - now) / 1000
          worker.postMessage({ type: 'decode', payload: packet.payload, sequence: packet.sequence,
            generation: runtime.clock.generation, sourceSamples: runtime.clock.sourceSamples, playAt, contextEpoch: runtime.contextEpoch }, [packet.payload.buffer])
        } catch (error) {
          failConnection(callID, token, error)
        }
      }
      connection.onerror = () => { /* onclose owns the bounded recovery */ }
      connection.onclose = event => {
        if (!isCurrent(callID, token) || socket !== connection) return
        // Policy/authentication/format errors require an explicit retry.
        if (event.code === 1008 || event.code === 1002 || event.code === 1003) {
          failConnection(callID, token, new Error(translate('runtime.callAudioConnectionFailed')))
        } else recoverConnection(callID, token, ownership)
      }
    } else if (data.type === 'encoded') {
      runtime.encodeInFlight = Math.max(0, runtime.encodeInFlight - 1)
      if (data.contextEpoch !== runtime.contextEpoch) return
      runtime.encodePending = Math.max(0, runtime.encodePending - 1)
      const connection = socket
      if (runtime.ready && runtime.context.state === 'running' && connection?.readyState === WebSocket.OPEN &&
          !runtime.clock.expired(data.time, runtime.context.currentTime)) {
        if (connection.bufferedAmount === 0) socketBufferedSince = undefined
        else socketBufferedSince ??= performance.now()
        if (connection.bufferedAmount > MAX_SOCKET_BUFFER_BYTES) {
          // A TCP writer stall must discard its queued audio through a new
          // connection; adding silence cannot clear WebSocket's internal queue.
          recoverConnection(callID, token, ownership)
          return
        }
        connection.send(encodeCallAudioPacket(data.sequence, data.payload))
        runtime.sentFrames += 1
        runtime.lastSentAt = performance.now()
      }
    } else if (data.type === 'decoded') {
      runtime.decodeInFlight = Math.max(0, runtime.decodeInFlight - 1)
      if (data.contextEpoch !== runtime.contextEpoch) return
      runtime.decodePending = Math.max(0, runtime.decodePending - 1)
      if (runtime.ready && runtime.context.state === 'running' && data.generation === runtime.clock.generation &&
          !runtime.clock.expired(data.playAt, runtime.context.currentTime)) {
        runtime.receivedFrames += 1
        runtime.lastReceivedAt = performance.now()
        runtime.node.port.postMessage({ type: 'play', pcm: data.pcm, playAt: data.playAt,
          sequence: data.sequence, generation: data.generation, sourceSamples: data.sourceSamples }, [data.pcm.buffer])
      }
    } else if (data.type === 'error') {
      if (data.requestType === 'encode') runtime.encodeInFlight = Math.max(0, runtime.encodeInFlight - 1)
      if (data.requestType === 'decode') runtime.decodeInFlight = Math.max(0, runtime.decodeInFlight - 1)
      if (data.contextEpoch !== undefined && data.contextEpoch !== runtime.contextEpoch) return
      failConnection(callID, token, new Error(data.message || translate('runtime.callAudioFailed')))
    }
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
    const coreModule = await loadAudioCore()
    if (!isCurrent(callID, token)) return
    pendingMicrophone = await navigator.mediaDevices.getUserMedia({ audio: selectedAudioInputConstraints(), video: false })
    if (!isCurrent(callID, token)) {
      for (const track of pendingMicrophone.getTracks()) track.stop()
      return
    }
    pendingContext = new AudioContext({ latencyHint: 'interactive' })
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
    const node = new AudioWorkletNode(context, 'modemdeck-call-audio', { numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1], processorOptions: { coreModule } })
    const destination = context.createMediaStreamDestination()
    node.connect(destination)
    const runtime: AudioRuntime = { coreModule, context, node, destination, contextEpoch: 0, captureCutoff: -Infinity, encodePending: 0, decodePending: 0, encodeInFlight: 0, decodeInFlight: 0, ready: false, clock: new CallAudioReceiveClock(coreModule), lastActivity: Date.now(), recoveryAttempts: 0, lastSentAt: -Infinity, lastReceivedAt: -Infinity, sentFrames: 0, receivedFrames: 0 }
    audioRuntime = runtime
    node.onprocessorerror = () => failConnection(callID, token, new Error(translate('runtime.callAudioFailed')))
    context.onstatechange = () => {
      if (isCurrent(callID, token) && audioRuntime === runtime) {
        callMediaState.playbackBlocked = context.state !== 'running'
        if (context.state !== 'running') {
          runtime.contextEpoch += 1
          runtime.captureCutoff = Math.max(runtime.captureCutoff, context.currentTime)
          runtime.encodePending = runtime.decodePending = 0
          runtime.sentFrames = runtime.receivedFrames = 0
          runtime.stableSince = undefined
          runtime.lastSentAt = runtime.lastReceivedAt = -Infinity
          node.port.postMessage({ type: 'clear', generation: runtime.clock.generation })
        }
      }
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
    node.port.onmessage = ({ data }: MessageEvent<{ type: string; pcm: Float32Array; index: number; time: number }>) => {
      if (!isCurrent(callID, token) || audioRuntime !== runtime || !runtime.ready || runtime.context.state !== 'running' || data.type !== 'capture') return
      if (data.time <= runtime.captureCutoff || runtime.clock.expired(data.time, runtime.context.currentTime) || runtime.encodeInFlight >= runtime.clock.sendQueueCapacity) return
      if (runtime.captureBase === undefined) runtime.captureBase = data.index
      // Sequence reflects capture time even when stale frames are discarded.
      const sequence = (data.index - runtime.captureBase) >>> 0
      runtime.encodePending += 1
      runtime.encodeInFlight += 1
      if (callMediaState.muted) data.pcm.fill(0)
      runtime.worker?.postMessage({ type: 'encode', pcm: data.pcm, sequence, time: data.time, contextEpoch: runtime.contextEpoch }, [data.pcm.buffer])
    }
    markAudioInputActive()
    remoteStream = destination.stream
    attachRemoteAudio(remoteStream)
    callMediaState.status = 'connecting'
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
