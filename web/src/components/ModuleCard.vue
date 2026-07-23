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
</script>

<template>
  <article
    class="module-card"
    :class="{
      'is-selected': selected,
      'is-compact': compact
    }"
  >
    <button class="module-card__main" type="button" @click="emit('select')">
      <header>
        <span class="module-card__icon"><RadioTower :size="19" /></span>
        <span class="module-card__identity">
          <strong>{{ lineLabel(line) }}</strong>
          <small>
            <i :class="{ 'is-online': online }" />
            {{ online ? '在线' : line.state || '状态未知' }}
          </small>
        </span>
        <span v-if="defaultLine" class="module-card__default">
          <Check :size="13" />
          默认
        </span>
      </header>

      <div class="module-card__radio">
        <span><Signal :size="16" />{{ line.operator || '运营商未知' }}</span>
        <strong>{{ signal === null ? '—' : `${signal}%` }}</strong>
      </div>

      <dl v-if="!compact">
        <div>
          <dt>型号</dt>
          <dd>{{ model }}</dd>
        </div>
        <div>
          <dt>端口</dt>
          <dd>{{ device?.port || '—' }}</dd>
        </div>
        <div>
          <dt>ICCID</dt>
          <dd>{{ line.iccid || device?.current_iccid || '—' }}</dd>
        </div>
        <div>
          <dt>固件</dt>
          <dd>{{ firmware || '—' }}</dd>
        </div>
      </dl>

      <footer>
        <span :class="{ 'is-enabled': line.capabilities?.voice }">
          <Phone :size="14" />
          Voice
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
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
}

.module-card.is-selected {
  border-color: var(--accent);
  box-shadow: inset 3px 0 0 var(--accent);
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
}

.module-card__main:hover {
  background: var(--surface-hover);
}

.module-card header {
  display: flex;
  min-width: 0;
  align-items: center;
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
  font-size: 13px;
}

.module-card__identity small {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 10px;
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

.module-card__default {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 3px;
  color: var(--accent-strong);
  font-size: 10px;
  font-weight: 650;
}

.module-card__radio {
  display: flex;
  min-width: 0;
  min-height: 38px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 10px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
}

.module-card__radio span {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  color: var(--muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.module-card__radio strong {
  flex: 0 0 auto;
  color: var(--accent-strong);
  font-size: 11px;
}

.module-card dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 14px;
  margin: 0;
}

.module-card dl div {
  min-width: 0;
}

.module-card dt {
  color: var(--muted);
  font-size: 9px;
}

.module-card dd {
  margin: 2px 0 0;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  font-size: 10px;
}

.module-card footer span.is-enabled {
  color: var(--accent-strong);
}

.module-card__actions {
  position: absolute;
  right: 8px;
  bottom: 7px;
  display: flex;
  gap: 2px;
  padding-left: 12px;
  background: var(--surface);
}

.module-card.is-compact .module-card__main {
  gap: 9px;
  padding: 12px;
}

.module-card.is-compact .module-card__actions {
  display: none;
}

@media (max-width: 560px) {
  .module-card dl {
    grid-template-columns: 1fr;
  }
}
</style>
