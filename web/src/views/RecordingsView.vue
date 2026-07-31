<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { AudioLines, Download, Star, Trash2 } from '@lucide/vue'
import type { RecordingEntry } from '../api/types'
import BatchActionBar from '../components/BatchActionBar.vue'
import CommunicationAvatar from '../components/CommunicationAvatar.vue'
import ContactHeaderIdentity from '../components/ContactHeaderIdentity.vue'
import ContactNumberActions from '../components/ContactNumberActions.vue'
import FavoriteFilterButton from '../components/FavoriteFilterButton.vue'
import InfiniteScrollTrigger from '../components/InfiniteScrollTrigger.vue'
import LineSelector from '../components/LineSelector.vue'
import LineTag from '../components/LineTag.vue'
import ListItemAvatarStatus from '../components/ListItemAvatarStatus.vue'
import ListItemStatusRail from '../components/ListItemStatusRail.vue'
import ListSkeleton from '../components/ListSkeleton.vue'
import ListSelectionToggle from '../components/ListSelectionToggle.vue'
import SearchField from '../components/SearchField.vue'
import SelectableListRow from '../components/SelectableListRow.vue'
import StatePanel from '../components/StatePanel.vue'
import SwipeActionRow from '../components/SwipeActionRow.vue'
import FavoriteActionButton from '../components/workspace/FavoriteActionButton.vue'
import WorkspaceDetailHeader from '../components/workspace/WorkspaceDetailHeader.vue'
import WorkspaceDetailPane from '../components/workspace/WorkspaceDetailPane.vue'
import WorkspaceListHeader from '../components/workspace/WorkspaceListHeader.vue'
import WorkspaceMasterDetail from '../components/workspace/WorkspaceMasterDetail.vue'
import { useListSelection } from '../composables/useListSelection'
import { audioState } from '../state/audio'
import { requestConfirmation } from '../state/confirmation'
import {
  deleteRecording,
  deleteRecordings,
  loadRecordingEntries,
  loadMoreRecordingEntries,
  recordingCatalogPagination,
  recordingCatalogState,
  setRecordingsFavorite
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
const favoriteOnly = ref(route.query.favorite === '1')
const deletingRecordingID = ref('')
const favoritePendingCallID = ref('')
const deleteError = ref('')
const batchBusy = ref(false)
const selection = useListSelection<RecordingEntry>(recording => recording.id)
const selecting = selection.active
const selectionCount = selection.count
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
  return recordingCatalogState.data.filter(recording => {
    if (favoriteOnly.value && !recording.favorite) return false
    if (lineFilterKey.value === 'all') return true
    const line = lineForRecording(recording)
    return line ? lineKey(line) === lineFilterKey.value : false
  })
})
const batchRecordings = computed(() => selection.selected(filteredRecordings.value))
const batchAllFavorite = computed(
  () =>
    batchRecordings.value.length > 0 &&
    batchRecordings.value.every(recording => recording.favorite)
)

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
  void router.push({
    name: 'recordings',
    query: {
      selected: recording.id,
      ...(favoriteOnly.value ? { favorite: '1' } : {})
    }
  })
}

function setFavoriteFilter(value: boolean): void {
  favoriteOnly.value = value
  void router.replace({
    name: 'recordings',
    query: {
      ...(selectedID.value ? { selected: selectedID.value } : {}),
      ...(value ? { favorite: '1' } : {})
    }
  })
}

