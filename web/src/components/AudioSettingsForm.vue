<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import {
  AudioLines,
  BellRing,
  MessageSquareText,
  Mic,
  PhoneCall,
  PhoneOutgoing,
  Play,
  Send,
  Square,
  Volume2
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import AudioDeviceControls from './AudioDeviceControls.vue'
import {
  audioState,
  setCallVolume,
  setMicrophoneGain,
  setRecordingPlaybackVolume,
  setRingAlertsVolume
} from '../state/audio'
import {
  browserSoundState,
  notificationCatalog,
  previewSound,
  ringtoneCatalog,
  setIncomingMessageSound,
  setIncomingMessageSoundEnabled,
  setOutgoingMessageSound,
  setOutgoingMessageSoundEnabled,
  setRingtone,
  setRingtoneEnabled,
  setWaitingSound,
  stopSoundPreview,
  type MessageSoundID,
  type RingtoneID,
  type SoundPreview
} from '../state/browserSounds'
import SettingsControlRow from './settings/SettingsControlRow.vue'
import SettingsSection from './settings/SettingsSection.vue'

const { t } = useI18n()
const selectedRingtone = computed(
  () =>
    ringtoneCatalog.find(ringtone => ringtone.id === browserSoundState.ringtone)?.name ||
    ''
)
const selectedRingtonePreview = computed<SoundPreview>(
  () => `ringtone:${browserSoundState.ringtone}`
)
const selectedIncomingMessage = computed(
  () =>
    notificationCatalog.find(
      notification => notification.id === browserSoundState.incomingMessage
    )?.name || ''
)
const selectedOutgoingMessage = computed(
  () =>
    notificationCatalog.find(
      notification => notification.id === browserSoundState.outgoingMessage
    )?.name || ''
)
const selectedIncomingMessagePreview = computed<SoundPreview>(
  () => `incoming-message:${browserSoundState.incomingMessage}`
)
const selectedOutgoingMessagePreview = computed<SoundPreview>(
  () => `outgoing-message:${browserSoundState.outgoingMessage}`
)

function changeRingtone(event: Event): void {
  setRingtone((event.currentTarget as HTMLSelectElement).value as RingtoneID)
}

function togglePreview(preview: SoundPreview | ''): void {
  if (preview) previewSound(preview)
}

function changeIncomingMessage(event: Event): void {
  setIncomingMessageSound(
    (event.currentTarget as HTMLSelectElement).value as MessageSoundID
  )
}

function changeOutgoingMessage(event: Event): void {
  setOutgoingMessageSound(
    (event.currentTarget as HTMLSelectElement).value as MessageSoundID
  )
}

function changeWaiting(event: Event): void {
  setWaitingSound((event.currentTarget as HTMLInputElement).checked)
}

function changeRingtoneEnabled(event: Event): void {
  setRingtoneEnabled((event.currentTarget as HTMLInputElement).checked)
}

function changeIncomingMessageEnabled(event: Event): void {
  setIncomingMessageSoundEnabled((event.currentTarget as HTMLInputElement).checked)
}

function changeOutgoingMessageEnabled(event: Event): void {
  setOutgoingMessageSoundEnabled((event.currentTarget as HTMLInputElement).checked)
}

function inputNumber(event: Event): number {
  return Number((event.currentTarget as HTMLInputElement).value)
}

onBeforeUnmount(() => {
  stopSoundPreview()
})
</script>

<template>
  <div class="audio-preferences">
    <SettingsSection
      :title="t('audio.devices')"
      title-id="audio-devices-title"
      :description="t('audio.devicesDescription')"
      icon-tone="blue"
    >
      <template #icon><AudioLines :size="20" /></template>
      <AudioDeviceControls />
    </SettingsSection>

    <SettingsSection
      :title="t('audio.levels')"
      title-id="audio-levels-title"
      :description="t('audio.levelsDescription')"
      icon-tone="warning"
    >
      <template #icon><Volume2 :size="20" /></template>

      <SettingsControlRow
        as="label"
        :title="t('audio.microphoneGain')"
      >
        <template #icon><Mic :size="18" /></template>
        <span class="audio-level-control">
          <input
            type="range"
            min="0"
            max="200"
            step="5"
            :value="audioState.microphoneGain"
            :aria-label="t('audio.microphoneGain')"
            @input="setMicrophoneGain(inputNumber($event))"
          />
          <output>{{ audioState.microphoneGain }}%</output>
        </span>
      </SettingsControlRow>

      <SettingsControlRow as="label" :title="t('audio.callVolume')">
        <template #icon><PhoneCall :size="18" /></template>
        <span class="audio-level-control">
          <input
            type="range"
            min="0"
            max="100"
            step="5"
            :value="audioState.callVolume"
            :aria-label="t('audio.callVolume')"
            @input="setCallVolume(inputNumber($event))"
          />
          <output>{{ audioState.callVolume }}%</output>
        </span>
      </SettingsControlRow>

      <SettingsControlRow as="label" :title="t('audio.ringAlertsVolume')">
        <template #icon><BellRing :size="18" /></template>
        <span class="audio-level-control">
          <input
            type="range"
            min="0"
            max="100"
            step="5"
            :value="audioState.ringAlertsVolume"
            :aria-label="t('audio.ringAlertsVolume')"
            @input="setRingAlertsVolume(inputNumber($event))"
          />
          <output>{{ audioState.ringAlertsVolume }}%</output>
        </span>
      </SettingsControlRow>

      <SettingsControlRow
        as="label"
        :title="t('audio.recordingPlaybackVolume')"
      >
        <template #icon><AudioLines :size="18" /></template>
        <span class="audio-level-control">
          <input
            type="range"
            min="0"
            max="100"
            step="5"
            :value="audioState.recordingPlaybackVolume"
            :aria-label="t('audio.recordingPlaybackVolume')"
            @input="setRecordingPlaybackVolume(inputNumber($event))"
          />
          <output>{{ audioState.recordingPlaybackVolume }}%</output>
        </span>
      </SettingsControlRow>
    </SettingsSection>

    <SettingsSection
      :title="t('audio.callSounds')"
      title-id="call-sounds-title"
      :description="t('audio.callSoundsDescription')"
    >
      <template #icon><BellRing :size="20" /></template>

      <SettingsControlRow
        :title="t('audio.incomingRingtone')"
        :description="selectedRingtone"
      >
        <template #icon><BellRing :size="18" /></template>
        <span class="audio-preference-row__controls">
          <select
            :value="browserSoundState.ringtone"
            :aria-label="t('audio.incomingRingtone')"
            @change="changeRingtone"
          >
            <option
              v-for="ringtone in ringtoneCatalog"
              :key="ringtone.id"
              :value="ringtone.id"
            >
              {{ ringtone.name }}
            </option>
          </select>
          <button
            class="icon-button"
            type="button"
            :title="
              browserSoundState.preview === selectedRingtonePreview
                ? t('audio.stopPreview')
                : t('audio.preview')
            "
            :aria-label="
              browserSoundState.preview === selectedRingtonePreview
                ? t('audio.stopPreview')
                : t('audio.previewRingtone', { ringtone: selectedRingtone })
            "
            @click="togglePreview(selectedRingtonePreview)"
          >
            <Square
              v-if="browserSoundState.preview === selectedRingtonePreview"
              :size="16"
              fill="currentColor"
            />
            <Play v-else :size="17" fill="currentColor" />
          </button>
          <label class="compact-switch">
            <span class="sr-only">{{ t('audio.incomingRingtone') }}</span>
            <input
              type="checkbox"
              role="switch"
              :checked="browserSoundState.ringtoneEnabled"
              @change="changeRingtoneEnabled"
            />
          </label>
        </span>
      </SettingsControlRow>

      <SettingsControlRow
        :title="t('audio.waitingTone')"
        :description="t('audio.outgoingCall')"
      >
        <template #icon><PhoneOutgoing :size="18" /></template>
        <span class="audio-preference-row__controls">
          <button
            class="icon-button"
            type="button"
            :title="
              browserSoundState.preview === 'waiting'
                ? t('audio.stopPreview')
                : t('audio.preview')
            "
            :aria-label="
              browserSoundState.preview === 'waiting'
                ? t('audio.stopPreview')
                : t('audio.previewWaitingTone')
            "
            @click="togglePreview('waiting')"
          >
            <Square
              v-if="browserSoundState.preview === 'waiting'"
              :size="16"
              fill="currentColor"
            />
            <Play v-else :size="17" fill="currentColor" />
          </button>
          <label class="compact-switch">
            <span class="sr-only">{{ t('audio.waitingTone') }}</span>
            <input
              type="checkbox"
              role="switch"
              :checked="browserSoundState.waiting"
              @change="changeWaiting"
            />
          </label>
        </span>
      </SettingsControlRow>
    </SettingsSection>

    <SettingsSection
      :title="t('audio.messageSounds')"
      title-id="message-sounds-title"
      :description="t('audio.messageSoundsDescription')"
      icon-tone="warning"
    >
      <template #icon><MessageSquareText :size="20" /></template>

      <SettingsControlRow
        :title="t('audio.incomingMessage')"
        :description="selectedIncomingMessage"
      >
        <template #icon><MessageSquareText :size="18" /></template>
        <span class="audio-preference-row__controls">
          <select
            :value="browserSoundState.incomingMessage"
            :aria-label="t('audio.incomingMessage')"
            @change="changeIncomingMessage"
          >
            <option
              v-for="notification in notificationCatalog"
              :key="notification.id"
              :value="notification.id"
            >
              {{ notification.name }}
            </option>
          </select>
          <button
            class="icon-button"
            type="button"
            :title="
              browserSoundState.preview === selectedIncomingMessagePreview
                ? t('audio.stopPreview')
                : t('audio.preview')
            "
            :aria-label="
              browserSoundState.preview === selectedIncomingMessagePreview
                ? t('audio.stopPreview')
                : t('audio.previewIncomingMessage')
            "
            @click="togglePreview(selectedIncomingMessagePreview)"
          >
            <Square
              v-if="browserSoundState.preview === selectedIncomingMessagePreview"
              :size="16"
              fill="currentColor"
            />
            <Play v-else :size="17" fill="currentColor" />
          </button>
          <label class="compact-switch">
            <span class="sr-only">{{ t('audio.incomingMessage') }}</span>
            <input
              type="checkbox"
              role="switch"
              :checked="browserSoundState.incomingMessageEnabled"
              @change="changeIncomingMessageEnabled"
            />
          </label>
        </span>
      </SettingsControlRow>

      <SettingsControlRow
        :title="t('audio.outgoingMessage')"
        :description="selectedOutgoingMessage"
      >
        <template #icon><Send :size="18" /></template>
        <span class="audio-preference-row__controls">
          <select
            :value="browserSoundState.outgoingMessage"
            :aria-label="t('audio.outgoingMessage')"
            @change="changeOutgoingMessage"
          >
            <option
              v-for="notification in notificationCatalog"
              :key="notification.id"
              :value="notification.id"
            >
              {{ notification.name }}
            </option>
          </select>
          <button
            class="icon-button"
            type="button"
            :title="
              browserSoundState.preview === selectedOutgoingMessagePreview
                ? t('audio.stopPreview')
                : t('audio.preview')
            "
            :aria-label="
              browserSoundState.preview === selectedOutgoingMessagePreview
                ? t('audio.stopPreview')
                : t('audio.previewOutgoingMessage')
            "
            @click="togglePreview(selectedOutgoingMessagePreview)"
          >
            <Square
              v-if="browserSoundState.preview === selectedOutgoingMessagePreview"
              :size="16"
              fill="currentColor"
            />
            <Play v-else :size="17" fill="currentColor" />
          </button>
          <label class="compact-switch">
            <span class="sr-only">{{ t('audio.outgoingMessage') }}</span>
            <input
              type="checkbox"
              role="switch"
              :checked="browserSoundState.outgoingMessageEnabled"
              @change="changeOutgoingMessageEnabled"
            />
          </label>
        </span>
      </SettingsControlRow>

      <p v-if="browserSoundState.error" class="audio-preferences__error" role="alert">
        {{ browserSoundState.error }}
      </p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.audio-preferences {
  display: flex;
  max-width: 760px;
  flex-direction: column;
  gap: 34px;
}

.audio-preference-row__controls {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
}

.audio-level-control {
  display: grid;
  width: min(250px, 36vw);
  flex: 0 0 auto;
  grid-template-columns: minmax(0, 1fr) 48px;
  align-items: center;
  gap: 12px;
}

.audio-level-control input {
  width: 100%;
  accent-color: var(--accent-strong);
  cursor: pointer;
}

.audio-level-control output {
  color: var(--muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.audio-preference-row__controls select {
  width: min(230px, 30vw);
  min-height: 40px;
  padding: 7px 32px 7px 10px;
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  font-size: 13px;
}

.audio-preferences__error {
  margin-top: 12px;
  color: var(--danger);
  font-size: 11px;
}

@media (max-width: 680px) {
  .audio-preferences {
    gap: 28px;
  }

  .audio-preference-row__controls {
    width: 100%;
    justify-content: flex-end;
  }

  .audio-level-control {
    width: 100%;
  }

  .audio-preference-row__controls select {
    width: 100%;
    min-width: 0;
    flex: 1;
  }
}
</style>
