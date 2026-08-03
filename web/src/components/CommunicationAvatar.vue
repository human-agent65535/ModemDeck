<script setup lang="ts">
import { computed } from 'vue'
import { AudioLines, MessageSquareText, Phone } from '@lucide/vue'
import type { CommunicationChannel } from '../utils/communicationAvatar'
import {
  communicationAvatarFallback,
  communicationAvatarPaletteKey
} from '../utils/communicationAvatar'
import BaseAvatar from './BaseAvatar.vue'

const props = withDefaults(
  defineProps<{
    channel: CommunicationChannel
    name: string
    address: string
    src?: string
    size?: 'small' | 'medium' | 'large'
    contactBound?: boolean
    muted?: boolean
  }>(),
  {
    src: '',
    size: 'medium',
    contactBound: false,
    muted: false
  }
)

const identity = computed(() => ({
  channel: props.channel,
  name: props.name,
  address: props.address,
  contactBound: props.contactBound
}))
const fallback = computed(() => communicationAvatarFallback(identity.value))
const paletteKey = computed(() =>
  communicationAvatarPaletteKey(identity.value, fallback.value)
)
const channelIcon = computed(() => {
  if (props.channel === 'message') return MessageSquareText
  if (props.channel === 'recording') return AudioLines
  return Phone
})
const badgeIconSize = computed(() => {
  if (props.size === 'large') return 14
  if (props.size === 'small') return 10
  return 12
})
</script>

<template>
  <span
    class="communication-avatar"
    :class="[
      `communication-avatar--${size}`,
      `communication-avatar--${channel}`,
      { 'is-muted': muted }
    ]"
  >
    <BaseAvatar
      :name="name || address"
      :src="src"
      :size="size"
      :fallback="fallback"
      :palette-key="paletteKey"
    />
    <span
      v-if="contactBound"
      class="communication-avatar__badge"
      aria-hidden="true"
    >
      <component :is="channelIcon" :size="badgeIconSize" :stroke-width="1.9" />
    </span>
  </span>
</template>

<style scoped>
.communication-avatar {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
}

.communication-avatar__badge {
  position: absolute;
  z-index: 1;
  right: -4px;
  bottom: -4px;
  display: inline-grid;
  width: 21px;
  height: 21px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border: 2px solid var(--surface);
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 14%);
}

.communication-avatar--call .communication-avatar__badge {
  color: var(--blue);
  background: var(--blue-soft);
}

.communication-avatar--small .communication-avatar__badge {
  right: -3px;
  bottom: -3px;
  width: 18px;
  height: 18px;
}

.communication-avatar--large .communication-avatar__badge {
  width: 24px;
  height: 24px;
}

.communication-avatar.is-muted .communication-avatar__badge,
.communication-avatar.is-muted :deep(.avatar) {
  color: var(--muted);
  background: var(--surface-hover);
}
</style>
