import { reactive } from 'vue'

export type AudioDeviceLoadStatus = 'idle' | 'loading' | 'ready' | 'error'
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
}

const INPUT_STORAGE_KEY = 'modemdeck.audio.input-device'
const OUTPUT_STORAGE_KEY = 'modemdeck.audio.output-device'
const MICROPHONE_TEST_LIMIT_MS = 30_000
const MICROPHONE_SAMPLE_MS = 80

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

const selectedInputID = storedDeviceID(INPUT_STORAGE_KEY)
const selectedOutputID = storedDeviceID(OUTPUT_STORAGE_KEY)
const outputSelectionSupported = supportsOutputSelection()

export const audioState = reactive<AudioState>({
  inputs: [],
  outputs: [],
  devicesStatus: 'idle',
  devicesError: '',
  devicesRevision: 0,
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
      ? '当前浏览器不支持指定音频输出设备'
      : '',
  microphoneTestStatus: 'idle',
  microphoneTestLevel: 0,
  microphoneTestSeconds: 0,
  microphoneTestError: ''
})

let deviceListenerActive = false
let refreshPromise: Promise<void> | undefined
let outputApplyGeneration = 0
let microphoneTestGeneration = 0
let microphoneTestStream: MediaStream | undefined
let microphoneTestContext: AudioContext | undefined
let microphoneTestAnalyser: AnalyserNode | undefined
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
    audioState.outputRoutingError = '当前浏览器不支持指定音频输出设备'
    return
  }
  if (selectedDeviceMissing('output')) {
    audioState.outputRoutingStatus = 'unavailable'
    audioState.outputRoutingError = '所选音频输出设备当前不可用'
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
    audioState.inputRoutingError = '所选麦克风当前不可用'
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
      audioState.devicesError = '当前浏览器无法枚举音频设备'
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
      updateInputSelectionStatus()
      updateOutputSelectionStatus()
    } catch (error) {
      audioState.devicesStatus = 'error'
      audioState.devicesError =
        error instanceof Error ? error.message : '无法读取音频设备'
    }
  })().finally(() => {
    refreshPromise = undefined
  })

  return refreshPromise
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
        error instanceof Error ? error.message : '无法恢复系统默认音频输出'
      return false
    }
  }

  if (typeof sinkElement.setSinkId !== 'function') {
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'unsupported'
    audioState.outputRoutingError = '当前浏览器不支持指定音频输出设备'
    return false
  }
  if (selectedDeviceMissing('output')) {
    if (token !== outputApplyGeneration) return false
    audioState.outputRoutingStatus = 'unavailable'
    audioState.outputRoutingError = '所选音频输出设备当前不可用'
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
      error instanceof Error ? error.message : '无法使用所选音频输出设备'
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
    if (error.name === 'NotAllowedError') return '未授权浏览器使用麦克风'
    if (error.name === 'NotFoundError' || error.name === 'OverconstrainedError') {
      return audioState.selectedInputID
        ? '所选麦克风当前不可用'
        : '未找到可用麦克风'
    }
    if (error.name === 'NotReadableError') return '麦克风无法读取，可能正被其他应用占用'
  }
  return error instanceof Error ? error.message : '麦克风测试失败'
}

export async function startMicrophoneTest(): Promise<void> {
  stopMicrophoneTest()
  const token = microphoneTestGeneration
  audioState.microphoneTestStatus = 'requesting'

  try {
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error('浏览器需要通过 HTTPS 才能使用麦克风')
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
    context.createMediaStreamSource(stream).connect(analyser)

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
