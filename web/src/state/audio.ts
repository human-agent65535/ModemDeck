import { reactive } from 'vue'
import { translate } from '../i18n'

export type AudioDeviceLoadStatus = 'idle' | 'loading' | 'ready' | 'error'
export type MicrophoneAccessStatus =
  | 'insecure-context'
  | 'unsupported'
  | 'prompt'
  | 'pending'
  | 'granted'
  | 'denied'
  | 'no-device'
  | 'error'
export type MicrophoneTestStatus = 'idle' | 'requesting' | 'active' | 'error'
export type AudioInputRoutingStatus =
  | 'default'
  | 'selected'
  | 'switching'
  | 'active'
  | 'unavailable'
  | 'error'
export type AudioOutputRoutingStatus =
  | 'default'
  | 'selected'
  | 'active'
  | 'unsupported'
  | 'unavailable'
  | 'error'

type SinkSelectableMediaElement = HTMLMediaElement & {
  setSinkId?: (deviceID: string) => Promise<void>
}

type AudioState = {
  inputs: MediaDeviceInfo[]
  outputs: MediaDeviceInfo[]
  devicesStatus: AudioDeviceLoadStatus
  devicesError: string
  devicesRevision: number
  microphoneAccessStatus: MicrophoneAccessStatus
  microphoneAccessError: string
  selectedInputID: string
  selectedOutputID: string
  inputRoutingStatus: AudioInputRoutingStatus
  inputRoutingError: string
  outputSelectionSupported: boolean
  outputRoutingStatus: AudioOutputRoutingStatus
  outputRoutingError: string
  microphoneTestStatus: MicrophoneTestStatus
  microphoneTestLevel: number
  microphoneTestSeconds: number
  microphoneTestError: string
  microphoneGain: number
  callVolume: number
  ringAlertsVolume: number
  recordingPlaybackVolume: number
}

const INPUT_STORAGE_KEY = 'modemdeck.audio.input-device'
const OUTPUT_STORAGE_KEY = 'modemdeck.audio.output-device'
const LEVELS_STORAGE_KEY = 'modemdeck.audio.levels.v1'
const MICROPHONE_TEST_LIMIT_MS = 30_000
const MICROPHONE_SAMPLE_MS = 80

export type BrowserAudioLevels = {
  microphoneGain: number
  callVolume: number
  ringAlertsVolume: number
  recordingPlaybackVolume: number
}

const DEFAULT_AUDIO_LEVELS: BrowserAudioLevels = {
  microphoneGain: 100,
  callVolume: 100,
  ringAlertsVolume: 100,
  recordingPlaybackVolume: 100
}

function normalizedLevel(
  value: unknown,
  fallback: number,
  minimum: number,
  maximum: number
): number {
  return typeof value === 'number' && Number.isFinite(value)
    ? Math.round(Math.min(maximum, Math.max(minimum, value)))
    : fallback
}

export function normalizeAudioLevels(raw: unknown): BrowserAudioLevels {
  if (!raw || typeof raw !== 'object') return { ...DEFAULT_AUDIO_LEVELS }
  const value = raw as Record<string, unknown>
  return {
    microphoneGain: normalizedLevel(
      value.microphoneGain,
      DEFAULT_AUDIO_LEVELS.microphoneGain,
      0,
      200
    ),
    callVolume: normalizedLevel(
      value.callVolume,
      DEFAULT_AUDIO_LEVELS.callVolume,
      0,
      100
    ),
    ringAlertsVolume: normalizedLevel(
      value.ringAlertsVolume,
      DEFAULT_AUDIO_LEVELS.ringAlertsVolume,
      0,
      100
    ),
    recordingPlaybackVolume: normalizedLevel(
      value.recordingPlaybackVolume,
      DEFAULT_AUDIO_LEVELS.recordingPlaybackVolume,
      0,
      100
    )
  }
}

function storedAudioLevels(): BrowserAudioLevels {
  try {
    const stored = window.localStorage.getItem(LEVELS_STORAGE_KEY)
    return stored ? normalizeAudioLevels(JSON.parse(stored)) : { ...DEFAULT_AUDIO_LEVELS }
  } catch {
    return { ...DEFAULT_AUDIO_LEVELS }
  }
}

function persistAudioLevels(): void {
  try {
    window.localStorage.setItem(
      LEVELS_STORAGE_KEY,
      JSON.stringify({
        microphoneGain: audioState.microphoneGain,
        callVolume: audioState.callVolume,
        ringAlertsVolume: audioState.ringAlertsVolume,
        recordingPlaybackVolume: audioState.recordingPlaybackVolume
      } satisfies BrowserAudioLevels)
    )
  } catch {
    // The in-memory levels still apply when browser storage is unavailable.
  }
}

