<script setup lang="ts">
import ApplicationUpdateBar from './components/ApplicationUpdateBar.vue'
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
  <ApplicationUpdateBar />
  <ConfirmationDialog />
  <FeedbackHost />
</template>
