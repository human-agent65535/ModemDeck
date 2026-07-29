<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ArrowLeft, AudioLines, Download, Trash2 } from '@lucide/vue'
import type { RecordingEntry } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import LineSelector from '../components/LineSelector.vue'
import LineTag from '../components/LineTag.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import { audioState } from '../state/audio'
import { requestConfirmation } from '../state/confirmation'
import {
  deleteRecording,
  loadRecordingEntries,
  recordingCatalogState
} from '../state/recording'
import {
  bootstrapResource,
  contactForNumber,
  displayPhoneNumber,
  lineForKey,
  lineKey,
  loadBootstrap,
  loadContacts
} from '../state/workspace'
import { formatDateTime, formatDuration, formatRelativeDate } from '../utils/format'
import { lineTagFallback, lineTagLine } from '../utils/lineIdentity'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const search = ref('')
const lineFilterKey = ref('all')
const deletingRecordingID = ref('')
const deleteError = ref('')
let searchTimer: number | undefined

const selectedID = computed(() =>
  typeof route.query.selected === 'string' ? route.query.selected : ''
)
const selected = computed(() =>
  recordingCatalogState.data.find(recording => recording.id === selectedID.value)
)
const lines = computed(() => bootstrapResource.data?.lines || [])
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const selectedContact = computed(() =>
  selected.value ? contactForNumber(selected.value.call.remote_number) : undefined
)
const filteredRecordings = computed(() => {
  if (lineFilterKey.value === 'all') return recordingCatalogState.data
  return recordingCatalogState.data.filter(recording => {
    const line = lineForRecording(recording)
    return line ? lineKey(line) === lineFilterKey.value : false
  })
})

function displayName(recording: RecordingEntry): string {
  return (
    recording.call.display_name ||
    contactForNumber(recording.call.remote_number)?.display_name ||
    recordingDisplayNumber(recording)
  )
}

function recordingDisplayNumber(recording: RecordingEntry): string {
  return displayPhoneNumber(
    recording.call.remote_number,
    recording.call.line_id
  )
}

function avatar(recording: RecordingEntry): string {
  return contactForNumber(recording.call.remote_number)?.avatar || ''
}

function directionLabel(recording: RecordingEntry): string {
  return recording.call.direction === 'incoming'
    ? t('dashboard.incoming')
    : t('dashboard.outgoing')
}

function lineForRecording(recording: RecordingEntry) {
  return lineForKey(recording.call.line_id)
}

function recordingLineFallback(recording: RecordingEntry): string {
  return lineTagFallback(
    lineForRecording(recording),
    lines.value,
    defaultLineID.value,
    recording.call.line_id
  )
}

function statusLabel(recording: RecordingEntry): string {
  if (recording.playable) return t('recordings.playable')
  switch (recording.status) {
    case 'pending':
      return t('recordings.pending')
    case 'recording':
      return t('recordings.recording')
    case 'failed':
      return t('recordings.failed')
    default:
      return t('recordings.unplayable')
  }
}