function storedDeviceID(key: string): string {
  try {
    return window.localStorage.getItem(key)?.trim() || ''
  } catch {
    return ''
  }
}

function persistDeviceID(key: string, deviceID: string): void {
  try {
    if (deviceID) window.localStorage.setItem(key, deviceID)
    else window.localStorage.removeItem(key)
  } catch {
    // Selection still applies for this session when storage is unavailable.
  }
}

function supportsOutputSelection(): boolean {
  if (typeof HTMLMediaElement === 'undefined') return false
  return typeof (HTMLMediaElement.prototype as SinkSelectableMediaElement).setSinkId === 'function'
}

function initialMicrophoneAccessStatus(): MicrophoneAccessStatus {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') return 'unsupported'
  if (!window.isSecureContext) return 'insecure-context'
  if (
    typeof RTCPeerConnection === 'undefined' ||
    typeof navigator.mediaDevices?.getUserMedia !== 'function' ||
    typeof navigator.mediaDevices?.enumerateDevices !== 'function'
  ) {
    return 'unsupported'
  }
  return 'prompt'
}

const selectedInputID = storedDeviceID(INPUT_STORAGE_KEY)
const selectedOutputID = storedDeviceID(OUTPUT_STORAGE_KEY)
const outputSelectionSupported = supportsOutputSelection()
const storedLevels = storedAudioLevels()

export const audioState = reactive<AudioState>({
  inputs: [],
  outputs: [],
  devicesStatus: 'idle',
  devicesError: '',
  devicesRevision: 0,
  microphoneAccessStatus: initialMicrophoneAccessStatus(),
  microphoneAccessError: '',
  selectedInputID,
  selectedOutputID,
  inputRoutingStatus: selectedInputID ? 'selected' : 'default',
  inputRoutingError: '',
  outputSelectionSupported,
  outputRoutingStatus: selectedOutputID
    ? outputSelectionSupported
      ? 'selected'
      : 'unsupported'
    : 'default',
  outputRoutingError:
    selectedOutputID && !outputSelectionSupported
      ? translate('runtime.audioOutputUnsupported')
      : '',
  microphoneTestStatus: 'idle',
  microphoneTestLevel: 0,
  microphoneTestSeconds: 0,
  microphoneTestError: '',
  ...storedLevels
})

let deviceListenerActive = false
let refreshPromise: Promise<void> | undefined
let startupAccessAttempted = false
let startupAccessPromise: Promise<void> | undefined
let microphonePermissionGranted = false
let outputApplyGeneration = 0
let microphoneTestGeneration = 0
let microphoneTestStream: MediaStream | undefined
let microphoneTestContext: AudioContext | undefined
let microphoneTestAnalyser: AnalyserNode | undefined
let microphoneTestGain: GainNode | undefined
let microphoneSampleTimer: number | undefined
let microphoneLimitTimer: number | undefined
let microphoneCountdownTimer: number | undefined

function selectedDeviceMissing(kind: 'input' | 'output'): boolean {
  if (audioState.devicesStatus !== 'ready') return false
  const selectedID =
    kind === 'input' ? audioState.selectedInputID : audioState.selectedOutputID
  const devices = kind === 'input' ? audioState.inputs : audioState.outputs
  return Boolean(selectedID) && !devices.some(device => device.deviceId === selectedID)
}

function updateOutputSelectionStatus(preserveActive = true): void {
  outputApplyGeneration += 1
  if (!audioState.selectedOutputID) {
    audioState.outputRoutingStatus = 'default'
    audioState.outputRoutingError = ''
    return
  }
  if (!audioState.outputSelectionSupported) {
    audioState.outputRoutingStatus = 'unsupported'
    audioState.outputRoutingError = translate('runtime.audioOutputUnsupported')
    return
  }
  if (selectedDeviceMissing('output')) {
    audioState.outputRoutingStatus = 'unavailable'
    audioState.outputRoutingError = translate('runtime.audioOutputUnavailable')
    return
  }
  if (!preserveActive || audioState.outputRoutingStatus !== 'active') {
    audioState.outputRoutingStatus = 'selected'
  }
  audioState.outputRoutingError = ''
}

