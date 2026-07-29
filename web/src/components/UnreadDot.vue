<script setup lang="ts">
defineProps<{
  label: string
}>()
</script>

<template>
  <span
    class="unread-dot"
    role="img"
    :aria-label="label"
    :title="label"
  />
</template>

<style scoped>
.unread-dot {
  display: inline-block;
  width: 9px;
  height: 9px;
  flex: 0 0 9px;
  background: var(--accent);
  border-radius: 50%;
  box-shadow: 0 0 0 3px var(--accent-soft);
}
</style>
