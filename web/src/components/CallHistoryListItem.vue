<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AudioLines,
  Star
} from '@lucide/vue'
import type { CallRecord } from '../api/types'
import type { LineTagLine } from '../utils/lineIdentity'
import { formatRelativeDate } from '../utils/format'
import CommunicationAvatar from './CommunicationAvatar.vue'
import LineTag from './LineTag.vue'
import ListItemAvatarStatus from './ListItemAvatarStatus.vue'
import ListItemStatusRail from './ListItemStatusRail.vue'

const props = withDefaults(
  defineProps<{
    call: CallRecord
    name: string
    number: string
    avatar?: string
    contactBound?: boolean
    line: LineTagLine
    lineFallback?: string
    selected?: boolean
    hasRecording?: boolean
  }>(),
  {
    avatar: '',
    contactBound: false,
    lineFallback: '',
    selected: false,
    hasRecording: false
  }
)

const emit = defineEmits<{
  select: [call: CallRecord]
}>()
const { t } = useI18n()

const directionLabel = computed(() => {
  if (props.call.missed) return t('dashboard.missedCall')
  return props.call.direction === 'incoming'
    ? t('dashboard.incoming')
    : t('dashboard.outgoing')
})
const showNumber = computed(
  () => props.number.trim().toLocaleLowerCase() !== props.name.trim().toLocaleLowerCase()
)
</script>

<template>
  <button
    class="list-item call-list-item"
    :class="{
      'is-selected': selected,
      'is-missed': call.missed,
      'is-unread': call.missed && !call.read
    }"
    type="button"
    :aria-label="
      call.missed && !call.read
        ? t('calls.viewUnreadDetails', { name })
        : t('calls.viewDetails', { name })
    "
    @click="emit('select', props.call)"
  >
    <ListItemAvatarStatus
      class="call-list-item__avatar"
      :unread-label="
        call.missed && !call.read
          ? t('calls.unreadMissed')
          : undefined
      "
    >
      <CommunicationAvatar
        channel="call"
        :name="name"
        :address="number"
        :src="avatar"
        :contact-bound="contactBound"
      />
    </ListItemAvatarStatus>
    <span class="list-item__content">
      <span class="call-list-item__identity">
        <strong>{{ name }}</strong>
      </span>
      <span class="call-list-item__meta">
        <LineTag :line="line" :fallback="lineFallback" />
        <small>
          {{ directionLabel }}<template v-if="showNumber"> · {{ number }}</template>
        </small>
      </span>
    </span>
    <ListItemStatusRail
      :date="formatRelativeDate(call.started_at)"
      :date-time="call.started_at"
    >
      <span
        v-if="hasRecording"
        class="call-list-item__recording"
        role="img"
        :aria-label="t('calls.hasRecording')"
        :title="t('calls.hasRecording')"
      >
        <AudioLines :size="15" aria-hidden="true" />
      </span>
      <template #favorite>
        <Star
          v-if="call.favorite"
          class="call-list-item__favorite"
          :size="15"
          fill="currentColor"
          :aria-label="t('common.favorite')"
        />
      </template>
    </ListItemStatusRail>
  </button>
</template>

<style scoped>
.call-list-item__meta {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 6px;
  overflow: hidden;
}

.call-list-item__meta small {
  min-width: 0;
  flex: 1 1 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.call-list-item__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.call-list-item__identity strong {
  min-width: 0;
}

.call-list-item__favorite {
  flex: 0 0 auto;
  color: var(--favorite);
}

.call-list-item__recording {
  display: inline-grid;
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  place-items: center;
  color: var(--accent-strong);
  background: transparent;
}
</style>