function updateInputSelectionStatus(preserveActive = true): void {
  if (!audioState.selectedInputID) {
    audioState.inputRoutingStatus = 'default'
    audioState.inputRoutingError = ''
    return
  }
  if (selectedDeviceMissing('input')) {
    audioState.inputRoutingStatus = 'unavailable'
    audioState.inputRoutingError = translate('runtime.microphoneUnavailable')
    return
  }
  if (
    !preserveActive ||
    (audioState.inputRoutingStatus !== 'active' &&
      audioState.inputRoutingStatus !== 'switching')
  ) {
    audioState.inputRoutingStatus = 'selected'
  }
  audioState.inputRoutingError = ''
}

export function inputDeviceMissing(): boolean {
  return selectedDeviceMissing('input')
}

export function outputDeviceMissing(): boolean {
  return selectedDeviceMissing('output')
}

export function setSelectedAudioInput(deviceID: string): void {
  audioState.selectedInputID = deviceID.trim()
  persistDeviceID(INPUT_STORAGE_KEY, audioState.selectedInputID)
  updateInputSelectionStatus()
}

export function setSelectedAudioOutput(deviceID: string): void {
  audioState.selectedOutputID = deviceID.trim()
  persistDeviceID(OUTPUT_STORAGE_KEY, audioState.selectedOutputID)
  updateOutputSelectionStatus()
}

export function setMicrophoneGain(value: number): void {
  audioState.microphoneGain = normalizedLevel(value, audioState.microphoneGain, 0, 200)
  if (microphoneTestGain && microphoneTestContext) {
    microphoneTestGain.gain.setTargetAtTime(
      audioState.microphoneGain / 100,
      microphoneTestContext.currentTime,
      0.015
    )
  }
  persistAudioLevels()
}

export function setCallVolume(value: number): void {
  audioState.callVolume = normalizedLevel(value, audioState.callVolume, 0, 100)
  persistAudioLevels()
}

export function setRingAlertsVolume(value: number): void {
  audioState.ringAlertsVolume = normalizedLevel(
    value,
    audioState.ringAlertsVolume,
    0,
    100
  )
  persistAudioLevels()
}

export function setRecordingPlaybackVolume(value: number): void {
  audioState.recordingPlaybackVolume = normalizedLevel(
    value,
    audioState.recordingPlaybackVolume,
    0,
    100
  )
  persistAudioLevels()
}

export function selectedAudioInputConstraints(
  deviceID = audioState.selectedInputID
): MediaTrackConstraints {
  return {
    autoGainControl: true,
    echoCancellation: true,
    noiseSuppression: true,
    ...(deviceID
      ? { deviceId: { exact: deviceID } }
      : {})
  }
}

export function refreshAudioDevices(): Promise<void> {
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    if (!navigator.mediaDevices?.enumerateDevices) {
      audioState.devicesStatus = 'error'
      audioState.devicesError = translate('runtime.enumerateAudioFailed')
      return
    }

    audioState.devicesStatus = 'loading'
    audioState.devicesError = ''
    try {
      const devices = await navigator.mediaDevices.enumerateDevices()
      audioState.inputs = devices.filter(device => device.kind === 'audioinput')
      audioState.outputs = devices.filter(device => device.kind === 'audiooutput')
      audioState.devicesStatus = 'ready'
      audioState.devicesRevision += 1
      if (
        microphonePermissionGranted &&
        (audioState.microphoneAccessStatus === 'granted' ||
          audioState.microphoneAccessStatus === 'no-device')
      ) {
        audioState.microphoneAccessStatus =
          audioState.inputs.length > 0 ? 'granted' : 'no-device'
      }
      updateInputSelectionStatus()
      updateOutputSelectionStatus()
    } catch (error) {
      audioState.devicesStatus = 'error'
      audioState.devicesError =
        error instanceof Error ? error.message : translate('runtime.readAudioDevicesFailed')
    }
  })().finally(() => {
    refreshPromise = undefined
  })

  return refreshPromise
}

async function refreshAudioDevicesAfterPermission(): Promise<void> {
  if (refreshPromise) await refreshPromise
  await refreshAudioDevices()
}

async function microphonePermissionState(): Promise<PermissionState | undefined> {
  if (!navigator.permissions?.query) return undefined
  try {
    const status = await navigator.permissions.query({
      name: 'microphone' as PermissionName
    })
    return status.state
  } catch {
    return undefined
  }
}

