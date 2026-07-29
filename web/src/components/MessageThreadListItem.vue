<script setup lang="ts">
import type { MessageThread } from '../api/types'
import type { LineTagLine } from '../utils/lineIdentity'
import { formatRelativeDate } from '../utils/format'
import BaseAvatar from './BaseAvatar.vue'
import LineTag from './LineTag.vue'
import UnreadDot from './UnreadDot.vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(
  defineProps<{
    thread: MessageThread
    name: string
    avatar?: string
    line: LineTagLine
    lineFallback?: string
    selected?: boolean
    arriving?: boolean
  }>(),
  {
    avatar: '',
    lineFallback: '',
    selected: false,
    arriving: false
  }
)

const emit = defineEmits<{
  select: [threadKey: string]
}>()
const { t } = useI18n()
</script>

<template>
  <button
    class="list-item list-item--thread"
    :class="{
      'is-selected': selected,
      'is-arriving': arriving
    }"
    type="button"
    @click="emit('select', props.thread.key)"
  >
    <BaseAvatar :name="name" :src="avatar" />
    <span class="list-item__content">
      <span class="list-item__title">
        <strong>{{ name }}</strong>
        <time>{{ formatRelativeDate(thread.last_timestamp) }}</time>
      </span>
      <span class="list-item__preview">
        <span class="message-thread-meta">
          <LineTag :line="line" :fallback="lineFallback" />
          <small>{{ thread.last_content || thread.peer }}</small>
        </span>
        <UnreadDot
          v-if="thread.unread_count"
          :label="t('messages.unreadCount', { count: thread.unread_count })"
        />
      </span>
    </span>
  </button>
</template>

<style scoped>
.list-item--thread.is-arriving {
  animation: incoming-thread 700ms ease-out;
}

@keyframes incoming-thread {
  from {
    background: var(--accent-soft);
    box-shadow: inset 3px 0 var(--accent);
  }
}

@media (prefers-reduced-motion: reduce) {
  .list-item--thread.is-arriving {
    animation: none;
  }
}

.message-thread-meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.message-thread-meta small {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
