<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Waypoints } from '@lucide/vue'
import type {
  LineSummary,
  NetworkUsage,
  ProxyInstance
} from '../api/types'
import LineTag from '../components/LineTag.vue'
import PageContentFrame from '../components/PageContentFrame.vue'
import ProxyCard from '../components/ProxyCard.vue'
import ProxyEditorModal from '../components/ProxyEditorModal.vue'
import StatePanel from '../components/StatePanel.vue'
import TrafficLineCard from '../components/TrafficLineCard.vue'
import TrafficSummary from '../components/TrafficSummary.vue'
import { requestConfirmation } from '../state/confirmation'
import { showSuccess } from '../state/feedback'
import {
  loadNetwork,
  networkState,
  removeProxy,
  saveProxy,
  setProxyEnabled,
  type ProxyDraft
} from '../state/network'
import { bootstrapResource, lineLabel, loadBootstrap } from '../state/workspace'

const { t } = useI18n()
const selectedLineID = ref('all')
const editorOpen = ref(false)
const editorProxy = ref<ProxyInstance>()

const lines = computed(() => bootstrapResource.data?.lines || [])
const snapshot = computed(() => networkState.snapshot)
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const editorInitialLineID = computed(() =>
  selectedLineID.value === 'all' ? defaultLineID.value : selectedLineID.value
)

function stableLineID(line: LineSummary): string {
  return line.id.trim()
}

function lineForID(lineID: string): LineSummary | undefined {
  return lines.value.find(line => stableLineID(line) === lineID)
}

function lineFallback(line: LineSummary | undefined, lineID = ''): string {
  if (line) return lineLabel(line)
  return lineID.trim()
    ? t('traffic.lineFallback', { suffix: lineID.trim().slice(-4) })
    : t('traffic.unknownLine')
}

function usageFor(
  period: 'today' | 'month',
  scopeKind: NetworkUsage['scope_kind'],
  scopeID: string
): NetworkUsage | undefined {
  const usage =
    period === 'today' ? snapshot.value?.today_usage : snapshot.value?.month_usage
  return usage?.find(item => item.scope_kind === scopeKind && item.scope_id === scopeID)
}

const visibleLines = computed(() =>
  selectedLineID.value === 'all'
    ? lines.value
    : lines.value.filter(line => stableLineID(line) === selectedLineID.value)
)

const visibleLineIDs = computed(
  () => new Set(visibleLines.value.map(stableLineID))
)

const visibleProxies = computed(() =>
  selectedLineID.value === 'all'
    ? networkState.proxies
    : networkState.proxies.filter(proxy => proxy.line_id === selectedLineID.value)
)

const connectedLineCount = computed(
  () =>
    snapshot.value?.lines.filter(
      line => visibleLineIDs.value.has(line.line_id) && line.connected
    ).length || 0
)

const runningProxyCount = computed(
  () =>
    snapshot.value?.proxies.filter(
      proxy =>
        visibleProxies.value.some(item => item.id === proxy.id) &&
        proxy.state === 'running'
    ).length || 0
)

function periodTotal(period: 'today' | 'month'): number {
  if (selectedLineID.value === 'all') {
    const total =
      period === 'today' ? snapshot.value?.today_total : snapshot.value?.month_total
    return (total?.rx_bytes || 0) + (total?.tx_bytes || 0)
  }
  const usage =
    period === 'today' ? snapshot.value?.today_usage : snapshot.value?.month_usage
  return (usage || [])
    .filter(item => item.scope_kind === 'line' && visibleLineIDs.value.has(item.scope_id))
    .reduce((total, item) => total + item.rx_bytes + item.tx_bytes, 0)
}

const todayTotal = computed(() => periodTotal('today'))
const monthTotal = computed(() => periodTotal('month'))
const synchronizationNotice = computed(() => {
  const current = snapshot.value
  if (!current) return ''

  const notices: string[] = []
  if (current.stale) notices.push(t('traffic.runtimeStale'))
  if (current.apply_exhausted) {
    notices.push(t('traffic.syncExhausted'))
  } else if (current.apply_pending) {
    switch (current.apply_status) {
      case 'agent_unavailable':
        notices.push(t('traffic.agentUnavailable'))
        break
      case 'agent_rejected':
        notices.push(t('traffic.agentRejected'))
        break
      case 'runtime_unavailable':
        notices.push(t('traffic.runtimeUnavailable'))
        break
      default:
        notices.push(t('traffic.syncPending'))
    }
  }
  return notices.join('；')
})

function lineRuntime(line: LineSummary) {
  const id = stableLineID(line)
  return snapshot.value?.lines.find(item => item.line_id === id)
}

function proxyRuntime(proxy: ProxyInstance) {
  return snapshot.value?.proxies.find(item => item.id === proxy.id)
}

function selectLine(lineID: string): void {
  selectedLineID.value = lineID
}

function openCreate(): void {
  if (networkState.busyID) return
  networkState.error = ''
  editorProxy.value = undefined
  editorOpen.value = true
}

