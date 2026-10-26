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
      {
        'communication-list-toolbar--has-line-filter': hasLineFilter,
        'communication-list-toolbar--has-trailing-action': $slots.primaryTrailing
      }
    ]"
  >
    <div class="pane-search-row communication-list-toolbar__primary">
      <slot name="primary" />
      <slot name="primaryTrailing" />
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
.communication-list-toolbar {
  --communication-toolbar-primary-height: 36px;
  --communication-toolbar-filter-height: 32px;

  gap: 7px;
  padding: 6px var(--space-3) 9px;
}

.communication-list-toolbar__primary :deep(.search-field) {
  height: var(--communication-toolbar-primary-height);
}

.communication-list-toolbar__primary :deep(.pane-selection-toggle),
.communication-list-toolbar__primary :deep(.favorite-filter-button) {
  width: var(--communication-toolbar-primary-height);
  height: var(--communication-toolbar-primary-height);
  min-height: var(--communication-toolbar-primary-height);
  flex-basis: var(--communication-toolbar-primary-height);
}

.communication-list-toolbar__filters {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.communication-list-toolbar__filters :deep(.segmented-control) {
  height: var(--communication-toolbar-filter-height);
  min-width: 0;
  flex: 1;
}

.communication-list-toolbar__filters :deep(.favorite-filter-button) {
  width: var(--communication-toolbar-filter-height);
  height: var(--communication-toolbar-filter-height);
  flex-basis: var(--communication-toolbar-filter-height);
}

.communication-list-toolbar--has-line-filter
  .communication-list-toolbar__primary
  :deep(.search-field) {
  padding-right: 44px;
}

.communication-list-toolbar__primary :deep(.line-selector.is-filter) {
  position: absolute;
  z-index: 4;
  top: 50%;
  right: 0;
  transform: translateY(-50%);
}

.communication-list-toolbar--has-trailing-action
  .communication-list-toolbar__primary
  :deep(.line-selector.is-filter) {
  right: calc(var(--communication-toolbar-primary-height) + var(--space-2));
}

/* Segments size to their labels, so a short "All" leaves room for "Incoming" in narrow lists. */
.communication-list-toolbar--filters-2 :deep(.segmented-control) {
  grid-template-columns: repeat(2, auto);
}

.communication-list-toolbar--filters-3 :deep(.segmented-control) {
  grid-template-columns: repeat(3, auto);
}

.communication-list-toolbar--filters-4 :deep(.segmented-control) {
  grid-template-columns: repeat(4, auto);
}

/* Phones: 44px touch targets (HIG) and the shell's 16px side gutter. */
@media (max-width: 640px) {
  .communication-list-toolbar {
    --communication-toolbar-primary-height: var(--touch-target);
    --communication-toolbar-filter-height: var(--touch-target);

    gap: 6px;
    padding: 6px var(--space-4) 8px;
  }

  .communication-list-toolbar__primary,
  .communication-list-toolbar__filters {
    gap: 6px;
  }

  .communication-list-toolbar--has-trailing-action
    .communication-list-toolbar__primary
    :deep(.line-selector.is-filter) {
    right: calc(var(--communication-toolbar-primary-height) + 6px);
  }
}
</style>
