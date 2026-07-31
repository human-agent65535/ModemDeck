<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string
    count?: number | string
    compactMode?: 'default' | 'hidden' | 'floating-action'
  }>(),
  {
    compactMode: 'default'
  }
)
</script>

<template>
  <header
    class="pane-header workspace-list-header"
    :class="`workspace-list-header--compact-${compactMode}`"
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

  .workspace-list-header.workspace-list-header--compact-floating-action {
    min-height: 0;
    flex-basis: 0;
    padding: 0;
    border: 0;
  }

  .workspace-list-header--compact-floating-action .workspace-list-header__title {
    display: none;
  }
}
</style>
