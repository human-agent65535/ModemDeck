import { reactive, watch, type WatchStopHandle } from 'vue'
import type { CallSession } from '../api/types'
import { translate } from '../i18n'
import { applySelectedAudioOutput, audioState } from './audio'

export const ringtoneCatalog = [
  {
    id: 'orion',
    name: 'Orion',
    source: new URL('../assets/ringtones/orion.ogg', import.meta.url).href
  },
  {
    id: 'classic',
    name: 'Classic',
    source: new URL('../assets/ringtones/classic.ogg', import.meta.url).href
  },
  {
    id: 'digital',
    name: 'Digital',
    source: new URL('../assets/ringtones/digital.ogg', import.meta.url).href
  },
  {
    id: 'chime',
    name: 'Chime',
    source: new URL('../assets/ringtones/chime.ogg', import.meta.url).href
  },
  {
    id: 'soft',
    name: 'Soft',
    source: new URL('../assets/ringtones/soft.ogg', import.meta.url).href
  },
  {
    id: 'andromeda',
    name: 'Andromeda',
    source: new URL('../assets/ringtones/andromeda.ogg', import.meta.url).href
  },
  {
    id: 'aquila',
    name: 'Aquila',
    source: new URL('../assets/ringtones/aquila.ogg', import.meta.url).href
  },
  {
    id: 'canis-major',
    name: 'Canis Major',
    source: new URL('../assets/ringtones/canis-major.ogg', import.meta.url).href
  },
  {
    id: 'carina',
    name: 'Carina',
    source: new URL('../assets/ringtones/carina.ogg', import.meta.url).href
  },
  {
    id: 'centaurus',
    name: 'Centaurus',
    source: new URL('../assets/ringtones/centaurus.ogg', import.meta.url).href
  },
  {
    id: 'cygnus',
    name: 'Cygnus',
    source: new URL('../assets/ringtones/cygnus.ogg', import.meta.url).href
  },
  {
    id: 'draco',
    name: 'Draco',
    source: new URL('../assets/ringtones/draco.ogg', import.meta.url).href
  },
  {
    id: 'hydra',
    name: 'Hydra',
    source: new URL('../assets/ringtones/hydra.ogg', import.meta.url).href
  },
  {
    id: 'machina',
    name: 'Machina',
    source: new URL('../assets/ringtones/machina.ogg', import.meta.url).href
  },
  {
    id: 'pegasus',
    name: 'Pegasus',
    source: new URL('../assets/ringtones/pegasus.ogg', import.meta.url).href
  },
  {
    id: 'perseus',
    name: 'Perseus',
    source: new URL('../assets/ringtones/perseus.ogg', import.meta.url).href
  },
  {
    id: 'rigel',
    name: 'Rigel',
    source: new URL('../assets/ringtones/rigel.ogg', import.meta.url).href
  },
  {
    id: 'sceptrum',
    name: 'Sceptrum',
    source: new URL('../assets/ringtones/sceptrum.ogg', import.meta.url).href
  },
  {
    id: 'solarium',
    name: 'Solarium',
    source: new URL('../assets/ringtones/solarium.ogg', import.meta.url).href
  },
  {
    id: 'ursa-minor',
    name: 'Ursa Minor',
    source: new URL('../assets/ringtones/ursa-minor.ogg', import.meta.url).href
  }
] as const

export const notificationCatalog = [
  {
    id: 'pixie-dust',
    name: 'Pixie Dust',
    source: new URL('../assets/notifications/pixie-dust.ogg', import.meta.url).href
  },
  {
    id: 'pizzicato',
    name: 'Pizzicato',
    source: new URL('../assets/notifications/pizzicato.ogg', import.meta.url).href
  },
  {
    id: 'tinkerbell',
    name: 'Tinkerbell',
    source: new URL('../assets/notifications/tinkerbell.ogg', import.meta.url).href
  },
  {
    id: 'drip',
    name: 'Drip',
    source: new URL('../assets/notifications/drip.ogg', import.meta.url).href
  },
  {
    id: 'heaven',
    name: 'Heaven',
    source: new URL('../assets/notifications/heaven.ogg', import.meta.url).href
  },
  {
    id: 'ta-da',
    name: 'Ta Da',
    source: new URL('../assets/notifications/ta-da.ogg', import.meta.url).href
  },
  {
    id: 'caffeine-snake',
    name: 'Caffeine Snake',
    source: new URL('../assets/notifications/caffeine-snake.ogg', import.meta.url).href
  },
  {
    id: 'dear-deer',
    name: 'Dear Deer',
    source: new URL('../assets/notifications/dear-deer.ogg', import.meta.url).href
  },
  {
    id: 'on-the-hunt',
    name: 'On The Hunt',
    source: new URL('../assets/notifications/on-the-hunt.ogg', import.meta.url).href
  },
  {
    id: 'voila',
    name: 'Voila',
    source: new URL('../assets/notifications/voila.ogg', import.meta.url).href
  }
] as const