async function setMicrophoneAccessFailure(error: unknown): Promise<void> {
  microphonePermissionGranted = false
  audioState.microphoneAccessError = ''

  if (error instanceof DOMException) {
    if (error.name === 'NotAllowedError' || error.name === 'SecurityError') {
      const permission = await microphonePermissionState()
      audioState.microphoneAccessStatus = permission === 'prompt' ? 'prompt' : 'denied'
      return
    }
    if (
      error.name === 'NotFoundError' ||
      error.name === 'DevicesNotFoundError' ||
      error.name === 'OverconstrainedError'
    ) {
      audioState.microphoneAccessStatus = 'no-device'
      return
    }
    if (error.name === 'NotReadableError') {
      audioState.microphoneAccessStatus = 'error'
      audioState.microphoneAccessError = translate('runtime.microphoneBusy')
      return
    }
  }

  audioState.microphoneAccessStatus = 'error'
  audioState.microphoneAccessError =
    error instanceof Error ? error.message : translate('runtime.microphonePermissionFailed')
}

export function initializeBrowserAudio(): Promise<void> {
  initializeAudioDevices()
  if (startupAccessPromise) return startupAccessPromise
  if (startupAccessAttempted) return Promise.resolve()
  startupAccessAttempted = true

  const environmentStatus = initialMicrophoneAccessStatus()
  if (environmentStatus !== 'prompt') {
    audioState.microphoneAccessStatus = environmentStatus
    audioState.microphoneAccessError = ''
    return Promise.resolve()
  }

  startupAccessPromise = (async () => {
    let temporaryStream: MediaStream | undefined
    let permissionGranted = false
    audioState.microphoneAccessStatus = 'pending'
    audioState.microphoneAccessError = ''

    try {
      temporaryStream = await navigator.mediaDevices.getUserMedia({
        audio: true,
        video: false
      })
      permissionGranted = true
      microphonePermissionGranted = true
      audioState.microphoneAccessStatus = 'granted'
    } catch (error) {
      await setMicrophoneAccessFailure(error)
    } finally {
      for (const track of temporaryStream?.getTracks() || []) track.stop()
      await refreshAudioDevicesAfterPermission()

      if (permissionGranted) {
        if (audioState.devicesStatus === 'error') {
          audioState.microphoneAccessStatus = 'error'
          audioState.microphoneAccessError =
            audioState.devicesError || translate('runtime.readAudioDevicesFailed')
        } else {
          audioState.microphoneAccessStatus =
            audioState.inputs.length > 0 ? 'granted' : 'no-device'
        }
      }
    }
  })().finally(() => {
    startupAccessPromise = undefined
  })

  return startupAccessPromise
}

function onDeviceChange(): void {
  void refreshAudioDevices()
}

export function initializeAudioDevices(): void {
  if (deviceListenerActive || !navigator.mediaDevices?.addEventListener) return
  navigator.mediaDevices.addEventListener('devicechange', onDeviceChange)
  deviceListenerActive = true
}

export function shutdownAudioDevices(): void {
  if (deviceListenerActive && navigator.mediaDevices?.removeEventListener) {
    navigator.mediaDevices.removeEventListener('devicechange', onDeviceChange)
  }
  deviceListenerActive = false
  stopMicrophoneTest()
}

export async function applySelectedAudioOutput(
  element: HTMLMediaElement
): Promise<boolean> {
  const token = ++outputApplyGeneration
  const selectedID = audioState.selectedOutputID
  const sinkElement = element as SinkSelectableMediaElement

  if (!selectedID) {
    try {
      if (typeof sinkElement.setSinkId === 'function') await sinkElement.setSinkId('')
      if (token !== outputApplyGeneration) return false
      audioState.outputRoutingStatus = 'default'
      audioState.outputRoutingError = ''
      return true
    } catch (error) {
      if (token !== outputApplyGeneration) return false
      audioState.outputRoutingStatus = 'error'
      audioState.outputRoutingError =
        error instanceof Error
          ? error.message
          : translate('runtime.restoreDefaultOutputFailed')
      return false
    }
  }

  if (typeof sinkElement.setSinkId !== 'function') {
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'unsupported'
    audioState.outputRoutingError = translate('runtime.audioOutputUnsupported')
    return false
  }
  if (selectedDeviceMissing('output')) {
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'unavailable'
    audioState.outputRoutingError = translate('runtime.audioOutputUnavailable')
    return false
  }

  try {
    await sinkElement.setSinkId(selectedID)
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'active'
    audioState.outputRoutingError = ''
    return true
  } catch (error) {
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'error'
    audioState.outputRoutingError =
      error instanceof Error ? error.message : translate('runtime.useOutputFailed')
    return false
  }
}

export function markAudioOutputInactive(): void {
  updateOutputSelectionStatus(false)
}

