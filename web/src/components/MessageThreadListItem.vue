<script setup lang="ts">
import type { MessageThread } from '../api/types'
import type { LineTagLine } from '../utils/lineIdentity'
import { formatRelativeDate } from '../utils/format'
import CommunicationAvatar from './CommunicationAvatar.vue'
import LineTag from './LineTag.vue'
import ListItemAvatarStatus from './ListItemAvatarStatus.vue'
import ListItemStatusRail from './ListItemStatusRail.vue'
import { useI18n } from 'vue-i18n'
import { Star } from '@lucide/vue'

const props = withDefaults(
  defineProps<{
    thread: MessageThread
    name: string
    peer: string
    avatar?: string
    contactBound?: boolean
    line: LineTagLine
    lineFallback?: string
    selected?: boolean
    showFavorite?: boolean
    favoriteInteractive?: boolean
  }>(),
  {
    avatar: '',
    contactBound: false,
    lineFallback: '',
    selected: false,
    showFavorite: true,
    favoriteInteractive: false
  }
)

const emit = defineEmits<{
  select: [threadKey: string]
  favorite: [thread: MessageThread]
}>()
const { t } = useI18n()
</script>

<template>
  <div
    class="list-item list-item--thread"
    :class="{
      'is-selected': selected
    }"
    role="button"
    tabindex="0"
    @click="emit('select', props.thread.key)"
    @keydown.enter.prevent="emit('select', props.thread.key)"
    @keydown.space.prevent="emit('select', props.thread.key)"
  >
    <ListItemAvatarStatus
      :unread-label="
        thread.unread_count > 0
          ? t('messages.unreadCount', { count: thread.unread_count })
          : thread.marked_unread
            ? t('messages.unread')
            : undefined
      "
    >
      <CommunicationAvatar
        channel="message"
        :name="name"
        :address="peer"
        :src="avatar"
        :contact-bound="contactBound"
      />
    </ListItemAvatarStatus>
    <span class="list-item__content">
      <strong>{{ name }}</strong>
      <span class="message-thread-meta">
        <LineTag :line="line" :fallback="lineFallback" />
        <small>{{ thread.last_content || peer }}</small>
      </span>
    </span>
    <ListItemStatusRail
      :date="formatRelativeDate(thread.last_timestamp)"
      :date-time="thread.last_timestamp"
    >
      <template #favorite>
        <button
          v-if="showFavorite && favoriteInteractive"
          class="message-thread-favorite"
          type="button"
          :class="{ 'is-active': thread.favorite }"
          :title="
            thread.favorite
              ? t('messages.unfavorite')
              : t('messages.favorite')
          "
          :aria-label="
            thread.favorite
              ? t('messages.unfavorite')
              : t('messages.favorite')
          "
          :aria-pressed="thread.favorite"
          @click.stop="emit('favorite', thread)"
        >
          <Star
            :size="15"
            :fill="thread.favorite ? 'currentColor' : 'none'"
            aria-hidden="true"
          />
        </button>
        <Star
          v-else-if="showFavorite && thread.favorite"
          class="message-thread-favorite-mark"
          :size="15"
          fill="currentColor"
          :aria-label="t('messages.favorite')"
        />
      </template>
    </ListItemStatusRail>
  </div>
</template>

<style scoped>
.message-thread-meta {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 6px;
  overflow: hidden;
}

.message-thread-meta small {
  min-width: 0;
  flex: 1 1 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.message-thread-favorite {
  display: inline-grid;
  width: 24px;
  height: 24px;
  place-items: center;
  color: var(--faint);
  background: transparent;
}

.message-thread-favorite:hover,
.message-thread-favorite.is-active,
.message-thread-favorite-mark {
  color: var(--favorite);
  background: transparent;
}

.message-thread-favorite-mark {
  flex: 0 0 auto;
}
</style>
