<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CassetteTape,
  PhoneIncoming,
  PhoneMissed,
  PhoneOutgoing
} from '@lucide/vue'
import type { CallRecord } from '../api/types'
import type { LineTagLine } from '../utils/lineIdentity'
import { formatRelativeDate } from '../utils/format'
import BaseAvatar from './BaseAvatar.vue'
import LineTag from './LineTag.vue'

const props = withDefaults(
  defineProps<{
    call: CallRecord
    name: string
    avatar?: string
    line: LineTagLine
    lineFallback?: string
    selected?: boolean
    hasRecording?: boolean
  }>(),
  {
    avatar: '',
    lineFallback: '',
    selected: false,
    hasRecording: false
  }
)

const emit = defineEmits<{
  select: [call: CallRecord]
}>()
const { t } = useI18n()

const directionIcon = computed(() => {
  if (props.call.missed) return PhoneMissed
  return props.call.direction === 'incoming' ? PhoneIncoming : PhoneOutgoing
})

const directionLabel = computed(() => {
  if (props.call.missed) return t('dashboard.missedCall')
  return props.call.direction === 'incoming'
    ? t('dashboard.incoming')
    : t('dashboard.outgoing')
})
</script>

<template>
  <button
    class="list-item call-list-item"
    :class="{ 'is-selected': selected, 'is-missed': call.missed }"
    type="button"
    :aria-label="t('calls.viewDetails', { name })"
    @click="emit('select', props.call)"
  >
    <span class="call-list-item__avatar">
      <BaseAvatar :name="name" :src="avatar" />
      <span class="call-direction-icon">
        <component :is="directionIcon" :size="12" />
      </span>
    </span>
    <span class="list-item__content">
      <span class="list-item__title">
        <strong>{{ name }}</strong>
        <time>{{ formatRelativeDate(call.started_at) }}</time>
      </span>
      <span class="call-list-item__meta">
        <LineTag :line="line" :fallback="lineFallback" />
        <small>{{ directionLabel }} · {{ call.remote_number }}</small>
        <span
          v-if="hasRecording"
          class="call-list-item__recording"
          role="img"
          :aria-label="t('calls.hasRecording')"
          :title="t('calls.hasRecording')"
        >
          <CassetteTape :size="15" aria-hidden="true" />
        </span>
      </span>
    </span>
  </button>
</template>

<style scoped>
.call-list-item {
  min-height: 76px;
}

.call-list-item__avatar {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
}

.call-list-item__avatar .call-direction-icon {
  position: absolute;
  right: -4px;
  bottom: -4px;
  width: 21px;
  height: 21px;
  flex: 0 0 21px;
  color: var(--blue);
  background: var(--blue-soft);
  border: 2px solid var(--surface);
  box-shadow: 0 1px 3px rgb(16 24 40 / 14%);
}

.call-list-item__meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.call-list-item__meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-list-item__recording {
  display: inline-grid;
  width: 22px;
  height: 22px;
  flex: 0 0 22px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}
</style>
