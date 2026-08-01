<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Download, LoaderCircle, RefreshCw, Trash2 } from '@lucide/vue'
import LoadingSkeletonBoundary from './skeletons/LoadingSkeletonBoundary.vue'
import SectionSkeleton from './skeletons/SectionSkeleton.vue'
import {
  deleteRecording,
  loadCallRecordings,
  recordingListState
} from '../state/recording'
import { audioState } from '../state/audio'
import { requestConfirmation } from '../state/confirmation'
import { formatDateTime, formatDuration } from '../utils/format'

const { t } = useI18n()
const props = defineProps<{
  callId: string
}>()
const deletingID = ref('')
const deleteError = ref('')

const current = computed(() =>
  recordingListState.callID === props.callId
    ? recordingListState
    : {
        status: 'loading' as const,
        data: [],
        error: ''
      }
)

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.max(0.1, bytes / 1024).toFixed(1)} KB`
  return `${Math.max(0.1, bytes / (1024 * 1024)).toFixed(1)} MB`
}

function downloadName(id: string, contentType: string): string {
  const normalized = contentType.toLocaleLowerCase()
  const extension = normalized.includes('ogg')
    ? 'ogg'
    : normalized.includes('flac')
      ? 'flac'
      : normalized.includes('mpeg')
        ? 'mp3'
        : normalized.includes('wav')
          ? 'wav'
          : 'audio'
  return `modemdeck-${id}.${extension}`
}

function statusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return t('recordings.pending')
    case 'recording':
      return t('recordings.recording')
    case 'failed':
      return t('recordings.failed')
    default:
      return t('recordings.playable')
  }
}

async function removeRecording(recordingID: string): Promise<void> {
  const confirmed = await requestConfirmation({
    title: t('recordings.deleteConfirmTitle'),
    message: t('recordings.deleteSegmentConfirmMessage'),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  deletingID.value = recordingID
  deleteError.value = ''
  try {
    await deleteRecording(props.callId, recordingID)
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('recordings.deleteFailed')
  } finally {
    deletingID.value = ''
  }
}

watch(
  () => props.callId,
  callID => {
    void loadCallRecordings(callID)
  },
  { immediate: true }
)
</script>

<template>
  <section class="recording-list" aria-labelledby="call-recordings-title">
    <header>
      <h3 id="call-recordings-title">{{ t('recordings.title') }}</h3>
      <span v-if="current.status === 'ready'">{{ current.data.length }}</span>
    </header>

    <p v-if="deleteError" class="recording-list__state recording-list__state--error">
      {{ deleteError }}
    </p>
    <LoadingSkeletonBoundary
      :loading="current.status === 'loading' || current.status === 'idle'"
    >
      <template #skeleton>
        <SectionSkeleton
          :label="t('recordings.loading')"
          variant="rows"
          :rows="2"
        />
      </template>
      <div
        v-if="current.status === 'error' || current.status === 'forbidden'"
        class="recording-list__state recording-list__state--error"
        role="alert"
      >
        <span>{{ current.error }}</span>
        <button
          v-if="current.status === 'error'"
          type="button"
          :title="t('common.retry')"
          :aria-label="t('recordings.reload')"
          @click="loadCallRecordings(callId, true)"
        >
          <RefreshCw :size="16" />
        </button>
      </div>
      <p v-else-if="current.data.length === 0" class="recording-list__empty">
        {{ t('recordings.emptyForCall') }}
      </p>
      <ol v-else>
      <li v-for="recording in current.data" :key="recording.id">
        <div class="recording-list__meta">
          <strong>{{ t('recordings.segment', { number: recording.segment_index }) }}</strong>
          <span>
            {{ formatDateTime(recording.recorded_at) }} ·
            {{ formatDuration(recording.duration_seconds) }} ·
            {{ formatSize(recording.size_bytes) }}
          </span>
        </div>
        <audio
          v-if="recording.playable && recording.download_url"
          :src="recording.download_url"
          :volume="audioState.recordingPlaybackVolume / 100"
          controls
          preload="metadata"
        >
          {{ t('recordings.audioUnsupported') }}
        </audio>
        <p v-else class="recording-list__availability">
          {{ statusLabel(recording.status) }}
        </p>
        <a
          v-if="recording.playable && recording.download_url"
          :href="recording.download_url"
          :download="downloadName(recording.id, recording.content_type || '')"
          :title="t('recordings.download')"
          :aria-label="t('recordings.download')"
        >
          <Download :size="18" />
        </a>
        <span v-else aria-hidden="true" />
        <button
          v-if="recording.status !== 'pending' && recording.status !== 'recording'"
          class="recording-list__delete"
          type="button"
          :disabled="Boolean(deletingID)"
          :title="t('recordings.delete')"
          :aria-label="t('recordings.delete')"
          @click="removeRecording(recording.id)"
        >
          <LoaderCircle v-if="deletingID === recording.id" class="spin" :size="17" />
          <Trash2 v-else :size="17" />
        </button>
        <span v-else aria-hidden="true" />
      </li>
      </ol>
    </LoadingSkeletonBoundary>
  </section>
</template>

<style scoped>
.recording-list {
  container-type: inline-size;
  border-top: 1px solid var(--border);
}

.recording-list > header {
  display: flex;
  min-height: 46px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.recording-list h3 {
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0;
  text-transform: uppercase;
}

.recording-list header span {
  color: var(--muted);
  font-size: 10px;
}

.recording-list__state,
.recording-list__empty {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 11px;
}

.recording-list__state--error {
  color: var(--danger);
}

.recording-list__state button {
  display: inline-grid;
  width: 30px;
  height: 30px;
  place-items: center;
  color: inherit;
  background: transparent;
  border-radius: 50%;
}

.recording-list ol {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
  padding: 0 0 4px;
  list-style: none;
}

.recording-list li {
  display: grid;
  min-width: 0;
  align-items: center;
  grid-template-columns: minmax(240px, 0.9fr) minmax(220px, 1fr) 34px 34px;
  gap: 12px;
  padding: 10px 12px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.recording-list__meta {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 8px;
}

.recording-list__meta strong {
  flex: 0 0 auto;
  font-size: 11px;
}

.recording-list__meta span {
  overflow: hidden;
  color: var(--muted);
  font-size: 9px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recording-list audio {
  width: 100%;
  min-width: 0;
  height: 34px;
}

.recording-list__availability {
  margin: 0;
  color: var(--muted);
  font-size: 11px;
}

.recording-list a {
  display: inline-grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--muted);
  border-radius: 50%;
}

.recording-list a:hover {
  color: var(--accent-strong);
  background: var(--surface-hover);
}

.recording-list__delete {
  display: inline-grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--muted);
  border-radius: 50%;
}

.recording-list__delete:hover:not(:disabled) {
  color: var(--danger);
  background: var(--danger-soft);
}

@container (max-width: 720px) {
  .recording-list li {
    grid-template-columns: minmax(0, 1fr) 34px 34px;
  }

  .recording-list__meta {
    grid-column: 1;
    grid-row: 1;
  }

  .recording-list audio {
    grid-column: 1 / 4;
    grid-row: 2;
  }

  .recording-list__availability {
    grid-column: 1 / 4;
    grid-row: 2;
  }

  .recording-list a {
    grid-column: 2;
    grid-row: 1;
  }

  .recording-list__delete {
    grid-column: 3;
    grid-row: 1;
  }
}
</style>