export function markAudioInputSwitching(): void {
  audioState.inputRoutingStatus = 'switching'
  audioState.inputRoutingError = ''
}

export function markAudioInputActive(): void {
  audioState.inputRoutingStatus = 'active'
  audioState.inputRoutingError = ''
}

export function markAudioInputError(message: string): void {
  audioState.inputRoutingStatus = 'error'
  audioState.inputRoutingError = message
}

export function markAudioInputInactive(): void {
  updateInputSelectionStatus(false)
}

function releaseMicrophoneTestResources(): void {
  if (microphoneSampleTimer !== undefined) window.clearInterval(microphoneSampleTimer)
  if (microphoneLimitTimer !== undefined) window.clearTimeout(microphoneLimitTimer)
  if (microphoneCountdownTimer !== undefined) window.clearInterval(microphoneCountdownTimer)
  microphoneSampleTimer = undefined
  microphoneLimitTimer = undefined
  microphoneCountdownTimer = undefined

  for (const track of microphoneTestStream?.getTracks() || []) track.stop()
  microphoneTestStream = undefined
  microphoneTestAnalyser = undefined
  microphoneTestGain?.disconnect()
  microphoneTestGain = undefined
  if (microphoneTestContext) void microphoneTestContext.close()
  microphoneTestContext = undefined
}

export function stopMicrophoneTest(): void {
  microphoneTestGeneration += 1
  releaseMicrophoneTestResources()
  audioState.microphoneTestStatus = 'idle'
  audioState.microphoneTestLevel = 0
  audioState.microphoneTestSeconds = 0
  audioState.microphoneTestError = ''
}

function microphoneTestError(error: unknown): string {
  if (error instanceof DOMException) {
    if (error.name === 'NotAllowedError') return translate('runtime.microphoneUnauthorized')
    if (error.name === 'NotFoundError' || error.name === 'OverconstrainedError') {
      return audioState.selectedInputID
        ? translate('runtime.microphoneUnavailable')
        : translate('runtime.noMicrophone')
    }
    if (error.name === 'NotReadableError') return translate('runtime.microphoneBusy')
  }
  return error instanceof Error ? error.message : translate('runtime.microphoneTestFailed')
}

export async function startMicrophoneTest(): Promise<void> {
  stopMicrophoneTest()
  const token = microphoneTestGeneration
  audioState.microphoneTestStatus = 'requesting'

  try {
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error(translate('runtime.microphoneHTTPSRequired'))
    }
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: selectedAudioInputConstraints(),
      video: false
    })
    if (token !== microphoneTestGeneration) {
      for (const track of stream.getTracks()) track.stop()
      return
    }

    microphoneTestStream = stream
    const context = new AudioContext()
    microphoneTestContext = context
    await context.resume()
    const analyser = context.createAnalyser()
    analyser.fftSize = 256
    const gain = context.createGain()
    gain.gain.value = audioState.microphoneGain / 100
    context.createMediaStreamSource(stream).connect(gain).connect(analyser)

    microphoneTestGain = gain
    microphoneTestAnalyser = analyser
    audioState.microphoneTestStatus = 'active'
    audioState.microphoneTestSeconds = MICROPHONE_TEST_LIMIT_MS / 1000
    audioState.microphoneTestError = ''

    const samples = new Uint8Array(analyser.fftSize)
    microphoneSampleTimer = window.setInterval(() => {
      if (!microphoneTestAnalyser) return
      microphoneTestAnalyser.getByteTimeDomainData(samples)
      let sum = 0
      for (const sample of samples) {
        const normalized = (sample - 128) / 128
        sum += normalized * normalized
      }
      audioState.microphoneTestLevel = Math.min(1, Math.sqrt(sum / samples.length) * 4)
    }, MICROPHONE_SAMPLE_MS)
    microphoneCountdownTimer = window.setInterval(() => {
      audioState.microphoneTestSeconds = Math.max(
        0,
        audioState.microphoneTestSeconds - 1
      )
    }, 1000)
    microphoneLimitTimer = window.setTimeout(() => {
      stopMicrophoneTest()
    }, MICROPHONE_TEST_LIMIT_MS)

    void refreshAudioDevices()
  } catch (error) {
    if (token !== microphoneTestGeneration) return
    releaseMicrophoneTestResources()
    audioState.microphoneTestStatus = 'error'
    audioState.microphoneTestLevel = 0
    audioState.microphoneTestSeconds = 0
    audioState.microphoneTestError = microphoneTestError(error)
  }
}
