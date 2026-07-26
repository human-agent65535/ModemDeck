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

export const waitingToneSource = new URL(
  '../assets/tones/waiting.ogg',
  import.meta.url
).href

export type AudibleRingtoneID = (typeof ringtoneCatalog)[number]['id']
export type RingtoneID = AudibleRingtoneID
export type AudibleNotificationID = (typeof notificationCatalog)[number]['id']
export type MessageSoundID = AudibleNotificationID
export type SoundPreview =
  | `ringtone:${AudibleRingtoneID}`
  | `incoming-message:${AudibleNotificationID}`
  | `outgoing-message:${AudibleNotificationID}`
  | 'waiting'
export type CallSoundMode = 'idle' | 'incoming' | 'waiting'

type BrowserSoundPreferences = {
  ringtone: RingtoneID
  ringtoneEnabled: boolean
  incomingMessage: MessageSoundID
  incomingMessageEnabled: boolean
  outgoingMessage: MessageSoundID
  outgoingMessageEnabled: boolean
  waiting: boolean
}

const SOUND_STORAGE_KEY = 'modemdeck.audio.sound-preferences.v1'
const MESSAGE_SOUND_HISTORY_LIMIT = 256
const RINGTONE_BASE_VOLUME = 0.82
const WAITING_BASE_VOLUME = 0.55
const INCOMING_MESSAGE_BASE_VOLUME = 0.72
const OUTGOING_MESSAGE_BASE_VOLUME = 0.5
const VALID_RINGTONES = new Set<RingtoneID>(
  ringtoneCatalog.map(ringtone => ringtone.id)
)
const VALID_NOTIFICATIONS = new Set<MessageSoundID>(
  notificationCatalog.map(notification => notification.id)
)

const DEFAULT_PREFERENCES: BrowserSoundPreferences = {
  ringtone: 'orion',
  ringtoneEnabled: true,
  incomingMessage: 'pixie-dust',
  incomingMessageEnabled: true,
  outgoingMessage: 'pizzicato',
  outgoingMessageEnabled: true,
  waiting: true
}

const ringtoneSources = Object.fromEntries(
  ringtoneCatalog.map(ringtone => [ringtone.id, ringtone.source])
) as Record<AudibleRingtoneID, string>
const notificationSources = Object.fromEntries(
  notificationCatalog.map(notification => [notification.id, notification.source])
) as Record<AudibleNotificationID, string>

