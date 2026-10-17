<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

export type TrafficShareSegment = {
  id: string
  label: string
  color: string
  rx: number
  tx: number
}

const props = defineProps<{
  segments: TrafficShareSegment[]
}>()

const { t } = useI18n()

// The API reports per-line totals only (no daily history), so the chart shows
// each line's share of this month's traffic with its download/upload split.
const total = computed(() =>
  props.segments.reduce((sum, segment) => sum + segment.rx + segment.tx, 0)
)
const rows = computed(() =>
  props.segments
    .map(segment => {
      const bytes = segment.rx + segment.tx
      return {
        ...segment,
        bytes,
        share: total.value > 0 ? bytes / total.value : 0
      }
    })
    .sort((left, right) => right.bytes - left.bytes)
)

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let value = bytes / 1024
  let unit = units[0]
  for (let index = 1; index < units.length && value >= 1024; index += 1) {
    value /= 1024
    unit = units[index]
  }
  const precision = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${unit}`
}

function percent(share: number): string {
  return `${Math.round(share * 100)}%`
}
</script>

<template>
  <section v-if="total > 0" class="traffic-share" :aria-labelledby="'traffic-share-title'">
    <header class="traffic-share__header">
      <h2 id="traffic-share-title">{{ t('traffic.shareTitle') }}</h2>
      <span class="num">{{ formatBytes(total) }}</span>
    </header>
    <div class="traffic-share__bar" aria-hidden="true">
      <span
        v-for="row in rows"
        :key="row.id"
        class="traffic-share__segment"
        :style="{ flexGrow: row.bytes, backgroundColor: row.color }"
      />
    </div>
    <ul class="traffic-share__legend">
      <li v-for="row in rows" :key="row.id">
        <span class="traffic-share__dot" :style="{ backgroundColor: row.color }" aria-hidden="true" />
        <strong>{{ row.label }}</strong>
        <span class="num traffic-share__value">
          {{ formatBytes(row.bytes) }} · {{ percent(row.share) }}
        </span>
        <small class="num">
          {{ t('traffic.downloadShort', { value: formatBytes(row.rx) }) }}
          · {{ t('traffic.uploadShort', { value: formatBytes(row.tx) }) }}
        </small>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.traffic-share {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.traffic-share__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
}

.traffic-share__header h2 {
  font-size: var(--text-base);
}

.traffic-share__header span {
  color: var(--muted);
  font-size: var(--text-sm);
}

.traffic-share__bar {
  display: flex;
  height: 12px;
  gap: 2px;
  overflow: hidden;
  background: var(--control-muted);
  border-radius: var(--radius-pill);
}

.traffic-share__segment {
  min-width: 4px;
  flex-basis: 0;
}

.traffic-share__legend {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  list-style: none;
}

.traffic-share__legend li {
  display: grid;
  align-items: center;
  column-gap: var(--space-2);
  grid-template-columns: 8px minmax(0, 1fr) auto;
}

.traffic-share__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.traffic-share__legend strong {
  overflow: hidden;
  font-size: var(--text-sm);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.traffic-share__value {
  color: var(--text);
  font-size: var(--text-sm);
}

.traffic-share__legend small {
  grid-column: 2 / -1;
  color: var(--muted);
  font-size: var(--text-xs);
}
</style>
