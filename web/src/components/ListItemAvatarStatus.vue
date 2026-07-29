<script setup lang="ts">
import { useSlots } from 'vue'
import UnreadDot from './UnreadDot.vue'

defineProps<{
  unreadLabel?: string
}>()

const slots = useSlots()
</script>

<template>
  <span class="list-item-avatar-status">
    <slot />
    <span
      v-if="unreadLabel"
      class="list-item-avatar-status__unread"
    >
      <UnreadDot :label="unreadLabel" compact />
    </span>
    <span
      v-if="slots.badge"
      class="list-item-avatar-status__badge"
    >
      <slot name="badge" />
    </span>
  </span>
</template>

<style scoped>
.list-item-avatar-status {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
}

.list-item-avatar-status__unread {
  position: absolute;
  z-index: 2;
  top: -2px;
  left: -2px;
  display: inline-flex;
}

.list-item-avatar-status__badge {
  position: absolute;
  z-index: 1;
  right: -4px;
  bottom: -4px;
  display: inline-flex;
}
</style>
