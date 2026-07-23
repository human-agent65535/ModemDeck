<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ArrowLeft, AudioLines, Download } from '@lucide/vue'
import type { RecordingEntry } from '../api/types'
import BaseAvatar from '../components/BaseAvatar.vue'
import SearchField from '../components/SearchField.vue'
import StatePanel from '../components/StatePanel.vue'
import {
  loadRecordingEntries,
  recordingCatalogState
} from '../state/recording'
import {
  contactForNumber,
  deviceName,
  loadContacts,
  loadDevices
} from '../state/workspace'
import { formatDateTime, formatDuration, formatRelativeDate } from '../utils/format'

const route = useRoute()
const router = useRouter()
const search = ref('')
let searchTimer: number | undefined

const selectedID = computed(() =>
  typeof route.query.selected === 'string' ? route.query.selected : ''
)
const selected = computed(() =>
  recordingCatalogState.data.find(recording => recording.id === selectedID.value)
)

function displayName(recording: RecordingEntry): string {
  return (
    recording.call.display_name ||
    contactForNumber(recording.call.remote_number)?.display_name ||
    recording.call.remote_number
  )
}

function directionLabel(recording: RecordingEntry): string {
  return recording.call.direction === 'incoming' ? '呼入' : '呼出'
}

function statusLabel(recording: RecordingEntry): string {
  if (recording.playable) return '可播放'
  switch (recording.status) {
    case 'pending':
      return '等待录音'
    case 'recording':
      return '录音中'
    case 'failed':
      return '录音失败'
    default:
      return '不可播放'
  }
}

function unavailableDetail(recording: RecordingEntry): string {
  switch (recording.status) {
    case 'pending':
      return '录音尚未开始'
    case 'recording':
      return '录音尚未结束'
    case 'failed':
      return '录音未完成'
    default:
      return '录音文件不可用'
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

onMounted(() => {
  void Promise.all([loadRecordingEntries(), loadContacts(), loadDevices()])
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
          <h1>录音</h1>
          <span v-if="recordingCatalogState.status === 'ready'">
            {{ recordingCatalogState.data.length }}
          </span>
        </div>
      </header>

      <div class="pane-search">
        <SearchField v-model="search" placeholder="搜索姓名或号码" />
      </div>

      <StatePanel
        v-if="recordingCatalogState.status === 'loading'"
        state="loading"
        title="正在载入录音"
      />
      <StatePanel
        v-else-if="recordingCatalogState.status === 'forbidden'"
        state="forbidden"
        title="无权查看通话录音"
        :detail="recordingCatalogState.error"
      />
      <StatePanel
        v-else-if="recordingCatalogState.status === 'error'"
        state="error"
        title="无法载入通话录音"
        :detail="recordingCatalogState.error"
        retryable
        @retry="loadRecordingEntries(search, true)"
      />
      <StatePanel
        v-else-if="recordingCatalogState.data.length === 0"
        state="empty"
        :title="search ? '没有匹配的录音' : '还没有通话录音'"
      />
      <div v-else class="item-list">
        <button
          v-for="recording in recordingCatalogState.data"
          :key="recording.id"
          class="list-item recording-list-item"
          :class="{ 'is-selected': recording.id === selectedID }"
          type="button"
          @click="selectRecording(recording)"
        >
          <span
            class="recording-list-item__icon"
            :class="{ 'is-unavailable': !recording.playable }"
          >
            <AudioLines :size="18" />
          </span>
          <span class="list-item__content">
            <span class="list-item__title">
              <strong>{{ displayName(recording) }}</strong>
              <time>{{ formatRelativeDate(recording.recorded_at) }}</time>
            </span>
            <small>
              {{ directionLabel(recording) }} ·
              {{
                recording.playable
                  ? formatDuration(recording.duration_seconds)
                  : statusLabel(recording)
              }}
            </small>
          </span>
        </button>
      </div>
    </aside>

    <article class="detail-pane">
      <template v-if="selected">
        <header class="detail-header">
          <button
            class="icon-button mobile-back"
            type="button"
            title="返回录音"
            aria-label="返回录音列表"
            @click="backToList"
          >
            <ArrowLeft :size="20" />
          </button>
          <BaseAvatar :name="displayName(selected)" size="large" />
          <div class="detail-header__identity">
            <h2>{{ displayName(selected) }}</h2>
            <span>{{ selected.call.remote_number }}</span>
          </div>
        </header>

        <div class="recording-detail">
          <section class="recording-player" aria-label="录音播放">
            <template v-if="selected.playable && selected.download_url">
              <audio :src="selected.download_url" controls preload="metadata">
                浏览器不支持音频播放。
              </audio>
              <a
                class="icon-button recording-download"
                :href="selected.download_url"
                :download="`modemdeck-${selected.id}.ogg`"
                title="下载录音"
                aria-label="下载录音"
              >
                <Download :size="19" />
              </a>
            </template>
            <StatePanel
              v-else
              state="empty"
              title="没有可播放片段"
              :detail="unavailableDetail(selected)"
            />
          </section>

          <section class="detail-section detail-facts">
            <h3>录音详情</h3>
            <dl>
              <div>
                <dt>时间</dt>
                <dd>{{ formatDateTime(selected.recorded_at) }}</dd>
              </div>
              <div>
                <dt>时长</dt>
                <dd>{{ formatDuration(selected.duration_seconds) }}</dd>
              </div>
              <div>
                <dt>方向</dt>
                <dd>{{ directionLabel(selected) }}</dd>
              </div>
              <div>
                <dt>设备</dt>
                <dd>{{ deviceName(selected.call.device_id) }}</dd>
              </div>
              <div>
                <dt>大小</dt>
                <dd>{{ formatSize(selected.size_bytes) }}</dd>
              </div>
              <div>
                <dt>对应通话</dt>
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
        title="录音不在当前结果中"
      />
      <StatePanel
        v-else
        state="empty"
        title="选择一条录音"
      />
    </article>
  </section>
</template>

<style scoped>
.recording-list-item {
  cursor: pointer;
}

.recording-list-item__icon {
  display: inline-grid;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.recording-list-item__icon.is-unavailable {
  color: var(--muted);
  background: var(--surface-hover);
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

@media (max-width: 560px) {
  .recording-detail {
    padding: 18px 14px;
  }
}
</style>