function unavailableDetail(recording: RecordingEntry): string {
  switch (recording.status) {
    case 'pending':
      return t('recordings.notStarted')
    case 'recording':
      return t('recordings.notEnded')
    case 'failed':
      return t('recordings.incomplete')
    default:
      return t('recordings.fileUnavailable')
  }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.max(0.1, bytes / 1024).toFixed(1)} KB`
  return `${Math.max(0.1, bytes / (1024 * 1024)).toFixed(1)} MB`
}

function selectRecording(recording: RecordingEntry): void {
  void router.push({ name: 'recordings', query: { selected: recording.id } })
}

async function removeRecording(recording: RecordingEntry): Promise<void> {
  const confirmed = await requestConfirmation({
    title: t('recordings.deleteConfirmTitle'),
    message: t('recordings.deleteConfirmMessage', {
      name: displayName(recording)
    }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  deletingRecordingID.value = recording.id
  deleteError.value = ''
  try {
    await deleteRecording(recording.call_id, recording.id)
    if (selectedID.value === recording.id) {
      await router.replace({ name: 'recordings' })
    }
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('recordings.deleteFailed')
  } finally {
    deletingRecordingID.value = ''
  }
}

function backToList(): void {
  void router.push({ name: 'recordings' })
}

function scheduleSearch(value: string): void {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    searchTimer = undefined
    void loadRecordingEntries(value)
  }, 250)
}

watch(search, scheduleSearch)

watch(lines, availableLines => {
  if (
    lineFilterKey.value !== 'all' &&
    !availableLines.some(line => lineKey(line) === lineFilterKey.value)
  ) {
    lineFilterKey.value = 'all'
  }
})

onMounted(() => {
  void Promise.all([loadBootstrap(), loadRecordingEntries(), loadContacts()])
})

onBeforeUnmount(() => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
})
</script>

<template>
  <section class="workspace" :class="{ 'has-selection': selected }">
    <aside class="list-pane">
      <header class="pane-header">
        <div>
          <h1>{{ t('shell.recordings') }}</h1>
          <span v-if="recordingCatalogState.status === 'ready'">
            {{ recordingCatalogState.data.length }}
          </span>
        </div>
      </header>

      <div class="pane-search">
        <div class="pane-search-row">
          <SearchField
            v-model="search"
            :placeholder="t('contacts.searchNameOrNumber')"
          />
          <LineSelector
            v-if="lines.length > 1"
            v-model="lineFilterKey"
            class="recording-line-filter"
            :lines="lines"
            :default-line-id="defaultLineID"
            :label="t('recordings.lineFilter')"
            include-all
            filter-mode
            :all-label="t('recordings.allLines')"
            :all-description="t('recordings.allLinesDescription')"
          />
        </div>
      </div>
      <p v-if="deleteError" class="field-error recording-delete-error" role="alert">
        {{ deleteError }}
      </p>

      <StatePanel
        v-if="recordingCatalogState.status === 'loading'"
        state="loading"
        :title="t('recordings.loading')"
      />
      <StatePanel
        v-else-if="recordingCatalogState.status === 'forbidden'"
        state="forbidden"
        :title="t('recordings.forbidden')"
        :detail="recordingCatalogState.error"
      />
      <StatePanel
        v-else-if="recordingCatalogState.status === 'error'"
        state="error"
        :title="t('recordings.loadFailed')"
        :detail="recordingCatalogState.error"
        retryable
        @retry="loadRecordingEntries(search, true)"
      />
      <StatePanel
        v-else-if="filteredRecordings.length === 0"
        state="empty"
        :title="
          search || lineFilterKey !== 'all'
            ? t('recordings.noMatches')
            : t('recordings.empty')
        "
      />
      <div v-else class="item-list">
        <SwipeActionRow
          v-for="recording in filteredRecordings"
          :key="recording.id"
          :delete-label="t('common.delete')"
          :disabled="Boolean(deletingRecordingID)"
          @delete="removeRecording(recording)"
        >
          <button
            class="list-item recording-list-item"
            :class="{ 'is-selected': recording.id === selectedID }"
            type="button"
            @click="selectRecording(recording)"
          >
            <span class="recording-list-item__avatar">
              <BaseAvatar :name="displayName(recording)" :src="avatar(recording)" />
              <span
                class="recording-list-item__icon"
                :class="{ 'is-unavailable': !recording.playable }"
              >
                <AudioLines :size="12" />
              </span>
            </span>
            <span class="list-item__content">
              <span class="list-item__title">
                <strong>{{ displayName(recording) }}</strong>
                <time>{{ formatRelativeDate(recording.recorded_at) }}</time>
              </span>
              <span class="recording-list-item__meta">
                <LineTag
                  :line="lineTagLine(lineForRecording(recording), recording.call.line_id)"
                  :fallback="recordingLineFallback(recording)"
                />
                <small>
                  {{ directionLabel(recording) }} ·
                  {{
                    recording.playable
                      ? formatDuration(recording.duration_seconds)
                      : statusLabel(recording)
                  }}
                </small>
              </span>
            </span>
          </button>
        </SwipeActionRow>
      </div>
    </aside>

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            :title="t('recordings.back')"
            :aria-label="t('recordings.backList')"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <ContactHeaderIdentity
            :name="displayName(selected)"
            :number="recordingDisplayNumber(selected)"
            :avatar="avatar(selected)"
            :line="lineTagLine(lineForRecording(selected), selected.call.line_id)"
            :line-fallback="recordingLineFallback(selected)"
          />
          <div class="recording-header__contact-actions">
            <ContactNumberActions
              :number="selected.call.remote_number"
              :contact="selectedContact"
              compact
            />
            <button
              class="icon-button icon-button--danger desktop-delete-action"
              type="button"
              :disabled="Boolean(deletingRecordingID)"
              :title="t('recordings.delete')"
              @click="removeRecording(selected)"
            >
              <Trash2 :size="18" />
            </button>
          </div>
        </header>

        <div class="recording-detail">
          <section class="recording-player" :aria-label="t('recordings.playback')">
            <template v-if="selected.playable && selected.download_url">
              <audio
                :src="selected.download_url"
                :volume="audioState.recordingPlaybackVolume / 100"
                controls
                preload="metadata"
              >
                {{ t('recordings.audioUnsupported') }}
              </audio>
              <a
                class="icon-button recording-download"
                :href="selected.download_url"
                :download="`modemdeck-${selected.id}.ogg`"
                :title="t('recordings.download')"
                :aria-label="t('recordings.download')"
              >
                <Download :size="19" />
              </a>
            </template>
            <StatePanel
              v-else
              state="empty"
              :title="t('recordings.noPlayableSegment')"
              :detail="unavailableDetail(selected)"
            />
          </section>

          <section class="detail-section detail-facts">
            <h3>{{ t('recordings.details') }}</h3>
            <dl>
              <div>
                <dt>{{ t('dashboard.time') }}</dt>
                <dd>{{ formatDateTime(selected.recorded_at) }}</dd>
              </div>
              <div>
                <dt>{{ t('dashboard.duration') }}</dt>
                <dd>{{ formatDuration(selected.duration_seconds) }}</dd>
              </div>
              <div>
                <dt>{{ t('dashboard.direction') }}</dt>
                <dd>{{ directionLabel(selected) }}</dd>
              </div>
              <div>
                <dt>{{ t('dashboard.line') }}</dt>
                <dd>
                  <LineTag
                    :line="lineTagLine(lineForRecording(selected), selected.call.line_id)"
                    :fallback="recordingLineFallback(selected)"
                  />
                </dd>
              </div>
              <div>
                <dt>{{ t('recordings.size') }}</dt>
                <dd>{{ formatSize(selected.size_bytes) }}</dd>
              </div>
              <div>
                <dt>{{ t('recordings.relatedCall') }}</dt>
                <dd>
                  <RouterLink
                    class="recording-call-link"
                    :to="{ name: 'calls', query: { selected: selected.call.id } }"
                  >
                    {{ formatDateTime(selected.call.started_at) }}
                  </RouterLink>
                </dd>
              </div>
            </dl>
          </section>
        </div>
      </template>

      <StatePanel
        v-else-if="selectedID && recordingCatalogState.status === 'ready'"
        state="empty"
        :title="t('recordings.notInResults')"
      />
      <StatePanel
        v-else
        state="empty"
        :title="t('recordings.select')"
      />
    </article>
  </section>
</template>

<style scoped>
.recording-list-item {
  cursor: pointer;
}

.recording-list-item__avatar {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
}

.recording-list-item__icon {
  position: absolute;
  right: -4px;
  bottom: -4px;
  display: inline-grid;
  width: 21px;
  height: 21px;
  flex: 0 0 21px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border: 2px solid var(--surface);
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 14%);
}

.recording-list-item__icon.is-unavailable {
  color: var(--muted);
  background: var(--surface-hover);
}

.recording-list-item__meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.recording-list-item__meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recording-detail {
  min-height: 0;
  padding: 26px;
  overflow-y: auto;
}

.recording-header__contact-actions {
  display: flex;
  align-items: center;
}

.recording-player {
  display: flex;
  width: min(760px, 100%);
  min-height: 92px;
  align-items: center;
  gap: 14px;
  margin-bottom: 30px;
  padding: 18px 0;
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
}

.recording-player audio {
  min-width: 0;
  flex: 1;
}

.recording-player .state-panel {
  min-height: 150px;
  padding: 18px;
}

.recording-download {
  border: 1px solid var(--border);
}

.recording-call-link {
  color: var(--accent-strong);
  font-weight: 650;
}

.recording-call-link:hover {
  text-decoration: underline;
}

.recording-delete-error {
  margin: 0 16px 8px;
}

@media (max-width: 1100px) {
  .desktop-delete-action {
    display: none;
  }
}

@media (max-width: 560px) {
  .recording-detail {
    padding: 18px 14px;
  }
}
</style>