export type AudibleRingtoneID = (typeof ringtoneCatalog)[number]['id']
export type RingtoneID = AudibleRingtoneID | 'silent'
export type AudibleNotificationID = (typeof notificationCatalog)[number]['id']
export type MessageSoundID = AudibleNotificationID | 'silent'
export type SoundPreview =
  | `ringtone:${AudibleRingtoneID}`
  | `incoming-message:${AudibleNotificationID}`
  | `outgoing-message:${AudibleNotificationID}`
  | 'waiting'
export type CallSoundMode = 'idle' | 'incoming' | 'waiting'

type BrowserSoundPreferences = {
  ringtone: RingtoneID
  incomingMessage: MessageSoundID
  outgoingMessage: MessageSoundID
  waiting: boolean
}

type GeneratedToneID = 'waiting'

type ToneSegment = {
  startMilliseconds: number
  durationMilliseconds: number
  frequency: number
  amplitude: number
}

const SOUND_STORAGE_KEY = 'modemdeck.audio.sound-preferences.v1'
const MESSAGE_SOUND_HISTORY_LIMIT = 256
const SAMPLE_RATE = 16_000
const VALID_RINGTONES = new Set<RingtoneID>([
  ...ringtoneCatalog.map(ringtone => ringtone.id),
  'silent'
])
const VALID_NOTIFICATIONS = new Set<MessageSoundID>([
  ...notificationCatalog.map(notification => notification.id),
  'silent'
])

const DEFAULT_PREFERENCES: BrowserSoundPreferences = {
  ringtone: 'orion',
  incomingMessage: 'pixie-dust',
  outgoingMessage: 'pizzicato',
  waiting: true
}

const ringtoneSources = Object.fromEntries(
  ringtoneCatalog.map(ringtone => [ringtone.id, ringtone.source])
) as Record<AudibleRingtoneID, string>
const notificationSources = Object.fromEntries(
  notificationCatalog.map(notification => [notification.id, notification.source])
) as Record<AudibleNotificationID, string>

const generatedToneDefinitions: Record<
  GeneratedToneID,
  { durationMilliseconds: number; segments: ToneSegment[] }
> = {
  waiting: {
    durationMilliseconds: 3000,
    segments: [
      {
        startMilliseconds: 0,
        durationMilliseconds: 1000,
        frequency: 425,
        amplitude: 0.22
      }
    ]
  }
}

export const browserSoundState = reactive<{
  ringtone: RingtoneID
  incomingMessage: MessageSoundID
  outgoingMessage: MessageSoundID
  waiting: boolean
  preview: SoundPreview | ''
  playbackBlocked: boolean
  error: string
}>({
  ...DEFAULT_PREFERENCES,
  preview: '',
  playbackBlocked: false,
  error: ''
})

let initialized = false
let outputWatchStop: WatchStopHandle | undefined
let callAudio: HTMLAudioElement | undefined
let effectAudio: HTMLAudioElement | undefined
let previewAudio: HTMLAudioElement | undefined
let currentSession: CallSession | null = null
let activeCallPlaybackKey = ''
let callPlaybackGeneration = 0
let previewGeneration = 0
const generatedToneURLs = new Map<GeneratedToneID, string>()
const playedIncomingMessageIDs = new Set<string>()

function storedPreferences(): BrowserSoundPreferences {
  if (typeof window === 'undefined') return { ...DEFAULT_PREFERENCES }
  try {
    const raw = window.localStorage.getItem(SOUND_STORAGE_KEY)
    if (!raw) return { ...DEFAULT_PREFERENCES }
    const value = JSON.parse(raw) as Record<string, unknown>
    return {
      ringtone:
        typeof value.ringtone === 'string' &&
        VALID_RINGTONES.has(value.ringtone as RingtoneID)
          ? (value.ringtone as RingtoneID)
          : DEFAULT_PREFERENCES.ringtone,
      incomingMessage: storedMessageSound(
        value.incomingMessage,
        DEFAULT_PREFERENCES.incomingMessage
      ),
      outgoingMessage: storedMessageSound(
        value.outgoingMessage,
        DEFAULT_PREFERENCES.outgoingMessage
      ),
      waiting:
        typeof value.waiting === 'boolean'
          ? value.waiting
          : DEFAULT_PREFERENCES.waiting
    }
  } catch {
    return { ...DEFAULT_PREFERENCES }
  }
}

