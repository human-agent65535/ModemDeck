<script setup lang="ts">
import { computed } from 'vue'
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
  }>(),
  {
    src: '',
    size: 'medium'
  }
)

const identity = computed(() => ({
  channel: props.channel,
  name: props.name,
  address: props.address
}))
const fallback = computed(() => communicationAvatarFallback(identity.value))
const paletteKey = computed(() =>
  communicationAvatarPaletteKey(identity.value, fallback.value)
)
</script>

<template>
  <BaseAvatar
    :name="name || address"
    :src="src"
    :size="size"
    :fallback="fallback"
    :palette-key="paletteKey"
  />
</template>
