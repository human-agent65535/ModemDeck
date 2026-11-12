<script setup lang="ts">
import { CalendarDays, ChartNoAxesCombined, RadioTower, Waypoints } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { formatBytes } from '../utils/format'

const { t } = useI18n()
defineProps<{
  todayBytes: number
  monthBytes: number
  connectedLines: number
  totalLines: number
  runningProxies: number
  totalProxies: number
}>()
</script>

<template>
  <section class="traffic-summary" :aria-label="t('traffic.overview')">
    <div>
      <span class="traffic-summary__icon"><ChartNoAxesCombined :size="19" /></span>
      <span>
        <small>{{ t('traffic.todayTraffic') }}</small>
        <strong>{{ formatBytes(todayBytes) }}</strong>
      </span>
    </div>
    <div>
      <span class="traffic-summary__icon is-blue"><CalendarDays :size="19" /></span>
      <span>
        <small>{{ t('traffic.monthTraffic') }}</small>
        <strong>{{ formatBytes(monthBytes) }}</strong>
      </span>
    </div>
    <div>
      <span class="traffic-summary__icon is-green"><RadioTower :size="19" /></span>
      <span>
        <small>{{ t('traffic.connectedLines') }}</small>
        <strong>{{ connectedLines }} / {{ totalLines }}</strong>
      </span>
    </div>
    <div>
      <span class="traffic-summary__icon is-rose"><Waypoints :size="19" /></span>
      <span>
        <small>{{ t('traffic.runningProxies') }}</small>
        <strong>{{ runningProxies }} / {{ totalProxies }}</strong>
      </span>
    </div>
  </section>
</template>

<style scoped>
.traffic-summary {
  display: grid;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.traffic-summary > div {
  display: flex;
  min-width: 0;
  min-height: 82px;
  align-items: center;
  gap: var(--space-3);
  padding: 14px 18px;
}

.traffic-summary > div + div {
  border-left: 1px solid var(--border);
}

.traffic-summary__icon {
  display: inline-grid;
  width: var(--icon-tile-size);
  height: var(--icon-tile-size);
  flex: 0 0 var(--icon-tile-size);
  place-items: center;
  color: var(--warning-strong);
  background: var(--warning-soft);
  border-radius: var(--radius-sm);
}

.traffic-summary__icon.is-blue {
  color: var(--blue);
  background: var(--blue-soft);
}

.traffic-summary__icon.is-green {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.traffic-summary__icon.is-rose {
  color: var(--danger-strong);
  background: var(--danger-soft);
}

.traffic-summary small,
.traffic-summary strong {
  display: block;
  min-width: 0;
}

.traffic-summary small {
  margin-bottom: 3px;
  color: var(--muted);
  font-size: var(--text-2xs);
}

.traffic-summary strong {
  overflow: hidden;
  font-size: var(--text-lg);
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 640px) {
  .traffic-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .traffic-summary > div:nth-child(3) {
    border-left: 0;
  }

  .traffic-summary > div:nth-child(n + 3) {
    border-top: 1px solid var(--border);
  }
}

@media (max-width: 420px) {
  .traffic-summary > div {
    min-height: 72px;
    gap: 9px;
    padding: 11px;
  }

  .traffic-summary__icon {
    width: 32px;
    height: 32px;
    flex-basis: 32px;
  }

  .traffic-summary strong {
    font-size: var(--text-lg);
  }
}
</style>
