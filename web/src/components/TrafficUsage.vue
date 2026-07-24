<script setup lang="ts">
import { ArrowDown, ArrowUp } from '@lucide/vue'

defineProps<{
  rx: number
  tx: number
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
  <span class="traffic-usage">
    <span title="下行">
      <ArrowDown :size="14" aria-hidden="true" />
      {{ formatBytes(rx) }}
    </span>
    <span title="上行">
      <ArrowUp :size="14" aria-hidden="true" />
      {{ formatBytes(tx) }}
    </span>
  </span>
</template>

<style scoped>
.traffic-usage {
  display: inline-flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  color: var(--muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.traffic-usage > span {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 3px;
  white-space: nowrap;
}

.traffic-usage > span:first-child svg {
  color: var(--blue);
}

.traffic-usage > span:last-child svg {
  color: var(--accent);
}
</style>
