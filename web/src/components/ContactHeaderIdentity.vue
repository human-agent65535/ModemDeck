<script setup lang="ts">
import { computed } from 'vue'
import type { CommunicationChannel } from '../utils/communicationAvatar'
import type { LineTagLine } from '../utils/lineIdentity'
import CommunicationAvatar from './CommunicationAvatar.vue'
import LineTag from './LineTag.vue'

const props = withDefaults(
  defineProps<{
    channel?: CommunicationChannel
    name: string
    number: string
    avatar?: string
    line: LineTagLine
    lineFallback: string
  }>(),
  {
    channel: 'call',
    avatar: ''
  }
)

const showNumber = computed(
  () =>
    Boolean(props.number.trim()) &&
    props.number.trim().toLocaleLowerCase() !== props.name.trim().toLocaleLowerCase()
)
</script>

<template>
  <CommunicationAvatar
    :channel="channel"
    :name="name"
    :address="number"
    :src="avatar"
    size="small"
  />
  <div class="contact-header-identity">
    <h2>{{ name }}</h2>
    <div class="contact-header-identity__meta">
      <LineTag :line="line" :fallback="lineFallback" />
      <span v-if="showNumber">{{ number }}</span>
    </div>
  </div>
</template>

<style scoped>
.contact-header-identity {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}

.contact-header-identity h2 {
  overflow: hidden;
  font-size: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.contact-header-identity__meta {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.contact-header-identity__meta > span {
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 760px) {
  .contact-header-identity h2 {
    font-size: 16px;
  }
}
</style>