function storedMessageSound(
  value: unknown,
  fallback: MessageSoundID
): MessageSoundID {
  if (typeof value === 'string' && VALID_NOTIFICATIONS.has(value as MessageSoundID)) {
    return value as MessageSoundID
  }
  if (typeof value === 'boolean') return value ? fallback : 'silent'
  return fallback
}

function applyPreferences(preferences: BrowserSoundPreferences): void {
  browserSoundState.ringtone = preferences.ringtone
  browserSoundState.incomingMessage = preferences.incomingMessage
  browserSoundState.outgoingMessage = preferences.outgoingMessage
  browserSoundState.waiting = preferences.waiting
}

function persistPreferences(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(
      SOUND_STORAGE_KEY,
      JSON.stringify({
        ringtone: browserSoundState.ringtone,
        incomingMessage: browserSoundState.incomingMessage,
        outgoingMessage: browserSoundState.outgoingMessage,
        waiting: browserSoundState.waiting
      } satisfies BrowserSoundPreferences)
    )
  } catch {
    // The in-memory preference still applies when browser storage is unavailable.
  }
}

function onPreferenceStorage(event: StorageEvent): void {
  if (event.key !== null && event.key !== SOUND_STORAGE_KEY) return
  applyPreferences(storedPreferences())
  activeCallPlaybackKey = ''
  syncCallSounds(currentSession)
}

function createAudioElement(): HTMLAudioElement | undefined {
  if (typeof document === 'undefined') return undefined
  const element = document.createElement('audio')
  element.preload = 'auto'
  element.setAttribute('playsinline', '')
  return element
}

function stopAudio(element: HTMLAudioElement | undefined): void {
  if (!element) return
  element.pause()
  try {
    element.currentTime = 0
  } catch {
    // Some browsers reject seeking before metadata is available.
  }
}

function writeASCII(view: DataView, offset: number, value: string): void {
  for (let index = 0; index < value.length; index += 1) {
    view.setUint8(offset + index, value.charCodeAt(index))
  }
}

function segmentSample(segment: ToneSegment, sampleIndex: number): number {
  const start = Math.round((segment.startMilliseconds / 1000) * SAMPLE_RATE)
  const length = Math.round((segment.durationMilliseconds / 1000) * SAMPLE_RATE)
  const relative = sampleIndex - start
  if (relative < 0 || relative >= length) return 0

  const fadeSamples = Math.min(Math.round(SAMPLE_RATE * 0.018), Math.floor(length / 2))
  const fadeIn = fadeSamples > 0 ? Math.min(1, relative / fadeSamples) : 1
  const fadeOut =
    fadeSamples > 0 ? Math.min(1, (length - relative - 1) / fadeSamples) : 1
  const envelope = Math.max(0, Math.min(fadeIn, fadeOut))
  return (
    Math.sin((2 * Math.PI * segment.frequency * relative) / SAMPLE_RATE) *
    segment.amplitude *
    envelope
  )
}

