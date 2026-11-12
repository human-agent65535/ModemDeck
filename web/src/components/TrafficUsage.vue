<script setup lang="ts">
import { ArrowDown, ArrowUp } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { formatBytes } from '../utils/format'

const { t } = useI18n()
defineProps<{
  rx: number
  tx: number
}>()
</script>

<template>
  <span class="traffic-usage">
    <span :title="t('traffic.download')">
      <ArrowDown :size="14" aria-hidden="true" />
      {{ formatBytes(rx) }}
    </span>
    <span :title="t('traffic.upload')">
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
  font-size: var(--text-xs);
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
