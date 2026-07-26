<script setup lang="ts">
import { CalendarDays, ChartNoAxesCombined, RadioTower, Waypoints } from '@lucide/vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
defineProps<{
  todayBytes: number
  monthBytes: number
  connectedLines: number
  totalLines: number
  runningProxies: number
  totalProxies: number
}>()

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
  border-radius: 8px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.traffic-summary > div {
  display: flex;
  min-width: 0;
  min-height: 82px;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
}

.traffic-summary > div + div {
  border-left: 1px solid var(--border);
}

.traffic-summary__icon {
  display: inline-grid;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  place-items: center;
  color: #7a4b00;
  background: #fff2d6;
  border-radius: 7px;
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
  color: #8a3448;
  background: #fae9ed;
}

.traffic-summary small,
.traffic-summary strong {
  display: block;
  min-width: 0;
}

.traffic-summary small {
  margin-bottom: 3px;
  color: var(--muted);
  font-size: 11px;
}

.traffic-summary strong {
  overflow: hidden;
  font-size: 18px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 760px) {
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

@media (max-width: 430px) {
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
    font-size: 15px;
  }
}
</style>
