<script setup lang="ts">
import ConfirmationDialog from './components/ConfirmationDialog.vue'
import FeedbackHost from './components/FeedbackHost.vue'
</script>

<template>
  <RouterView v-slot="{ Component, route }">
    <Transition name="application-surface">
      <component
        :is="Component"
        :key="route.name === 'login' ? 'login' : 'application'"
      />
    </Transition>
  </RouterView>
  <ConfirmationDialog />
  <FeedbackHost />
</template>