function generatedToneSource(id: GeneratedToneID): string {
  const existing = generatedToneURLs.get(id)
  if (existing) return existing

  const definition = generatedToneDefinitions[id]
  const sampleCount = Math.round(
    (definition.durationMilliseconds / 1000) * SAMPLE_RATE
  )
  const buffer = new ArrayBuffer(44 + sampleCount * 2)
  const view = new DataView(buffer)
  writeASCII(view, 0, 'RIFF')
  view.setUint32(4, 36 + sampleCount * 2, true)
  writeASCII(view, 8, 'WAVE')
  writeASCII(view, 12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, SAMPLE_RATE, true)
  view.setUint32(28, SAMPLE_RATE * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  writeASCII(view, 36, 'data')
  view.setUint32(40, sampleCount * 2, true)

  for (let sampleIndex = 0; sampleIndex < sampleCount; sampleIndex += 1) {
    const sample = definition.segments.reduce(
      (sum, segment) => sum + segmentSample(segment, sampleIndex),
      0
    )
    view.setInt16(
      44 + sampleIndex * 2,
      Math.round(Math.max(-1, Math.min(1, sample)) * 0x7fff),
      true
    )
  }

  const url = URL.createObjectURL(new Blob([buffer], { type: 'audio/wav' }))
  generatedToneURLs.set(id, url)
  return url
}

function setAudioSource(element: HTMLAudioElement, source: string): void {
  if (element.dataset.modemdeckSource === source) return
  element.dataset.modemdeckSource = source
  element.src = source
  element.load()
}

function playbackError(error: unknown): string {
  if (error instanceof DOMException && error.name === 'NotAllowedError') {
    return translate('audio.playbackBlocked')
  }
  return error instanceof Error ? error.message : translate('audio.playbackFailed')
}

async function playAudio(
  element: HTMLAudioElement,
  source: string,
  options: { loop: boolean; volume: number; restart: boolean }
): Promise<void> {
  setAudioSource(element, source)
  element.loop = options.loop
  element.volume = options.volume
  if (options.restart) {
    try {
      element.currentTime = 0
    } catch {
      // Playback can still start from the beginning after a new source is loaded.
    }
  }
  if (!(await applySelectedAudioOutput(element))) {
    throw new Error(audioState.outputRoutingError || translate('audio.playbackFailed'))
  }
  await element.play()
}

export function callSoundMode(session: CallSession | null): CallSoundMode {
  if (!session) return 'idle'
  if (session.direction === 'incoming' && session.phase === 'ringing') {
    return 'incoming'
  }
  if (
    session.direction === 'outgoing' &&
    (session.phase === 'unknown' ||
      session.phase === 'dialing' ||
      session.phase === 'ringing')
  ) {
    return 'waiting'
  }
  return 'idle'
}

function callPlaybackSource(mode: CallSoundMode): {
  source: string
  volume: number
} | null {
  if (mode === 'incoming') {
    if (browserSoundState.ringtone === 'silent') return null
    return {
      source: ringtoneSources[browserSoundState.ringtone],
      volume: 0.82
    }
  }
  if (mode === 'waiting' && browserSoundState.waiting) {
    return { source: generatedToneSource('waiting'), volume: 0.55 }
  }
  return null
}

export function syncCallSounds(session: CallSession | null): void {
  currentSession = session
  const mode = callSoundMode(session)
  const source = callPlaybackSource(mode)
  const key = source && session ? `${mode}:${session.id}:${source.source}` : ''
  if (key === activeCallPlaybackKey) return

  activeCallPlaybackKey = key
  const token = ++callPlaybackGeneration
  stopAudio(callAudio)

  if (!source) return
  callAudio ||= createAudioElement()
  if (!callAudio) return

  void playAudio(callAudio, source.source, {
    loop: true,
    volume: source.volume,
    restart: true
  }).then(
    () => {
      if (token !== callPlaybackGeneration) stopAudio(callAudio)
      else {
        browserSoundState.playbackBlocked = false
        browserSoundState.error = ''
      }
    },
    error => {
      if (token !== callPlaybackGeneration) return
      browserSoundState.playbackBlocked =
        error instanceof DOMException && error.name === 'NotAllowedError'
      browserSoundState.error = playbackError(error)
    }
  )
}

export function claimIncomingMessageSound(
  messageID: string,
  claimed = playedIncomingMessageIDs
): boolean {
  if (!messageID || claimed.has(messageID)) return false
  claimed.add(messageID)
  while (claimed.size > MESSAGE_SOUND_HISTORY_LIMIT) {
    const oldest = claimed.values().next().value
    if (!oldest) break
    claimed.delete(oldest)
  }
  return true
}

async function playEffect(
  sound: AudibleNotificationID,
  volume: number
): Promise<void> {
  effectAudio ||= createAudioElement()
  if (!effectAudio) return
  try {
    await playAudio(effectAudio, notificationSources[sound], {
      loop: false,
      volume,
      restart: true
    })
    browserSoundState.playbackBlocked = false
    browserSoundState.error = ''
  } catch (error) {
    browserSoundState.playbackBlocked =
      error instanceof DOMException && error.name === 'NotAllowedError'
    browserSoundState.error = playbackError(error)
  }
}

export function playIncomingMessageSound(messageID: string): void {
  if (
    browserSoundState.incomingMessage === 'silent' ||
    !claimIncomingMessageSound(messageID)
  ) {
    return
  }
  void playEffect(browserSoundState.incomingMessage, 0.72)
}

export function playOutgoingMessageSound(): void {
  if (browserSoundState.outgoingMessage === 'silent') return
  void playEffect(browserSoundState.outgoingMessage, 0.5)
}

function previewSource(preview: SoundPreview): { source: string; volume: number } {
  if (preview.startsWith('ringtone:')) {
    const ringtone = preview.slice('ringtone:'.length) as AudibleRingtoneID
    return { source: ringtoneSources[ringtone], volume: 0.82 }
  }
  if (preview === 'waiting') {
    return {
      source: generatedToneSource('waiting'),
      volume: 0.55
    }
  }
  const [channel, sound] = preview.split(':') as [
    'incoming-message' | 'outgoing-message',
    AudibleNotificationID
  ]
  return {
    source: notificationSources[sound],
    volume: channel === 'incoming-message' ? 0.72 : 0.5
  }
}

export function stopSoundPreview(): void {
  previewGeneration += 1
  stopAudio(previewAudio)
  browserSoundState.preview = ''
}

export function previewSound(preview: SoundPreview): void {
  if (browserSoundState.preview === preview) {
    stopSoundPreview()
    return
  }

  stopSoundPreview()
  previewAudio ||= createAudioElement()
  if (!previewAudio) return
  const token = previewGeneration
  const selected = previewSource(preview)
  browserSoundState.preview = preview
  previewAudio.onended = () => {
    if (token === previewGeneration) browserSoundState.preview = ''
  }

  void playAudio(previewAudio, selected.source, {
    loop: false,
    volume: selected.volume,
    restart: true
  }).then(
    () => {
      if (token !== previewGeneration) return
      browserSoundState.playbackBlocked = false
      browserSoundState.error = ''
    },
    error => {
      if (token !== previewGeneration) return
      browserSoundState.preview = ''
      browserSoundState.playbackBlocked =
        error instanceof DOMException && error.name === 'NotAllowedError'
      browserSoundState.error = playbackError(error)
    }
  )
}

export function setRingtone(ringtone: RingtoneID): void {
  if (!VALID_RINGTONES.has(ringtone)) return
  browserSoundState.ringtone = ringtone
  persistPreferences()
  activeCallPlaybackKey = ''
  syncCallSounds(currentSession)
}

export function setIncomingMessageSound(sound: MessageSoundID): void {
  if (!VALID_NOTIFICATIONS.has(sound)) return
  browserSoundState.incomingMessage = sound
  persistPreferences()
}

export function setOutgoingMessageSound(sound: MessageSoundID): void {
  if (!VALID_NOTIFICATIONS.has(sound)) return
  browserSoundState.outgoingMessage = sound
  persistPreferences()
}

export function setWaitingSound(enabled: boolean): void {
  browserSoundState.waiting = enabled
  persistPreferences()
  activeCallPlaybackKey = ''
  syncCallSounds(currentSession)
}

async function reroutePlayingAudio(): Promise<void> {
  for (const element of [callAudio, effectAudio, previewAudio]) {
    if (element && !element.paused) await applySelectedAudioOutput(element)
  }
}

export function initializeBrowserSounds(): void {
  if (initialized) return
  initialized = true
  applyPreferences(storedPreferences())
  if (typeof window !== 'undefined') {
    window.addEventListener('storage', onPreferenceStorage)
  }
  outputWatchStop = watch(
    () => audioState.selectedOutputID,
    () => {
      void reroutePlayingAudio()
    }
  )
}

export function shutdownBrowserSounds(): void {
  initialized = false
  currentSession = null
  activeCallPlaybackKey = ''
  callPlaybackGeneration += 1
  stopSoundPreview()
  stopAudio(callAudio)
  stopAudio(effectAudio)
  callAudio = undefined
  effectAudio = undefined
  previewAudio = undefined
  outputWatchStop?.()
  outputWatchStop = undefined
  if (typeof window !== 'undefined') {
    window.removeEventListener('storage', onPreferenceStorage)
  }
  for (const url of generatedToneURLs.values()) URL.revokeObjectURL(url)
  generatedToneURLs.clear()
  playedIncomingMessageIDs.clear()
  browserSoundState.playbackBlocked = false
  browserSoundState.error = ''
}