async function toggleRecordingFavorite(recording: RecordingEntry): Promise<void> {
  if (favoritePendingCallID.value) return
  favoritePendingCallID.value = recording.call_id
  deleteError.value = ''
  const favorite = !recording.favorite
  try {
    await setRecordingsFavorite([recording], favorite)
    if (!favorite && favoriteOnly.value && selectedID.value === recording.id) {
      await router.replace({
        name: 'recordings',
        query: { favorite: '1' }
      })
    }
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('common.favoriteFailed')
  } finally {
    favoritePendingCallID.value = ''
  }
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

async function batchDelete(): Promise<void> {
  const recordings = batchRecordings.value
  if (batchBusy.value || recordings.length === 0) return
  const confirmed = await requestConfirmation({
    title: t('recordings.deleteSelectedTitle'),
    message: t('recordings.deleteSelectedMessage', { count: recordings.length }),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  batchBusy.value = true
  deleteError.value = ''
  try {
    const deleted = new Set(recordings.map(recording => recording.id))
    await deleteRecordings(recordings)
    if (deleted.has(selectedID.value)) {
      await router.replace({ name: 'recordings' })
    }
    selection.exit()
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('recordings.deleteFailed')
  } finally {
    batchBusy.value = false
  }
}

async function batchSetFavorite(favorite: boolean): Promise<void> {
  const recordings = batchRecordings.value
  if (batchBusy.value || recordings.length === 0) return
  batchBusy.value = true
  deleteError.value = ''
  try {
    await setRecordingsFavorite(recordings, favorite)
    if (!favorite && favoriteOnly.value) {
      if (recordings.some(recording => recording.id === selectedID.value)) {
        await router.replace({
          name: 'recordings',
          query: { favorite: '1' }
        })
      }
      selection.clear()
    }
  } catch (error) {
    deleteError.value =
      error instanceof Error ? error.message : t('common.favoriteFailed')
  } finally {
    batchBusy.value = false
  }
}

function onSelectionKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && selection.active.value) selection.exit()
}

function scheduleSearch(value: string): void {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    searchTimer = undefined
    void loadRecordingEntries(value)
  }, 250)
}

watch(search, value => {
  selection.clear()
  scheduleSearch(value)
})

watch(
  () => route.query.favorite,
  value => {
    favoriteOnly.value = value === '1'
  }
)

watch(favoriteOnly, () => selection.clear())

watch(
  [
    selectedID,
    selected,
    () => recordingCatalogState.status,
    () => recordingCatalogPagination.hasMore,
    () => recordingCatalogPagination.loadingMore
  ],
  ([id, recording, status, hasMore, loadingMore]) => {
    if (
      id &&
      !recording &&
      status === 'ready' &&
      hasMore &&
      !loadingMore
    ) {
      void loadMoreRecordingEntries()
    }
  }
)

watch(filteredRecordings, recordings => selection.reconcile(recordings))

watch(lines, availableLines => {
  if (
    lineFilterKey.value !== 'all' &&
    !availableLines.some(line => lineKey(line) === lineFilterKey.value)
  ) {
    lineFilterKey.value = 'all'
  }
  selection.clear()
})

onMounted(() => {
  window.addEventListener('keydown', onSelectionKeydown)
  void Promise.all([loadBootstrap(), loadRecordingEntries(), loadContacts()])
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onSelectionKeydown)
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
})
</script>