function openEdit(proxy: ProxyInstance): void {
  if (networkState.busyID) return
  networkState.error = ''
  editorProxy.value = proxy
  editorOpen.value = true
}

function closeEditor(): void {
  if (networkState.busyID) return
  editorOpen.value = false
  editorProxy.value = undefined
}

async function submitProxy(draft: ProxyDraft): Promise<void> {
  const line = lineForID(draft.line_id)
  const saved = await saveProxy(
    draft,
    lineFallback(line, draft.line_id),
    editorProxy.value
  )
  if (saved) {
    closeEditor()
    showSuccess(t('common.saved'))
  }
}

async function toggleProxy(proxy: ProxyInstance, enabled: boolean): Promise<void> {
  await setProxyEnabled(proxy, enabled)
}

async function deleteProxy(proxy: ProxyInstance): Promise<void> {
  const protocol = proxy.mode === 'http' ? 'HTTP CONNECT' : 'SOCKS5'
  const confirmed = await requestConfirmation({
    title: t('traffic.deleteProxyTitle', { protocol }),
    message: t('traffic.deleteProxyMessage'),
    confirmLabel: t('common.delete'),
    tone: 'danger'
  })
  if (!confirmed) return
  await removeProxy(proxy)
}

watch(lines, current => {
  if (
    selectedLineID.value !== 'all' &&
    !current.some(line => stableLineID(line) === selectedLineID.value)
  ) {
    selectedLineID.value = 'all'
  }
})

onMounted(() => {
  void Promise.all([loadBootstrap(true), loadNetwork(true)])
})
</script>

<template>
  <section class="traffic-page">
    <header class="traffic-page__header">
      <h1>{{ t('shell.traffic') }}</h1>
    </header>

    <StatePanel
      v-if="networkState.status === 'loading' && !snapshot"
      state="loading"
      :title="t('traffic.loading')"
    />
    <StatePanel
      v-else-if="networkState.status === 'forbidden'"
      state="forbidden"
      :title="t('traffic.forbidden')"
      :detail="networkState.error"
    />
    <StatePanel
      v-else-if="networkState.status === 'error' && !snapshot"
      state="error"
      :title="t('traffic.loadFailed')"
      :detail="networkState.error"
      retryable
      @retry="loadNetwork(true)"
    />

    <div v-else class="traffic-page__scroll">
      <PageContentFrame mode="dashboard" class="traffic-page__content">
        <div
          v-if="snapshot && !snapshot.available"
          class="traffic-banner is-warning"
          role="status"
        >
          {{ snapshot.unavailable_reason || t('traffic.serviceUnavailable') }}
        </div>
        <div
          v-if="synchronizationNotice"
          class="traffic-banner"
          :class="snapshot?.apply_exhausted ? 'is-error' : 'is-warning'"
          :role="snapshot?.apply_exhausted ? 'alert' : 'status'"
        >
          {{ synchronizationNotice }}
        </div>
        <div v-if="networkState.error" class="traffic-banner is-error" role="alert">
          {{ networkState.error }}
        </div>
        <div v-else-if="networkState.notice" class="traffic-banner" role="status">
          {{ networkState.notice }}
        </div>

        <TrafficSummary
          :today-bytes="todayTotal"
          :month-bytes="monthTotal"
          :connected-lines="connectedLineCount"
          :total-lines="visibleLines.length"
          :running-proxies="runningProxyCount"
          :total-proxies="visibleProxies.length"
        />

        <nav class="traffic-line-filter" :aria-label="t('traffic.filterLines')">
          <button
            type="button"
            :class="{ 'is-selected': selectedLineID === 'all' }"
            :aria-pressed="selectedLineID === 'all'"
            @click="selectLine('all')"
          >
            {{ t('traffic.allLines') }}
          </button>
          <button
            v-for="line in lines"
            :key="stableLineID(line)"
            type="button"
            :class="{ 'is-selected': selectedLineID === stableLineID(line) }"
            :aria-pressed="selectedLineID === stableLineID(line)"
            @click="selectLine(stableLineID(line))"
          >
            <LineTag :line="line" :fallback="lineFallback(line)" />
          </button>
        </nav>

        <section class="traffic-section">
          <header>
            <div>
              <h2>{{ t('traffic.lines') }}</h2>
              <span>{{ visibleLines.length }}</span>
            </div>
          </header>
          <div v-if="visibleLines.length" class="traffic-line-grid">
            <TrafficLineCard
              v-for="line in visibleLines"
              :key="stableLineID(line)"
              :line="line"
              :fallback="lineFallback(line)"
              :runtime="lineRuntime(line)"
              :boot-epoch="snapshot?.boot_epoch"
              :observed-at="snapshot?.observed_at"
              :today="usageFor('today', 'line', stableLineID(line))"
              :month="usageFor('month', 'line', stableLineID(line))"
            />
          </div>
          <StatePanel v-else state="empty" :title="t('traffic.noLines')" />
        </section>

        <section class="traffic-section">
          <header>
            <div>
              <h2>{{ t('traffic.proxies') }}</h2>
              <span>{{ visibleProxies.length }}</span>
            </div>
          </header>
          <div class="proxy-grid">
            <button
              class="proxy-add-card"
              type="button"
              :disabled="lines.length === 0 || Boolean(networkState.busyID)"
              @click="openCreate"
            >
              <span><Plus :size="22" /></span>
              <strong>{{ t('traffic.addProxy') }}</strong>
            </button>

            <ProxyCard
              v-for="proxy in visibleProxies"
              :key="proxy.id"
              :proxy="proxy"
              :line="lineForID(proxy.line_id)"
              :fallback="lineFallback(lineForID(proxy.line_id), proxy.line_id)"
              :runtime="proxyRuntime(proxy)"
              :today="usageFor('today', 'proxy', proxy.id)"
              :month="usageFor('month', 'proxy', proxy.id)"
              :busy="Boolean(networkState.busyID)"
              @edit="openEdit"
              @toggle="toggleProxy"
              @remove="deleteProxy"
            />
          </div>
          <div v-if="visibleProxies.length === 0" class="proxy-empty">
            <Waypoints :size="18" />
            {{ t('traffic.noProxies') }}
          </div>
        </section>
      </PageContentFrame>
    </div>

    <ProxyEditorModal
      :open="editorOpen"
      :proxy="editorProxy"
      :lines="lines"
      :initial-line-id="editorInitialLineID"
      :busy="Boolean(networkState.busyID)"
      :error="networkState.error"
      @close="closeEditor"
      @save="submitProxy"
    />
  </section>
