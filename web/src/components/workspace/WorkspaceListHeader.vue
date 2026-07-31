<script setup lang="ts">
defineProps<{
  title: string
  count?: number | string
  hideOnCompact?: boolean
}>()
</script>

<template>
  <header
    class="pane-header workspace-list-header"
    :class="{ 'workspace-list-header--compact-hidden': hideOnCompact }"
  >
    <div class="workspace-list-header__title">
      <h1>{{ title }}</h1>
      <span v-if="count !== undefined">{{ count }}</span>
    </div>
    <slot name="actions" />
  </header>
</template>

<style scoped>
.workspace-list-header {
  justify-content: flex-start;
  gap: var(--detail-action-gap);
}

.workspace-list-header__title {
  margin-right: auto;
}

@media (max-width: 860px) {
  .workspace-list-header.workspace-list-header--compact-hidden {
    display: none;
  }
}
</style>
