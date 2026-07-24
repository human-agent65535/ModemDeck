<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import ConfirmationDialog from './components/ConfirmationDialog.vue'
import {
  initializeBrowserAudio,
  shutdownAudioDevices
} from './state/audio'

onMounted(() => {
  void initializeBrowserAudio()
})

onBeforeUnmount(() => {
  shutdownAudioDevices()
})
</script>

<template>
  <RouterView />
  <ConfirmationDialog />
</template>
