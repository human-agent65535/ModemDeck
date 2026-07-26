<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Cable, CircleAlert, CircleCheck, Clock3 } from '@lucide/vue'
import type { LineSummary, NetworkLineStatus, NetworkUsage } from '../api/types'
import LineTag from './LineTag.vue'
import TrafficUsage from './TrafficUsage.vue'
import { trafficLineState } from './trafficLineState'

const { t } = useI18n()
const props = defineProps<{
  line: LineSummary
  fallback: string
  runtime?: NetworkLineStatus
  today?: NetworkUsage
  month?: NetworkUsage
}>()

const connection = computed(() => trafficLineState(props.runtime))

function primaryIdentity(): string {
  return props.line.phone_number.trim() || props.fallback
}

function secondaryIdentity(): string {
  const operator = props.line.operator.trim()
  return operator && operator !== primaryIdentity() ? operator : ''
}
</script>

<template>
  <article class="traffic-line-card">
    <header>
      <div class="traffic-line-card__identity">
        <LineTag :line="line" :fallback="fallback" />
        <strong>{{ primaryIdentity() }}</strong>
        <small v-if="secondaryIdentity()">{{ secondaryIdentity() }}</small>
      </div>
      <span
        class="traffic-line-card__state"
        :class="`is-${connection.kind}`"
      >
        <CircleCheck v-if="connection.kind === 'connected'" :size="15" />
        <CircleAlert v-else-if="connection.kind === 'error'" :size="15" />
        <Clock3 v-else :size="15" />
        {{ t(connection.labelKey) }}
      </span>
    </header>

    <dl>
      <div>
        <dt><Cable :size="14" /> {{ t('traffic.interface') }}</dt>
        <dd>{{ runtime?.interface || '—' }}</dd>
      </div>
      <div>
        <dt>{{ t('traffic.today') }}</dt>
        <dd>
          <TrafficUsage :rx="today?.rx_bytes || 0" :tx="today?.tx_bytes || 0" />
        </dd>
      </div>
      <div>
        <dt>{{ t('traffic.month') }}</dt>
        <dd>
          <TrafficUsage :rx="month?.rx_bytes || 0" :tx="month?.tx_bytes || 0" />
        </dd>
      </div>
    </dl>
  </article>
</template>

<style scoped>
.traffic-line-card {
  min-width: 0;
  padding: 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.traffic-line-card > header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}

.traffic-line-card__identity {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 4px 8px;
}

.traffic-line-card__identity strong,
.traffic-line-card__identity small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.traffic-line-card__identity strong {
  align-self: center;
  font-size: 14px;
}

.traffic-line-card__identity small {
  grid-column: 1 / -1;
  color: var(--muted);
  font-size: 12px;
}

.traffic-line-card__state {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}

.traffic-line-card__state.is-connected {
  color: var(--accent-strong);
}

.traffic-line-card__state.is-error {
  color: var(--danger);
}

.traffic-line-card dl {
  display: grid;
  gap: 10px;
  margin: 14px 0 0;
}

.traffic-line-card dl > div {
  display: grid;
  min-width: 0;
  align-items: center;
  grid-template-columns: 64px minmax(0, 1fr);
  gap: 10px;
}

.traffic-line-card dt {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
}

.traffic-line-card dd {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 430px) {
  .traffic-line-card > header {
    align-items: stretch;
    flex-direction: column;
  }

  .traffic-line-card__state {
    align-self: flex-start;
  }
}
</style>
