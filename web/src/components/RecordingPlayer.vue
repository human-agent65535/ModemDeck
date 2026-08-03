<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Pause, Play } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  applySelectedAudioOutput,
  audioState,
  cancelSelectedAudioOutputApplication
} from '../state/audio'

const props = withDefaults(
  defineProps<{
    src: string
    label?: string
    compact?: boolean
  }>(),
  {
    label: '',
    compact: false
  }
)

const { t } = useI18n()
const audio = ref<HTMLAudioElement>()
const currentTime = ref(0)
const duration = ref(0)
const playing = ref(false)
const playbackError = ref('')

const accessibleLabel = computed(() => props.label || t('recordings.playback'))
const volume = computed(() =>
  Math.min(1, Math.max(0, audioState.recordingPlaybackVolume / 100))
)
const timelineMaximum = computed(() => Math.max(1, duration.value))
const timelineValue = computed(() =>
  Math.min(timelineMaximum.value, Math.max(0, currentTime.value))
)

function playbackTime(seconds: number): string {
  const normalized = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0
  const hours = Math.floor(normalized / 3600)
  const minutes = Math.floor((normalized % 3600) / 60)
  const remainder = normalized % 60
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(remainder).padStart(2, '0')}`
  }
  return `${minutes}:${String(remainder).padStart(2, '0')}`
}

function syncTimeline(element: HTMLAudioElement): void {
  currentTime.value = Number.isFinite(element.currentTime) ? element.currentTime : 0
  duration.value = Number.isFinite(element.duration) ? element.duration : 0
}

function onMetadata(event: Event): void {
  playbackError.value = ''
  syncTimeline(event.currentTarget as HTMLAudioElement)
}

function onTimeUpdate(event: Event): void {
  syncTimeline(event.currentTarget as HTMLAudioElement)
}

function onPlay(): void {
  playing.value = true
  playbackError.value = ''
}

function onPause(event: Event): void {
  playing.value = false
  syncTimeline(event.currentTarget as HTMLAudioElement)
}

function onEnded(event: Event): void {
  playing.value = false
  syncTimeline(event.currentTarget as HTMLAudioElement)
}

function onError(): void {
  playing.value = false
  playbackError.value = t('audio.playbackFailed')
}

function resetPlayback(): void {
  playing.value = false
  currentTime.value = 0
  duration.value = 0
  playbackError.value = ''
}

function seek(event: Event): void {
  const element = audio.value
  if (!element || duration.value <= 0) return
  const nextTime = (event.currentTarget as HTMLInputElement).valueAsNumber
  if (!Number.isFinite(nextTime)) return
  element.currentTime = Math.min(duration.value, Math.max(0, nextTime))
  currentTime.value = element.currentTime
}

async function togglePlayback(): Promise<void> {
  const element = audio.value
  if (!element) return
  if (!element.paused) {
    element.pause()
    return
  }

  playbackError.value = ''
  element.volume = volume.value
  if (!(await applySelectedAudioOutput(element))) {
    playbackError.value =
      audioState.outputRoutingError || t('audio.playbackFailed')
    return
  }
  try {
    await element.play()
  } catch (error) {
    playbackError.value =
      error instanceof Error ? error.message : t('audio.playbackFailed')
  }
}

watch(volume, nextVolume => {
  if (audio.value) audio.value.volume = nextVolume
})

watch(
  () => audioState.selectedOutputID,
  () => {
    const element = audio.value
    if (element && !element.paused) void applySelectedAudioOutput(element)
  }
)

watch(() => props.src, resetPlayback)

onBeforeUnmount(() => {
  const element = audio.value
  if (!element) return
  element.pause()
  cancelSelectedAudioOutputApplication(element)
})
</script>

<template>
  <div
    class="recording-audio-player"
    :class="{ 'recording-audio-player--compact': compact }"
    role="group"
    :aria-label="accessibleLabel"
  >
    <audio
      ref="audio"
      class="recording-audio-player__native"
      :src="src"
      :volume="volume"
      preload="metadata"
      @durationchange="onMetadata"
      @loadedmetadata="onMetadata"
      @timeupdate="onTimeUpdate"
      @play="onPlay"
      @pause="onPause"
      @ended="onEnded"
      @emptied="resetPlayback"
      @error="onError"
    />

    <button
      class="recording-audio-player__toggle"
      type="button"
      :aria-label="playing ? t('recordings.pause') : t('recordings.play')"
      :title="playing ? t('recordings.pause') : t('recordings.play')"
      @click="togglePlayback"
    >
      <Pause v-if="playing" :size="compact ? 15 : 17" fill="currentColor" />
      <Play v-else :size="compact ? 15 : 17" fill="currentColor" />
    </button>

    <span class="recording-audio-player__progress">
      <span class="recording-audio-player__timeline">
        <progress
          :value="timelineValue"
          :max="timelineMaximum"
          aria-hidden="true"
        />
        <input
          type="range"
          min="0"
          :max="timelineMaximum"
          step="0.1"
          :value="timelineValue"
          :disabled="duration <= 0"
          :aria-label="t('recordings.seek')"
          :aria-valuetext="playbackTime(currentTime)"
          @input="seek"
        />
      </span>
      <span class="recording-audio-player__time" aria-hidden="true">
        <span>{{ playbackTime(currentTime) }}</span>
        <span>{{ playbackTime(duration) }}</span>
      </span>
    </span>

    <span
      v-if="playbackError"
      class="recording-audio-player__error"
      role="alert"
    >
      {{ playbackError }}
    </span>
  </div>
</template>

<style scoped>
.recording-audio-player {
  display: grid;
  width: 100%;
  min-width: 0;
  grid-template-columns: 40px minmax(0, 1fr);
  align-items: center;
  gap: 12px;
}

.recording-audio-player__native {
  display: none;
}

.recording-audio-player__toggle {
  display: inline-grid;
  width: 40px;
  height: 40px;
  place-items: center;
  color: var(--on-accent);
  background: var(--accent);
  border-radius: 50%;
  transition:
    background-color var(--motion-base) var(--ease-standard),
    transform var(--motion-fast) var(--ease-standard);
}

.recording-audio-player__toggle:hover {
  background: var(--accent-strong);
}

.recording-audio-player__toggle:active {
  transform: scale(0.96);
}

.recording-audio-player__toggle:focus-visible,
.recording-audio-player__timeline input:focus-visible {
  outline: 3px solid var(--accent-soft);
  outline-offset: 2px;
}

.recording-audio-player__progress {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.recording-audio-player__timeline {
  position: relative;
  display: block;
  height: 18px;
}

.recording-audio-player__timeline progress,
.recording-audio-player__timeline input {
  position: absolute;
  top: 50%;
  left: 0;
  width: 100%;
  margin: 0;
  transform: translateY(-50%);
}

.recording-audio-player__timeline progress {
  height: 4px;
  overflow: hidden;
  appearance: none;
  background: var(--control-muted);
  border: 0;
  border-radius: 999px;
}

.recording-audio-player__timeline progress::-webkit-progress-bar {
  background: var(--control-muted);
  border-radius: 999px;
}

.recording-audio-player__timeline progress::-webkit-progress-value {
  background: var(--accent-strong);
  border-radius: 999px;
}

.recording-audio-player__timeline progress::-moz-progress-bar {
  background: var(--accent-strong);
  border-radius: 999px;
}

.recording-audio-player__timeline input {
  height: 18px;
  appearance: none;
  cursor: pointer;
  background: transparent;
  border-radius: 999px;
}

.recording-audio-player__timeline input:disabled {
  cursor: default;
}

.recording-audio-player__timeline input::-webkit-slider-runnable-track {
  height: 4px;
  background: transparent;
}

.recording-audio-player__timeline input::-webkit-slider-thumb {
  width: 12px;
  height: 12px;
  margin-top: -4px;
  appearance: none;
  background: var(--surface);
  border: 2px solid var(--accent-strong);
  border-radius: 50%;
}

.recording-audio-player__timeline input::-moz-range-track {
  height: 4px;
  background: transparent;
}

.recording-audio-player__timeline input::-moz-range-thumb {
  width: 9px;
  height: 9px;
  background: var(--surface);
  border: 2px solid var(--accent-strong);
  border-radius: 50%;
}

.recording-audio-player__time {
  display: flex;
  min-width: 0;
  justify-content: space-between;
  color: var(--muted);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
}

.recording-audio-player__error {
  grid-column: 1 / -1;
  color: var(--danger);
  font-size: 10px;
}

.recording-audio-player--compact {
  grid-template-columns: 32px minmax(0, 1fr);
  gap: 8px;
}

.recording-audio-player--compact .recording-audio-player__toggle {
  width: 32px;
  height: 32px;
}

@media (prefers-reduced-motion: reduce) {
  .recording-audio-player__toggle {
    transition: none;
  }
}
</style>