</template>

<style scoped>
.traffic-page {
  display: flex;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  background: var(--background);
}

.traffic-page__header {
  display: flex;
  min-height: 64px;
  flex: 0 0 64px;
  align-items: center;
  padding: 0 24px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.traffic-page__header h1 {
  font-size: 21px;
}

.traffic-page > :deep(.state-panel) {
  flex: 1;
}

.traffic-page__scroll {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
}

.traffic-page__content {
  display: grid;
  gap: 22px;
  padding: 22px 24px 34px;
}

.traffic-banner {
  padding: 10px 12px;
  color: var(--accent-strong);
  font-size: 12px;
  background: var(--accent-soft);
  border: 1px solid #b8ddd5;
  border-radius: 6px;
}

.traffic-banner.is-warning {
  color: #7a4b00;
  background: #fff6dd;
  border-color: #e9cf93;
}

.traffic-banner.is-error {
  color: var(--danger);
  background: var(--danger-soft);
  border-color: #efc3ca;
}

.traffic-line-filter {
  display: flex;
  min-width: 0;
  gap: 6px;
  padding-bottom: 2px;
  overflow-x: auto;
}

.traffic-line-filter > button {
  display: inline-flex;
  min-height: 34px;
  flex: 0 0 auto;
  align-items: center;
  padding: 0 11px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.traffic-line-filter > button:hover {
  color: var(--text);
  border-color: var(--border-strong);
}

.traffic-line-filter > button.is-selected {
  color: var(--accent-strong);
  background: var(--surface-selected);
  border-color: #aed8cf;
}

.traffic-line-filter :deep(.line-tag) {
  max-width: 130px;
}

.traffic-section {
  display: grid;
  min-width: 0;
  gap: 12px;
}

.traffic-section > header {
  display: flex;
  min-height: 30px;
  align-items: center;
  justify-content: space-between;
}

.traffic-section > header > div {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.traffic-section h2 {
  font-size: 16px;
}

.traffic-section > header span {
  color: var(--muted);
  font-size: 12px;
}

.traffic-line-grid {
  display: grid;
  min-width: 0;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(320px, 420px));
  justify-content: start;
}

.traffic-line-grid > :deep(.traffic-line-card) {
  width: 100%;
  max-width: 420px;
}

.proxy-grid {
  display: grid;
  min-width: 0;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(min(310px, 100%), 420px));
  justify-content: start;
}

.proxy-grid > :deep(.proxy-card),
.proxy-add-card {
  width: 100%;
  max-width: 420px;
}

.proxy-add-card {
  display: grid;
  min-height: 270px;
  place-content: center;
  gap: 10px;
  color: var(--muted);
  background: var(--surface);
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
}

.proxy-add-card:hover:not(:disabled) {
  color: var(--accent-strong);
  background: var(--surface-selected);
  border-color: var(--accent);
}

.proxy-add-card > span {
  display: inline-grid;
  width: 42px;
  height: 42px;
  place-items: center;
  justify-self: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 7px;
}

.proxy-add-card strong {
  font-size: 13px;
}

.proxy-empty {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 12px;
}

@media (max-width: 600px) {
  .traffic-page__header {
    display: none;
  }

  .traffic-page__content {
    gap: 18px;
    padding: 16px var(--space-4) 28px;
  }

  .traffic-line-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .traffic-line-filter > button {
    min-height: var(--touch-target);
  }

  .proxy-add-card {
    min-height: 56px;
    grid-template-columns: 36px minmax(0, 1fr);
    align-items: center;
    order: 2;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    text-align: left;
  }

  .proxy-add-card > span {
    width: 36px;
    height: 36px;
    justify-self: start;
  }
}
</style>
