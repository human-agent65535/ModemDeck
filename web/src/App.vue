<script setup lang="ts">
import ConfirmationDialog from './components/ConfirmationDialog.vue'
import FeedbackHost from './components/FeedbackHost.vue'
</script>

<template>
  <RouterView />
  <ConfirmationDialog />
  <FeedbackHost />
</template>
