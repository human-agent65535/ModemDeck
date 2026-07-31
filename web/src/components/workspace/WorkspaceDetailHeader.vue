<script setup lang="ts">
withDefaults(
  defineProps<{
    density?: 'default' | 'compact'
  }>(),
  {
    density: 'default'
  }
)
</script>

<template>
  <header
    class="workspace-detail-header"
    :class="`workspace-detail-header--${density}`"
  >
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
  --detail-action-size: 38px;

  display: flex;
  min-width: 0;
  min-height: 82px;
  flex: 0 0 auto;
  align-items: center;
  gap: 14px;
  padding: 14px 24px;
  border-bottom: 1px solid var(--border);
  container-type: inline-size;
}

.workspace-detail-header--compact {
  min-height: 68px;
  padding-top: 10px;
  padding-bottom: 10px;
}

.workspace-detail-header__actions {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--detail-action-gap);
  margin-left: auto;
}

.workspace-detail-header :deep(.workspace-detail-identity) {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 4px;
}

.workspace-detail-header :deep(.workspace-detail-identity h2) {
  overflow: hidden;
  font-size: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workspace-detail-header :deep(.workspace-detail-identity span) {
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 560px) {
  .workspace-detail-header {
    --detail-action-size: 36px;

    min-height: 62px;
    padding: 9px 12px;
  }

  .workspace-detail-header :deep(.avatar--large) {
    width: 44px;
    height: 44px;
    font-size: 13px;
  }

  .workspace-detail-header :deep(.workspace-detail-identity h2) {
    font-size: 16px;
  }
}
</style>