export const browserSoundState = reactive<{
  ringtone: RingtoneID
  ringtoneEnabled: boolean
  incomingMessage: MessageSoundID
  incomingMessageEnabled: boolean
  outgoingMessage: MessageSoundID
  outgoingMessageEnabled: boolean
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
let audioWatchStop: WatchStopHandle | undefined
let callAudio: HTMLAudioElement | undefined
let effectAudio: HTMLAudioElement | undefined
let effectBaseVolume = 0
let previewAudio: HTMLAudioElement | undefined
let currentSession: CallSession | null = null
let activeCallPlaybackKey = ''
let callPlaybackGeneration = 0
let previewGeneration = 0
const playedIncomingMessageIDs = new Set<string>()

function storedPreferences(): BrowserSoundPreferences {
  if (typeof window === 'undefined') return { ...DEFAULT_PREFERENCES }
  try {
    const raw = window.localStorage.getItem(SOUND_STORAGE_KEY)
    if (!raw) return { ...DEFAULT_PREFERENCES }
    return normalizeBrowserSoundPreferences(JSON.parse(raw))
  } catch {
    return { ...DEFAULT_PREFERENCES }
  }
}

function storedAudibleMessageSound(
  value: unknown,
  fallback: MessageSoundID
): MessageSoundID {
  if (typeof value === 'string' && VALID_NOTIFICATIONS.has(value as MessageSoundID)) {
    return value as MessageSoundID
  }
  return fallback
}

function storedEnabled(
  explicit: unknown,
  legacyValue: unknown,
  fallback: boolean
): boolean {
  if (typeof explicit === 'boolean') return explicit
  if (legacyValue === 'silent') return false
  if (typeof legacyValue === 'boolean') return legacyValue
  return fallback
}

export function normalizeBrowserSoundPreferences(
  raw: unknown
): BrowserSoundPreferences {
  if (!raw || typeof raw !== 'object') return { ...DEFAULT_PREFERENCES }
  const value = raw as Record<string, unknown>
  return {
    ringtone:
      typeof value.ringtone === 'string' &&
      VALID_RINGTONES.has(value.ringtone as RingtoneID)
        ? (value.ringtone as RingtoneID)
        : DEFAULT_PREFERENCES.ringtone,
    ringtoneEnabled: storedEnabled(
      value.ringtoneEnabled,
      value.ringtone,
      DEFAULT_PREFERENCES.ringtoneEnabled
    ),
    incomingMessage: storedAudibleMessageSound(
      value.incomingMessage,
      DEFAULT_PREFERENCES.incomingMessage
    ),
    incomingMessageEnabled: storedEnabled(
      value.incomingMessageEnabled,
      value.incomingMessage,
      DEFAULT_PREFERENCES.incomingMessageEnabled
    ),
    outgoingMessage: storedAudibleMessageSound(
      value.outgoingMessage,
      DEFAULT_PREFERENCES.outgoingMessage
    ),
    outgoingMessageEnabled: storedEnabled(
      value.outgoingMessageEnabled,
      value.outgoingMessage,
      DEFAULT_PREFERENCES.outgoingMessageEnabled
    ),
    waiting:
      typeof value.waiting === 'boolean'
        ? value.waiting
        : DEFAULT_PREFERENCES.waiting
  }
}

function applyPreferences(preferences: BrowserSoundPreferences): void {
  browserSoundState.ringtone = preferences.ringtone
  browserSoundState.ringtoneEnabled = preferences.ringtoneEnabled
  browserSoundState.incomingMessage = preferences.incomingMessage
  browserSoundState.incomingMessageEnabled = preferences.incomingMessageEnabled
  browserSoundState.outgoingMessage = preferences.outgoingMessage
  browserSoundState.outgoingMessageEnabled = preferences.outgoingMessageEnabled
  browserSoundState.waiting = preferences.waiting
}

function persistPreferences(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(
      SOUND_STORAGE_KEY,
      JSON.stringify({
        ringtone: browserSoundState.ringtone,
        ringtoneEnabled: browserSoundState.ringtoneEnabled,
        incomingMessage: browserSoundState.incomingMessage,
        incomingMessageEnabled: browserSoundState.incomingMessageEnabled,
        outgoingMessage: browserSoundState.outgoingMessage,
        outgoingMessageEnabled: browserSoundState.outgoingMessageEnabled,
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
    if (!browserSoundState.ringtoneEnabled) return null
    return {
      source: ringtoneSources[browserSoundState.ringtone],
      volume: RINGTONE_BASE_VOLUME * (audioState.ringAlertsVolume / 100)
    }
  }
  if (mode === 'waiting' && browserSoundState.waiting) {
    return {
      source: waitingToneSource,
      volume: WAITING_BASE_VOLUME * (audioState.callVolume / 100)
    }
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
  baseVolume: number
): Promise<void> {
  effectAudio ||= createAudioElement()
  if (!effectAudio) return
  effectBaseVolume = baseVolume
  try {
    await playAudio(effectAudio, notificationSources[sound], {
      loop: false,
      volume: baseVolume * (audioState.ringAlertsVolume / 100),
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
  const firstDelivery = claimIncomingMessageSound(messageID)
  if (!firstDelivery || !browserSoundState.incomingMessageEnabled) return
  void playEffect(browserSoundState.incomingMessage, INCOMING_MESSAGE_BASE_VOLUME)
}

export function playOutgoingMessageSound(): void {
  if (!browserSoundState.outgoingMessageEnabled) return
  void playEffect(browserSoundState.outgoingMessage, OUTGOING_MESSAGE_BASE_VOLUME)
}

function previewSource(preview: SoundPreview): { source: string; volume: number } {
  if (preview.startsWith('ringtone:')) {
    const ringtone = preview.slice('ringtone:'.length) as AudibleRingtoneID
    return {
      source: ringtoneSources[ringtone],
      volume: RINGTONE_BASE_VOLUME * (audioState.ringAlertsVolume / 100)
    }
  }
  if (preview === 'waiting') {
    return {
      source: waitingToneSource,
      volume: WAITING_BASE_VOLUME * (audioState.callVolume / 100)
    }
  }
  const [channel, sound] = preview.split(':') as [
    'incoming-message' | 'outgoing-message',
    AudibleNotificationID
  ]
  return {
    source: notificationSources[sound],
    volume:
      (channel === 'incoming-message'
        ? INCOMING_MESSAGE_BASE_VOLUME
        : OUTGOING_MESSAGE_BASE_VOLUME) *
      (audioState.ringAlertsVolume / 100)
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

export function setRingtoneEnabled(enabled: boolean): void {
  browserSoundState.ringtoneEnabled = enabled
  persistPreferences()
  activeCallPlaybackKey = ''
  syncCallSounds(currentSession)
}

export function setIncomingMessageSound(sound: MessageSoundID): void {
  if (!VALID_NOTIFICATIONS.has(sound)) return
  browserSoundState.incomingMessage = sound
  persistPreferences()
}

export function setIncomingMessageSoundEnabled(enabled: boolean): void {
  browserSoundState.incomingMessageEnabled = enabled
  persistPreferences()
}

export function setOutgoingMessageSound(sound: MessageSoundID): void {
  if (!VALID_NOTIFICATIONS.has(sound)) return
  browserSoundState.outgoingMessage = sound
  persistPreferences()
}

export function setOutgoingMessageSoundEnabled(enabled: boolean): void {
  browserSoundState.outgoingMessageEnabled = enabled
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

function applyActiveAudioLevels(): void {
  if (callAudio) {
    const source = callPlaybackSource(callSoundMode(currentSession))
    if (source) callAudio.volume = source.volume
  }
  if (effectAudio && effectBaseVolume > 0) {
    effectAudio.volume = effectBaseVolume * (audioState.ringAlertsVolume / 100)
  }
  if (previewAudio && browserSoundState.preview) {
    previewAudio.volume = previewSource(browserSoundState.preview).volume
  }
}

export function initializeBrowserSounds(): void {
  if (initialized) return
  initialized = true
  applyPreferences(storedPreferences())
  if (typeof window !== 'undefined') {
    window.addEventListener('storage', onPreferenceStorage)
  }
  audioWatchStop = watch(
    () =>
      [
        audioState.selectedOutputID,
        audioState.callVolume,
        audioState.ringAlertsVolume
      ] as const,
    () => {
      applyActiveAudioLevels()
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
  effectBaseVolume = 0
  previewAudio = undefined
  audioWatchStop?.()
  audioWatchStop = undefined
  if (typeof window !== 'undefined') {
    window.removeEventListener('storage', onPreferenceStorage)
  }
  playedIncomingMessageIDs.clear()
  browserSoundState.playbackBlocked = false
  browserSoundState.error = ''
}