<template>
  <WorkspaceMasterDetail
    :has-selection="Boolean(selected)"
    :batch-selecting="selecting"
  >
    <template #list>
      <WorkspaceListHeader
        :title="t('shell.recordings')"
        :count="
          recordingCatalogState.status === 'ready'
            ? recordingCatalogState.data.length
            : undefined
        "
      />

      <div class="pane-search">
        <div class="pane-search-row">
          <ListSelectionToggle
            :active="selecting"
            :label="t('common.selectMultiple')"
            :done-label="t('common.done')"
            :disabled="
              recordingCatalogState.status !== 'ready' ||
              recordingCatalogState.data.length === 0
            "
            @toggle="selection.toggleMode"
          />
          <SearchField
            v-model="search"
            :placeholder="t('common.search')"
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
          <FavoriteFilterButton
            :active="favoriteOnly"
            :label="t('common.favoriteOnly')"
            @toggle="setFavoriteFilter(!favoriteOnly)"
          />
        </div>
      </div>
      <p v-if="deleteError" class="field-error recording-delete-error" role="alert">
        {{ deleteError }}
      </p>

      <ListSkeleton
        v-if="recordingCatalogState.status === 'loading'"
        :label="t('recordings.loading')"
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
      <div v-else class="item-list">
        <StatePanel
          v-if="
            filteredRecordings.length === 0 &&
            !recordingCatalogPagination.hasMore &&
            !recordingCatalogPagination.loadingMore
          "
          state="empty"
          :title="
            search || favoriteOnly || lineFilterKey !== 'all'
              ? t('recordings.noMatches')
              : t('recordings.empty')
          "
        />
        <template v-else>
          <SelectableListRow
            v-for="recording in filteredRecordings"
            :key="recording.id"
            :active="selecting"
            :selected="selection.has(recording)"
            :label="t('common.selectItem', { name: displayName(recording) })"
            @toggle="selection.toggle(recording)"
          >
            <SwipeActionRow
              :delete-label="t('common.delete')"
              :disabled="
                selecting ||
                Boolean(deletingRecordingID) ||
                favoritePendingCallID === recording.call_id
              "
              @delete="removeRecording(recording)"
            >
              <button
                class="list-item recording-list-item"
                :class="{ 'is-selected': recording.id === selectedID }"
                type="button"
                @click="selectRecording(recording)"
              >
                <ListItemAvatarStatus class="recording-list-item__avatar">
                  <CommunicationAvatar
                    channel="call"
                    :name="displayName(recording)"
                    :address="recordingDisplayNumber(recording)"
                    :src="avatar(recording)"
                  />
                  <template #badge>
                    <span
                      class="recording-list-item__icon"
                      :class="{ 'is-unavailable': !recording.playable }"
                    >
                      <AudioLines :size="12" />
                    </span>
                  </template>
                </ListItemAvatarStatus>
                <span class="list-item__content">
                  <strong>{{ displayName(recording) }}</strong>
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
                <ListItemStatusRail
                  :date="formatRelativeDate(recording.recorded_at)"
                  :date-time="recording.recorded_at"
                >
                  <template #favorite>
                    <Star
                      v-if="recording.favorite"
                      class="recording-list-item__favorite"
                      :size="15"
                      fill="currentColor"
                      :aria-label="t('common.favorite')"
                    />
                  </template>
                </ListItemStatusRail>
              </button>
            </SwipeActionRow>
          </SelectableListRow>
        </template>
        <InfiniteScrollTrigger
          :has-more="recordingCatalogPagination.hasMore"
          :loading="recordingCatalogPagination.loadingMore"
          :error="recordingCatalogPagination.error"
          :loading-label="t('recordings.loading')"
          :retry-label="t('common.retry')"
          @load="loadMoreRecordingEntries"
        />
      </div>
      <BatchActionBar
        v-if="selecting"
        :selected="selectionCount"
        :total="filteredRecordings.length"
        :selected-label="t('common.selectedCount', { count: selectionCount })"
        :select-all-label="t('common.selectAll')"
        :clear-all-label="t('common.clearAll')"
        :done-label="t('common.done')"
        :busy="batchBusy"
        @select-all="selection.selectAll(filteredRecordings)"
        @done="selection.exit"
      >
        <button
          v-if="batchRecordings.length > 0"
          type="button"
          :disabled="batchBusy"
          :title="
            batchAllFavorite
              ? t('common.unfavorite')
              : t('common.favorite')
          "
          @click="batchSetFavorite(!batchAllFavorite)"
        >
          <Star
            :size="17"
            :fill="batchAllFavorite ? 'currentColor' : 'none'"
          />
          <span>
            {{
              batchAllFavorite
                ? t('common.unfavorite')
                : t('common.favorite')
            }}
          </span>
        </button>
        <button
          v-if="batchRecordings.length > 0"
          class="is-danger"
          type="button"
          :disabled="batchBusy"
          :title="t('common.delete')"
          @click="batchDelete"
        >
          <Trash2 :size="17" />
          <span>{{ t('common.delete') }}</span>
        </button>
      </BatchActionBar>
    </template>

    <template #detail>
      <WorkspaceDetailPane :content-key="selected?.id">
        <template v-if="selected">
          <WorkspaceDetailHeader>
          <template #identity>
            <ContactHeaderIdentity
              :name="displayName(selected)"
              :number="recordingDisplayNumber(selected)"
              :avatar="avatar(selected)"
              :line="lineTagLine(lineForRecording(selected), selected.call.line_id)"
              :line-fallback="recordingLineFallback(selected)"
            />
          </template>
          <template #actions>
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
            <FavoriteActionButton
              :active="selected.favorite"
              :disabled="Boolean(favoritePendingCallID)"
              :activate-label="t('common.favorite')"
              :deactivate-label="t('common.unfavorite')"
              @toggle="toggleRecordingFavorite(selected)"
            />
          </template>
        </WorkspaceDetailHeader>

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
        <template #empty>
          <StatePanel
            v-if="selectedID && recordingCatalogState.status === 'ready'"
            state="empty"
            :title="t('recordings.notInResults')"
          />
          <StatePanel
            v-else
            state="empty"
            :title="t('recordings.select')"
          />
        </template>
      </WorkspaceDetailPane>
    </template>
  </WorkspaceMasterDetail>
</template>

<style scoped>
.recording-list-item {
  cursor: pointer;
}

.recording-list-item__icon {
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

.recording-list-item__favorite {
  flex: 0 0 auto;
  color: #a86400;
}

.recording-detail {
  min-height: 0;
  padding: 26px;
  overflow-y: auto;
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
