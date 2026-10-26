<template>
  <header class="workspace-detail-header">
    <slot name="leading" />
    <slot name="identity" />
    <div
      v-if="$slots.actions"
      class="workspace-detail-header__actions"
    >
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.workspace-detail-header {
  display: flex;
  min-width: 0;
  min-height: 60px;
  flex: 0 0 auto;
  /* A narrow detail column (tablets) moves the actions under the name instead of
     squeezing the name to nothing or overlapping it. */
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  padding: 7px 20px;
  border-bottom: 1px solid var(--border);
  container-type: inline-size;
  /* Containment makes the header its own stacking context; keep it, and its More menu,
     above the scrolling body that follows it. */
  position: relative;
  z-index: 2;
}

.workspace-detail-header__actions {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--detail-action-gap);
  margin-left: auto;
}

.workspace-detail-header :deep(:is(.workspace-detail-identity, .contact-header-identity)) {
  min-width: min(160px, 100%);
}

.workspace-detail-header :deep(.workspace-detail-identity) {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 4px;
}

.workspace-detail-header :deep(.workspace-detail-identity h2) {
  overflow: hidden;
  font-size: var(--text-md);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-detail-header :deep(.workspace-detail-identity span) {
  overflow: hidden;
  color: var(--muted);
  font-size: var(--text-xs);
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 560px) {
  .workspace-detail-header {
    min-height: 62px;
    padding: 9px 12px;
  }

  .workspace-detail-header :deep(.avatar--large) {
    width: 44px;
    height: 44px;
    font-size: var(--text-sm);
  }

  .workspace-detail-header :deep(.workspace-detail-identity h2) {
    font-size: var(--text-md);
  }
}
</style>
