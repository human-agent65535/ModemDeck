<script setup lang="ts">
withDefaults(
  defineProps<{
    filterColumns?: 2 | 3 | 4
    hasLineFilter?: boolean
  }>(),
  {
    filterColumns: 4,
    hasLineFilter: false
  }
)
</script>

<template>
  <div
    class="pane-search communication-list-toolbar"
    :class="[
      `communication-list-toolbar--filters-${filterColumns}`,
      { 'communication-list-toolbar--has-line-filter': hasLineFilter }
    ]"
  >
    <div class="pane-search-row communication-list-toolbar__primary">
      <slot name="primary" />
    </div>
    <div
      v-if="$slots.filters"
      class="communication-list-toolbar__filters"
    >
      <slot name="filters" />
    </div>
  </div>
</template>

<style scoped>
.communication-list-toolbar__filters {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.communication-list-toolbar__filters :deep(.segmented-control) {
  min-width: 0;
  flex: 1;
}

.communication-list-toolbar--has-line-filter
  .communication-list-toolbar__primary
  :deep(.search-field) {
  padding-right: 50px;
}

.communication-list-toolbar__primary :deep(.line-selector.is-filter) {
  position: absolute;
  z-index: 4;
  top: 3px;
  right: 3px;
}

.communication-list-toolbar--filters-2 :deep(.segmented-control) {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.communication-list-toolbar--filters-3 :deep(.segmented-control) {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.communication-list-toolbar--filters-4 :deep(.segmented-control) {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

@media (max-width: 560px) {
  .communication-list-toolbar {
    gap: var(--space-2);
    padding-inline: var(--space-3);
  }

  .communication-list-toolbar__primary,
  .communication-list-toolbar__filters {
    gap: var(--space-2);
  }

  .communication-list-toolbar__filters :deep(button) {
    min-height: var(--touch-target);
  }

  .communication-list-toolbar__filters :deep(.segmented-control) {
    height: var(--touch-target);
  }
}
</style>
