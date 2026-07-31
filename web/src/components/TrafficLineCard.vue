<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowDown,
  ArrowUp,
  CircleAlert,
  CircleCheck,
  Clock3,
  Gauge,
  Network
} from '@lucide/vue'
import type { LineSummary, NetworkLineStatus, NetworkUsage } from '../api/types'
import {
  calculateNetworkRate,
  formatNetworkRate,
  type NetworkCounterSample,
  type NetworkRate
} from '../utils/networkRate'
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
  bootEpoch?: string
  observedAt?: string
}>()

const connection = computed(() => trafficLineState(props.runtime))
const rate = ref<NetworkRate>()
let previousSample: NetworkCounterSample | undefined

watch(
  () => [
    props.bootEpoch,
    props.observedAt,
    props.runtime?.connected,
    props.runtime?.interface,
    props.runtime?.rx_bytes,
    props.runtime?.tx_bytes
  ],
  () => {
    if (!props.runtime || !props.observedAt) {
      rate.value = undefined
      previousSample = undefined
      return
    }
    const sample: NetworkCounterSample = {
      bootEpoch: props.bootEpoch || '',
      observedAt: props.observedAt,
      interface: props.runtime.interface,
      connected: props.runtime.connected,
      rxBytes: props.runtime.rx_bytes,
      txBytes: props.runtime.tx_bytes
    }
    rate.value = calculateNetworkRate(previousSample, sample)
    previousSample = sample
  },
  { immediate: true }
)

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
        <dt><Network :size="14" /> {{ t('traffic.ipAddress') }}</dt>
        <dd :title="runtime?.addresses.join('、')">
          {{ runtime?.addresses.join(' · ') || '—' }}
        </dd>
      </div>
      <div>
        <dt><Gauge :size="14" /> {{ t('traffic.currentRate') }}</dt>
        <dd class="traffic-line-card__rate">
          <span :title="t('traffic.downloadRate')">
            <ArrowDown :size="14" />
            {{ rate ? formatNetworkRate(rate.rxBytesPerSecond) : '—' }}
          </span>
          <span :title="t('traffic.uploadRate')">
            <ArrowUp :size="14" />
            {{ rate ? formatNetworkRate(rate.txBytesPerSecond) : '—' }}
          </span>
        </dd>
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
  container-type: inline-size;
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
  margin: 14px 0 0;
  overflow: hidden;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.traffic-line-card dl > div {
  display: flex;
  min-width: 0;
  min-height: 72px;
  flex-direction: column;
  justify-content: center;
  gap: 7px;
  padding: 11px 12px;
}

.traffic-line-card dl > div:nth-child(even) {
  border-left: 1px solid var(--border);
}

.traffic-line-card dl > div:nth-child(n + 3) {
  border-top: 1px solid var(--border);
}

.traffic-line-card dt {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}

.traffic-line-card dd {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.traffic-line-card__rate {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 10px;
}

.traffic-line-card__rate span {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}

.traffic-line-card__rate span:first-child svg {
  color: var(--blue);
}

.traffic-line-card__rate span:last-child svg {
  color: var(--accent);
}

@container (max-width: 280px) {
  .traffic-line-card > header {
    align-items: stretch;
    flex-direction: column;
  }

  .traffic-line-card__state {
    align-self: flex-start;
  }

  .traffic-line-card dl {
    grid-template-columns: minmax(0, 1fr);
  }

  .traffic-line-card dl > div:nth-child(even) {
    border-left: 0;
  }

  .traffic-line-card dl > div:nth-child(n + 2) {
    border-top: 1px solid var(--border);
  }
}
</style>
