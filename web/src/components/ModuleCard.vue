<script setup lang="ts">
import {
  CardSim,
  CircleAlert,
  CircleCheck,
  LoaderCircle,
  MessageSquareText,
  Phone,
  RadioTower,
  Trash2
} from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Device, LineSummary, NetworkLineStatus } from '../api/types'
import { lineHasCallControl, lineLabel } from '../state/workspace'
import { formatDateTime } from '../utils/format'
import {
  isRegisteredNetwork,
  operatorFacts,
  registrationStateLabel
} from '../utils/operatorNetwork'
import SignalBars from './SignalBars.vue'
import { trafficLineState } from './trafficLineState'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    line: LineSummary
    device?: Device
    selected?: boolean
    defaultLine?: boolean
    actions?: boolean
    selectable?: boolean
    deletable?: boolean
    deletePending?: boolean
    flightMode?: boolean
    runtime?: NetworkLineStatus
  }>(),
  {
    device: undefined,
    selected: false,
    defaultLine: false,
    actions: false,
    selectable: true,
    deletable: false,
    deletePending: false,
    flightMode: undefined,
    runtime: undefined
  }
)

const emit = defineEmits<{
  select: []
  makeDefault: []
  delete: []
}>()

const moduleOnly = computed(() => props.line.module_only === true)
const signal = computed(
  () =>
    props.device?.present === false
      ? null
      : props.line.signal_quality ?? props.device?.signal_quality ?? null
)
const flightMode = computed(
  () =>
    !moduleOnly.value &&
    (props.flightMode ??
      (props.line.radio_desired_enabled_known
        ? !props.line.radio_desired_enabled
        : false))
)
const radioWaitingForRegistration = computed(
  () =>
    !flightMode.value &&
    props.line.radio_desired_enabled_known &&
    props.line.radio_desired_enabled &&
    ['disabled', 'disabling', 'enabling', 'initializing'].includes(
      (props.line.state || '').trim().toLocaleLowerCase()
    )
)
const online = computed(
  () =>
    !flightMode.value &&
    !radioWaitingForRegistration.value &&
    isRegisteredNetwork(props.line)
)
const model = computed(
  () => props.line.model || props.device?.model || t('lines.unknownModel')
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
const lastSeen = computed(() => {
  const value = props.device?.last_seen || ''
  return value ? formatDateTime(value) : '—'
})
const networkFacts = computed(() =>
  operatorFacts(
    flightMode.value || radioWaitingForRegistration.value
      ? {
          ...props.line,
          state: 'disabled',
          registration_state_known: false,
          roaming: false
        }
      : props.line,
    '—',
    key => t(key)
  )
)
const stateLabel = computed(() => {
  if (moduleOnly.value && props.device?.present === false) {
    return t('device.disconnected')
  }
  if (moduleOnly.value && !props.device?.sim_inserted) return t('device.noCard')
  if (flightMode.value) return t('device.flightMode')
  if (radioWaitingForRegistration.value) return t('device.radioRecovering')
  const state = (props.line.state || '').toLocaleLowerCase()
  let label = props.line.state || t('lines.unknownState')
  if (state === 'connected') label = t('lines.connected')
  if (state === 'registered') label = t('lines.registered')
  if (state === 'enabled') label = t('lines.enabled')
  if (state === 'searching') label = t('lines.searching')
  if (state === 'disabled') label = t('lines.disabled')
  if (state === 'locked') label = t('lines.simLocked')
  if (state === 'failed') label = t('lines.failed')
  if (['failed', 'locked', 'disabled'].includes(state)) return label
  return registrationStateLabel(props.line, label, key => t(key))
})
const dataConnection = computed(() => {
  if (!props.runtime) return null
  return trafficLineState(props.runtime)
})
</script>

<template>
  <article
    class="module-card"
    :class="{ 'is-selected': selected }"
  >
    <button
      class="module-card__main"
      type="button"
      :aria-label="t('lines.configureModule', { label: lineLabel(line) })"
      :aria-pressed="selected"
      :disabled="!selectable"
      @click="selectable && emit('select')"
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
        <span
          v-if="dataConnection && dataConnection.kind !== 'idle'"
          class="module-card__data-status"
          :class="`is-${dataConnection.kind}`"
        >
          <CircleCheck v-if="dataConnection.kind === 'connected'" :size="13" />
          <CircleAlert v-else :size="13" />
          {{ t(dataConnection.labelKey) }}
        </span>
      </header>

      <dl class="module-card__facts">
        <div v-for="fact in networkFacts" :key="fact.id">
          <dt>{{ fact.label }}</dt>
          <dd>{{ fact.value }}</dd>
        </div>
        <div>
          <dt>{{ t('lines.signal') }}</dt>
          <dd class="module-card__signal-value">
            <SignalBars :value="signal" :flight-mode="flightMode" />
            <span>
              {{
                flightMode
                  ? t('device.flightMode')
                  : signal === null
                    ? '—'
                    : `${signal}%`
              }}
            </span>
          </dd>
        </div>
        <div>
          <dt>{{ t('lines.model') }}</dt>
          <dd>{{ model }}</dd>
        </div>
        <div>
          <dt>{{ t('lines.firmware') }}</dt>
          <dd>{{ firmware || '—' }}</dd>
        </div>
        <div class="is-code">
          <dt>IMEI</dt>
          <dd :title="equipmentIdentifier">{{ equipmentIdentifier || '—' }}</dd>
        </div>
        <div class="is-code">
          <dt>ICCID</dt>
          <dd :title="simIdentifier">{{ simIdentifier || '—' }}</dd>
        </div>
        <div class="is-code">
          <dt>{{ t('lines.port') }}</dt>
          <dd>{{ device?.port || '—' }}</dd>
        </div>
        <div>
          <dt>{{ t('device.lastSeen') }}</dt>
          <dd>
            <time v-if="device?.last_seen" :datetime="device.last_seen">{{ lastSeen }}</time>
            <span v-else>—</span>
          </dd>
        </div>
      </dl>

    </button>

    <footer class="module-card__footer">
      <div class="module-card__capabilities" :aria-label="t('lines.moduleCapabilities')">
        <span :class="{ 'is-enabled': lineHasCallControl(line) }">
          <Phone :size="14" />
          {{ t('lines.callControl') }}
        </span>
        <span :class="{ 'is-enabled': line.capabilities?.messaging }">
          <MessageSquareText :size="14" />
          SMS
        </span>
        <span :class="{ 'is-enabled': line.capabilities?.sim }">
          <CardSim :size="14" />
          SIM
        </span>
      </div>

      <div v-if="deletable" class="module-card__actions">
        <button
          class="module-card__delete-action"
          type="button"
          :title="t('device.deleteModule')"
          :aria-label="t('device.deleteModule')"
          :disabled="deletePending"
          @click="emit('delete')"
        >
          <LoaderCircle v-if="deletePending" class="spin" :size="16" />
          <Trash2 v-else :size="16" />
          <span>{{ t('common.delete') }}</span>
        </button>
      </div>
      <div v-else-if="actions && !moduleOnly" class="module-card__actions">
        <button
          class="module-card__default-action"
          type="button"
          :title="defaultLine ? t('lines.currentDefaultLine') : t('lines.setDefaultLine')"
          :aria-label="defaultLine ? t('lines.currentDefaultLine') : t('lines.setDefaultLine')"
          :aria-pressed="defaultLine"
          :disabled="defaultLine"
          @click="emit('makeDefault')"
        >
          <CircleCheck :size="16" />
          <span>{{ defaultLine ? t('lines.defaultLine') : t('lines.setAsDefault') }}</span>
        </button>
      </div>
      <span
        v-else-if="defaultLine"
        class="module-card__default-status"
        :title="t('lines.currentDefaultLine')"
      >
        <CircleCheck :size="16" />
        {{ t('lines.defaultLine') }}
      </span>
    </footer>
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

.module-card__main:disabled {
  cursor: default;
  opacity: 1;
}

.module-card__main:disabled:hover {
  background: transparent;
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

.module-card__data-status {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 3px;
  font-size: 12px;
  font-weight: 650;
  padding: 3px 6px;
  border-radius: 4px;
}

.module-card__data-status {
  color: var(--muted);
  background: var(--surface-subtle);
}

.module-card__data-status.is-connected {
  color: var(--accent-strong);
  background: var(--accent-soft);
}

.module-card__data-status.is-error {
  color: var(--danger);
  background: var(--danger-soft);
}

.module-card__facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 14px;
  margin: 0;
  padding: 10px;
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 5px;
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

.module-card__signal-value {
  display: flex;
  align-items: center;
  gap: 5px;
}

.module-card__footer {
  display: grid;
  min-height: 48px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: color-mix(in srgb, var(--surface) 88%, transparent);
  border-top: 1px solid var(--border);
}

.module-card.is-selected .module-card__footer {
  background: var(--surface-selected);
}

.module-card__capabilities {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.module-card__capabilities > span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 12px;
  white-space: nowrap;
}

.module-card__capabilities > span.is-enabled {
  color: var(--accent-strong);
}

.module-card__actions {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
}

.module-card__default-action,
.module-card__delete-action,
.module-card__default-status {
  display: inline-flex;
  min-width: 0;
  height: 34px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
  white-space: nowrap;
}

.module-card__default-action {
  padding: 0 8px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  cursor: pointer;
}

.module-card__delete-action {
  padding: 0 8px;
  color: var(--danger);
  background: var(--surface);
  border: 1px solid color-mix(in srgb, var(--danger) 42%, var(--border));
  border-radius: 5px;
  cursor: pointer;
}

.module-card__delete-action:hover:not(:disabled) {
  background: var(--danger-soft);
  border-color: var(--danger);
}

.module-card__delete-action:disabled {
  cursor: wait;
  opacity: 0.65;
}

.module-card__default-action:hover:not(:disabled) {
  background: var(--surface-hover);
  border-color: var(--accent);
}

.module-card__default-action:disabled {
  color: var(--accent-strong);
  cursor: default;
  opacity: 1;
  background: var(--accent-soft);
  border-color: color-mix(in srgb, var(--accent) 48%, var(--border));
}

.module-card__default-status {
  padding: 0 4px;
}

.module-card__actions .icon-button {
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
}

@container (max-width: 350px) {
  .module-card__main {
    padding-inline: 12px;
  }

  .module-card__footer {
    gap: 5px;
    padding-inline: 8px;
  }

  .module-card__capabilities {
    gap: 6px;
  }

  .module-card__default-action {
    padding-inline: 6px;
  }
}

</style>
