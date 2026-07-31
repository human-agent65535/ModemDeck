<script setup lang="ts">
withDefaults(
  defineProps<{
    label: string
    sidebarTitle?: string
    sidebarDescription?: string
    detailOpen?: boolean
    detailKey?: string | number | null
  }>(),
  {
    sidebarTitle: '',
    sidebarDescription: '',
    detailOpen: false,
    detailKey: null
  }
)
</script>

<template>
  <section
    class="settings-master-detail mobile-drilldown"
    :class="{ 'is-detail-open': detailOpen }"
    :aria-label="label"
  >
    <aside
      class="settings-master-detail__sidebar mobile-drilldown__list"
      :aria-label="sidebarTitle || label"
    >
      <header class="settings-master-detail__sidebar-header">
        <span class="settings-master-detail__sidebar-copy">
          <strong>{{ sidebarTitle || label }}</strong>
          <small v-if="sidebarDescription">{{ sidebarDescription }}</small>
        </span>
        <slot name="sidebar-action" />
      </header>
      <div
        v-if="$slots['sidebar-toolbar']"
        class="settings-master-detail__sidebar-toolbar"
      >
        <slot name="sidebar-toolbar" />
      </div>
      <div class="settings-master-detail__list">
        <slot name="sidebar" />
      </div>
    </aside>
    <div class="settings-master-detail__detail mobile-drilldown__detail">
      <div
        :key="detailKey == null ? 'settings-detail' : String(detailKey)"
        class="settings-master-detail__detail-content"
      >
        <slot />
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings-master-detail {
  --settings-master-sidebar: clamp(240px, 25%, 280px);

  display: grid;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  grid-template-columns: var(--settings-master-sidebar) minmax(0, 1fr);
  background: var(--surface);
  border-top: 1px solid var(--border);
  container-type: inline-size;
}

.settings-master-detail__sidebar,
.settings-master-detail__detail {
  min-width: 0;
}

.settings-master-detail__sidebar {
  display: flex;
  min-height: 0;
  overflow: hidden;
  flex-direction: column;
  background: var(--surface-subtle);
  border-right: 1px solid var(--border);
}

.settings-master-detail__sidebar-header {
  display: flex;
  min-height: 64px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 12px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.settings-master-detail__sidebar-copy {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 2px;
}

.settings-master-detail__sidebar-copy strong {
  overflow: hidden;
  color: var(--text);
  font-size: 14px;
  font-weight: 750;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings-master-detail__sidebar-copy small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  font-weight: 550;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings-master-detail__sidebar-toolbar {
  flex: 0 0 auto;
}

.settings-master-detail__list {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  background: var(--surface-subtle);
}

.settings-master-detail__list :deep(.settings-resource-row) {
  width: 100%;
  min-height: 64px;
  text-align: left;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}

.settings-master-detail__list :deep(.settings-resource-row:hover) {
  background: var(--surface-hover);
}

.settings-master-detail__list :deep(.settings-resource-row.is-selected) {
  background: var(--surface-selected);
  box-shadow: inset 3px 0 0 var(--accent);
}

.settings-master-detail__detail {
  display: flex;
  min-height: 0;
  overflow: hidden;
  background: var(--surface);
}

.settings-master-detail__detail-content {
  min-width: 0;
  min-height: 0;
  flex: 1;
  padding-inline: 24px;
  overflow-y: auto;
  overscroll-behavior: contain;
}

@media (min-width: 861px) {
  .settings-master-detail__detail-content {
    animation: settings-master-detail-content-in var(--motion-base)
      var(--ease-standard) both;
  }
}

@keyframes settings-master-detail-content-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
}

@media (max-width: 860px) {
  .settings-master-detail {
    min-height: 0;
    grid-template-columns: minmax(0, 1fr);
    border-top: 0;
  }

  .settings-master-detail__sidebar {
    border-right: 0;
  }

  .settings-master-detail__detail-content {
    padding: 12px 16px 0;
  }
}
</style>
