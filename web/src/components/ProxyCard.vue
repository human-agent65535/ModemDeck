<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CircleAlert,
  CircleCheck,
  CircleHelp,
  Clock3,
  Pencil,
  Power,
  Trash2
} from '@lucide/vue'
import type {
  LineSummary,
  NetworkProxyStatus,
  NetworkUsage,
  ProxyInstance
} from '../api/types'
import { lineTagLine } from '../utils/lineIdentity'
import LineTag from './LineTag.vue'
import TrafficUsage from './TrafficUsage.vue'

const { t } = useI18n()
const props = defineProps<{
  proxy: ProxyInstance
  line?: LineSummary
  fallback: string
  runtime?: NetworkProxyStatus
  today?: NetworkUsage
  month?: NetworkUsage
  busy: boolean
}>()

defineEmits<{
  edit: [proxy: ProxyInstance]
  toggle: [proxy: ProxyInstance, enabled: boolean]
  remove: [proxy: ProxyInstance]
}>()

type DisplayRuntimeState = NetworkProxyStatus['state'] | 'unknown'

const pendingDelete = computed(() => props.proxy.apply_state === 'pending_delete')

function runtimeState(): DisplayRuntimeState {
  return props.runtime?.state || 'unknown'
}

function stateLabel(): string {
  switch (runtimeState()) {
    case 'running':
      return t('proxy.running')
    case 'waiting_for_bearer':
      return t('proxy.waitingForLine')
    case 'error':
      return t('proxy.configurationError')
    case 'unknown':
      return t('proxy.runtimeUnknown')
    default:
      return t('proxy.disabled')
  }
}

function applyStateLabel(): string {
  switch (props.proxy.apply_state) {
    case 'pending_create':
      return t('proxy.pendingCreate')
    case 'pending_update':
      return t('proxy.pendingUpdate')
    case 'pending_delete':
      return t('proxy.deleting')
    default:
      return ''
  }
}
</script>

<template>
  <article class="proxy-card">
    <header>
      <div>
        <LineTag
          :line="lineTagLine(line, proxy.line_id)"
          :fallback="fallback"
        />
        <strong>{{ proxy.mode === 'http' ? 'HTTP CONNECT' : 'SOCKS5' }}</strong>
      </div>
      <button
        class="proxy-switch"
        :class="{ 'is-enabled': proxy.enabled }"
        type="button"
        role="switch"
        :aria-checked="proxy.enabled"
        :aria-label="proxy.enabled ? t('proxy.disable') : t('proxy.enable')"
        :title="pendingDelete ? t('proxy.deleting') : proxy.enabled ? t('proxy.disable') : t('proxy.enable')"
        :disabled="busy || pendingDelete"
        @click="$emit('toggle', proxy, !proxy.enabled)"
      >
        <span />
      </button>
    </header>

    <div class="proxy-card__status" :class="`is-${runtimeState()}`">
      <CircleCheck v-if="runtimeState() === 'running'" :size="15" />
      <CircleAlert v-else-if="runtimeState() === 'error'" :size="15" />
      <Power v-else-if="runtimeState() === 'disabled'" :size="15" />
      <Clock3 v-else-if="runtimeState() === 'waiting_for_bearer'" :size="15" />
      <CircleHelp v-else :size="15" />
      <span class="proxy-card__runtime-label">{{ stateLabel() }}</span>
      <span v-if="applyStateLabel()" class="proxy-card__apply-state">
        {{ applyStateLabel() }}
      </span>
      <small
        v-if="runtimeState() === 'error' && runtime?.last_error"
        class="proxy-card__runtime-error"
      >
        {{ runtime.last_error }}
      </small>
    </div>

    <dl>
      <div>
        <dt>{{ t('traffic.listen') }}</dt>
        <dd>{{ proxy.listen_address }}:{{ proxy.listen_port }}</dd>
      </div>
      <div>
        <dt>{{ t('traffic.interface') }}</dt>
        <dd>{{ runtime?.interface || '—' }}</dd>
      </div>
      <div>
        <dt>{{ t('traffic.today') }}</dt>
        <dd><TrafficUsage :rx="today?.rx_bytes || 0" :tx="today?.tx_bytes || 0" /></dd>
      </div>
      <div>
        <dt>{{ t('traffic.month') }}</dt>
        <dd><TrafficUsage :rx="month?.rx_bytes || 0" :tx="month?.tx_bytes || 0" /></dd>
      </div>
      <div>
        <dt>{{ t('traffic.connections') }}</dt>
        <dd>
          {{
            runtime
              ? `${t('traffic.activeConnections', { count: runtime.active_connections })} · ${t('traffic.cumulativeConnections', { count: runtime.connections })}`
              : '—'
          }}
        </dd>
      </div>
    </dl>

    <footer>
      <button
        class="icon-button"
        type="button"
        :title="t('proxy.edit')"
        :aria-label="t('proxy.edit')"
        :disabled="busy || pendingDelete"
        @click="$emit('edit', proxy)"
      >
        <Pencil :size="17" />
      </button>
      <button
        class="icon-button is-danger"
        type="button"
        :title="pendingDelete ? t('proxy.deleting') : t('proxy.delete')"
        :aria-label="t('proxy.delete')"
        :disabled="busy || pendingDelete"
        @click="$emit('remove', proxy)"
      >
        <Trash2 :size="17" />
      </button>
    </footer>
  </article>
</template>

<style scoped>
.proxy-card {
  display: flex;
  min-width: 0;
  min-height: 270px;
  flex-direction: column;
  padding: 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.proxy-card > header {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.proxy-card > header > div {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.proxy-card > header strong {
  overflow: hidden;
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.proxy-switch {
  position: relative;
  width: 38px;
  height: 22px;
  flex: 0 0 38px;
  padding: 0;
  background: var(--border-strong);
  border-radius: 11px;
  transition: background 140ms ease;
}

.proxy-switch > span {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 16px;
  height: 16px;
  background: #ffffff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 24%);
  transition: transform 140ms ease;
}

.proxy-switch.is-enabled {
  background: var(--accent);
}

.proxy-switch.is-enabled > span {
  transform: translateX(16px);
}

.proxy-card__status {
  display: flex;
  min-width: 0;
  min-height: 38px;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin: 14px 0 2px;
  padding: 8px 10px;
  color: var(--muted);
  font-size: 12px;
  background: var(--surface-subtle);
  border-radius: 6px;
}

.proxy-card__runtime-label {
  flex: 0 0 auto;
  font-weight: 650;
}

.proxy-card__apply-state {
  margin-left: auto;
  padding: 2px 5px;
  color: #7a4b00;
  font-size: 11px;
  font-weight: 650;
  background: #fff0c2;
  border-radius: 4px;
}

.proxy-card__runtime-error {
  min-width: 0;
  flex: 1 0 100%;
  padding-left: 21px;
  overflow: hidden;
  color: inherit;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.proxy-card__status.is-running {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.proxy-card__status.is-error {
  color: var(--danger);
  background: var(--danger-soft);
}

.proxy-card dl {
  display: grid;
  gap: 9px;
  margin: 12px 0 14px;
}

.proxy-card dl > div {
  display: grid;
  min-width: 0;
  align-items: center;
  grid-template-columns: 84px minmax(0, 1fr);
  gap: 8px;
}

.proxy-card dt {
  color: var(--muted);
  font-size: 12px;
  white-space: nowrap;
}

.proxy-card dd {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.proxy-card > footer {
  display: flex;
  justify-content: flex-end;
  gap: 3px;
  margin-top: auto;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}

.proxy-card > footer .is-danger {
  color: var(--danger);
}
</style>
