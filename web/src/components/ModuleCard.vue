<script setup lang="ts">
import {
  CardSim,
  Check,
  MessageSquareText,
  Pencil,
  Phone,
  RadioTower,
  Signal,
  Star
} from '@lucide/vue'
import { computed } from 'vue'
import type { Device, LineSummary } from '../api/types'
import { lineLabel } from '../state/workspace'

const props = withDefaults(
  defineProps<{
    line: LineSummary
    device?: Device
    selected?: boolean
    defaultLine?: boolean
    actions?: boolean
    compact?: boolean
  }>(),
  {
    device: undefined,
    selected: false,
    defaultLine: false,
    actions: false,
    compact: false
  }
)

const emit = defineEmits<{
  select: []
  rename: []
  makeDefault: []
}>()

const online = computed(() =>
  ['registered', 'connected', 'enabled', 'searching'].includes(
    (props.line.state || '').toLocaleLowerCase()
  )
)
const signal = computed(
  () => props.line.signal_quality ?? props.device?.signal_quality ?? null
)
const model = computed(
  () => props.line.model || props.device?.model || '未知型号'
)
const firmware = computed(
  () => props.line.firmware || props.device?.firmware || ''
)
const equipmentIdentifier = computed(
  () => props.line.device_imei || props.device?.imei || ''
)
const simIdentifier = computed(
  () => props.line.iccid || props.device?.current_iccid || ''
)
const stateLabel = computed(() => {
  const state = (props.line.state || '').toLocaleLowerCase()
  if (state === 'connected') return '已连接'
  if (state === 'registered') return '已驻网'
  if (state === 'enabled') return '已启用'
  if (state === 'searching') return '搜索网络'
  if (state === 'disabled') return '已停用'
  if (state === 'failed') return '异常'
  return props.line.state || '状态未知'
})
</script>

<template>
  <article
    class="module-card"
    :class="{
      'is-selected': selected,
      'is-compact': compact
    }"
  >
    <button
      class="module-card__main"
      type="button"
      :aria-label="`配置模组 ${lineLabel(line)}`"
      :aria-pressed="selected"
      @click="emit('select')"
    >
      <header>
        <span class="module-card__icon"><RadioTower :size="19" /></span>
        <span class="module-card__identity">
          <strong>{{ lineLabel(line) }}</strong>
          <small>
            <i :class="{ 'is-online': online }" />
            {{ stateLabel }}
          </small>
        </span>
        <span class="module-card__badges">
          <span v-if="selected" class="module-card__current">
            <Check :size="13" />
            当前配置
          </span>
          <span v-if="defaultLine" class="module-card__default">
            <Star :size="13" fill="currentColor" />
            默认线路
          </span>
        </span>
      </header>

      <dl class="module-card__facts">
        <div>
          <dt><Signal :size="13" />运营商</dt>
          <dd>{{ line.operator || '—' }}</dd>
        </div>
        <div>
          <dt>信号</dt>
          <dd>{{ signal === null ? '—' : `${signal}%` }}</dd>
        </div>
        <div v-if="!compact">
          <dt>型号</dt>
          <dd>{{ model }}</dd>
        </div>
        <div v-if="!compact">
          <dt>固件</dt>
          <dd>{{ firmware || '—' }}</dd>
        </div>
        <div v-if="!compact" class="is-code">
          <dt>IMEI</dt>
          <dd :title="equipmentIdentifier">{{ equipmentIdentifier || '—' }}</dd>
        </div>
        <div v-if="!compact" class="is-code is-wide">
          <dt>ICCID</dt>
          <dd :title="simIdentifier">{{ simIdentifier || '—' }}</dd>
        </div>
        <div v-if="!compact" class="is-code">
          <dt>端口</dt>
          <dd>{{ device?.port || '—' }}</dd>
        </div>
      </dl>

      <footer>
        <span :class="{ 'is-enabled': line.capabilities?.voice }">
          <Phone :size="14" />
          通话
        </span>
        <span :class="{ 'is-enabled': line.capabilities?.messaging }">
          <MessageSquareText :size="14" />
          SMS
        </span>
        <span :class="{ 'is-enabled': line.capabilities?.sim }">
          <CardSim :size="14" />
          SIM
        </span>
      </footer>
    </button>

    <div v-if="actions" class="module-card__actions">
      <button
        class="icon-button"
        type="button"
        title="设为默认线路"
        aria-label="设为默认线路"
        :disabled="defaultLine"
        @click="emit('makeDefault')"
      >
        <Star :size="17" :fill="defaultLine ? 'currentColor' : 'none'" />
      </button>
      <button
        class="icon-button"
        type="button"
        title="修改模组名称"
        aria-label="修改模组名称"
        @click="emit('rename')"
      >
        <Pencil :size="17" />
      </button>
    </div>
  </article>
</template>

<style scoped>
.module-card {
  position: relative;
  min-width: 0;
  container-type: inline-size;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
  transition:
    border-color 150ms ease,
    box-shadow 150ms ease,
    background 150ms ease;
}

.module-card.is-selected {
  border-color: var(--accent);
  background: var(--surface-selected);
  box-shadow:
    inset 4px 0 0 var(--accent),
    0 0 0 1px var(--accent);
}

.module-card__main {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 12px;
  padding: 15px;
  color: inherit;
  text-align: left;
  background: transparent;
  cursor: pointer;
}

.module-card__main:hover {
  background: var(--surface-hover);
}

.module-card.is-selected .module-card__main:hover {
  background: var(--surface-selected);
}

.module-card header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 10px;
}

.module-card__icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  color: var(--blue);
  background: var(--blue-soft);
  border-radius: 6px;
}

.module-card__identity {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}

.module-card__identity strong,
.module-card__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.module-card__identity strong {
  font-size: 14px;
}

.module-card__identity small {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
}

.module-card__identity i {
  width: 7px;
  height: 7px;
  flex: 0 0 7px;
  background: #a9b1bd;
  border-radius: 50%;
}

.module-card__identity i.is-online {
  background: #18a46f;
}

.module-card__badges {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.module-card__current,
.module-card__default {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 3px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
}

.module-card__current {
  padding: 3px 6px;
  background: var(--accent-soft);
  border-radius: 4px;
}

.module-card__facts {
  padding: 10px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.module-card__facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 14px;
  margin: 0;
}

.module-card__facts div {
  min-width: 0;
}

.module-card dt {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
}

.module-card dd {
  margin: 3px 0 0;
  overflow-wrap: anywhere;
  font-size: 12px;
  line-height: 1.35;
}

.module-card .is-code dd {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.module-card footer {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.module-card footer span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 12px;
}

.module-card footer span.is-enabled {
  color: var(--accent-strong);
}

.module-card__actions {
  display: flex;
  min-height: 40px;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
  padding: 4px 8px;
  border-top: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 88%, transparent);
}

.module-card.is-selected .module-card__actions {
  background: var(--surface-selected);
}

.module-card.is-compact .module-card__main {
  gap: 9px;
  padding: 12px;
}

@media (max-width: 560px) {
  .module-card__badges {
    align-items: flex-start;
  }
}

@container (min-width: 410px) {
  .module-card__facts {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .module-card__facts .is-wide {
    grid-column: span 2;
  }
}
</style>
