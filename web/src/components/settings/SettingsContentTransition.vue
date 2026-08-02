<script setup lang="ts">
defineProps<{
  contentKey: string | number
}>()
</script>

<template>
  <div :key="String(contentKey)" class="settings-content-transition">
    <slot />
  </div>
</template>

<style scoped>
.settings-content-transition {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
}

@media (min-width: 861px) {
  .settings-content-transition {
    animation: settings-content-in var(--motion-base) var(--ease-standard) both;
  }
}

@keyframes settings-content-in {
  from {
    opacity: 0;
    transform: translateY(var(--space-1));
  }
}
</style>
