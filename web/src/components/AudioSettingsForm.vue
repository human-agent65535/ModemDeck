<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import {
  AudioLines,
  BellRing,
  MessageSquareText,
  PhoneOutgoing,
  Play,
  Send,
  Square
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import AudioDeviceControls from './AudioDeviceControls.vue'
import {
  browserSoundState,
  notificationCatalog,
  previewSound,
  ringtoneCatalog,
  setIncomingMessageSound,
  setOutgoingMessageSound,
  setRingtone,
  setWaitingSound,
  stopSoundPreview,
  type MessageSoundID,
  type RingtoneID,
  type SoundPreview
} from '../state/browserSounds'

const { t } = useI18n()
const selectedRingtone = computed(
  () =>
    ringtoneCatalog.find(ringtone => ringtone.id === browserSoundState.ringtone)?.name ||
    t('audio.silent')
)
const selectedRingtonePreview = computed<SoundPreview | ''>(() =>
  browserSoundState.ringtone === 'silent'
    ? ''
    : `ringtone:${browserSoundState.ringtone}`
)
const selectedIncomingMessage = computed(
  () =>
    notificationCatalog.find(
      notification => notification.id === browserSoundState.incomingMessage
    )?.name || t('audio.silent')
)
const selectedOutgoingMessage = computed(
  () =>
    notificationCatalog.find(
      notification => notification.id === browserSoundState.outgoingMessage
    )?.name || t('audio.silent')
)
const selectedIncomingMessagePreview = computed<SoundPreview | ''>(() =>
  browserSoundState.incomingMessage === 'silent'
    ? ''
    : `incoming-message:${browserSoundState.incomingMessage}`
)
const selectedOutgoingMessagePreview = computed<SoundPreview | ''>(() =>
  browserSoundState.outgoingMessage === 'silent'
    ? ''
    : `outgoing-message:${browserSoundState.outgoingMessage}`
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

onBeforeUnmount(() => {
  stopSoundPreview()
})
</script>

<template>
  <div class="audio-preferences">
    <section class="audio-preferences__section" aria-labelledby="audio-devices-title">
      <header class="audio-preferences__header">
        <span class="audio-preferences__icon"><AudioLines :size="20" /></span>
        <div>
          <h3 id="audio-devices-title">{{ t('audio.devices') }}</h3>
          <p>{{ t('audio.devicesDescription') }}</p>
        </div>
      </header>
      <AudioDeviceControls />
    </section>

    <section class="audio-preferences__section" aria-labelledby="call-sounds-title">
      <header class="audio-preferences__header">
        <span class="audio-preferences__icon is-call"><BellRing :size="20" /></span>
        <div>
          <h3 id="call-sounds-title">{{ t('audio.callSounds') }}</h3>
          <p>{{ t('audio.callSoundsDescription') }}</p>
        </div>
      </header>

      <div class="audio-preference-row">
        <span class="audio-preference-row__identity">
          <BellRing :size="18" />
          <span>
            <strong>{{ t('audio.incomingRingtone') }}</strong>
            <small>{{ selectedRingtone }}</small>
          </span>
        </span>
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
            <option value="silent">{{ t('audio.silent') }}</option>
          </select>
          <button
            class="icon-button"
            type="button"
            :disabled="!selectedRingtonePreview"
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
        </span>
      </div>

      <div class="audio-preference-row">
        <span class="audio-preference-row__identity">
          <PhoneOutgoing :size="18" />
          <span>
            <strong>{{ t('audio.waitingTone') }}</strong>
            <small>{{ t('audio.outgoingCall') }}</small>
          </span>
        </span>
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
      </div>
    </section>

    <section class="audio-preferences__section" aria-labelledby="message-sounds-title">
      <header class="audio-preferences__header">
        <span class="audio-preferences__icon is-message">
          <MessageSquareText :size="20" />
        </span>
        <div>
          <h3 id="message-sounds-title">{{ t('audio.messageSounds') }}</h3>
          <p>{{ t('audio.messageSoundsDescription') }}</p>
        </div>
      </header>

      <div class="audio-preference-row">
        <span class="audio-preference-row__identity">
          <MessageSquareText :size="18" />
          <span>
            <strong>{{ t('audio.incomingMessage') }}</strong>
            <small>{{ selectedIncomingMessage }}</small>
          </span>
        </span>
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
            <option value="silent">{{ t('audio.silent') }}</option>
          </select>
          <button
            class="icon-button"
            type="button"
            :disabled="!selectedIncomingMessagePreview"
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
        </span>
      </div>

      <div class="audio-preference-row">
        <span class="audio-preference-row__identity">
          <Send :size="18" />
          <span>
            <strong>{{ t('audio.outgoingMessage') }}</strong>
            <small>{{ selectedOutgoingMessage }}</small>
          </span>
        </span>
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
            <option value="silent">{{ t('audio.silent') }}</option>
          </select>
          <button
            class="icon-button"
            type="button"
            :disabled="!selectedOutgoingMessagePreview"
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
        </span>
      </div>

      <p v-if="browserSoundState.error" class="audio-preferences__error" role="alert">
        {{ browserSoundState.error }}
      </p>
    </section>
  </div>
</template>

<style scoped>
.audio-preferences {
  display: flex;
  max-width: 760px;
  flex-direction: column;
  gap: 34px;
}

.audio-preferences__section {
  min-width: 0;
}

.audio-preferences__header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.audio-preferences__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--blue);
  background: var(--blue-soft);
  border-radius: 50%;
}

.audio-preferences__icon.is-call {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.audio-preferences__icon.is-message {
  color: #8a5700;
  background: #fff4d6;
}

.audio-preferences h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.audio-preferences__header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 11px;
}

.audio-preference-row {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--border);
}

.audio-preference-row__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 11px;
}

.audio-preference-row__identity > svg {
  flex: 0 0 auto;
  color: var(--muted);
}

.audio-preference-row__identity > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.audio-preference-row strong {
  color: var(--text);
  font-size: 13px;
}

.audio-preference-row small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audio-preference-row__controls {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
}

.audio-preference-row select {
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

  .audio-preference-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
    padding: 13px 0;
  }

  .audio-preference-row__controls {
    width: 100%;
    justify-content: flex-end;
  }

  .audio-preference-row select {
    width: 100%;
    min-width: 0;
    flex: 1;
  }
}
</style>
