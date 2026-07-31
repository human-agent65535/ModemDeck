<script setup lang="ts">
withDefaults(
  defineProps<{
    label: string
    mobileMode?: 'stack' | 'drilldown'
    detailOpen?: boolean
  }>(),
  {
    mobileMode: 'stack',
    detailOpen: false
  }
)
</script>

<template>
  <section
    class="settings-master-detail"
    :class="[
      `settings-master-detail--${mobileMode}`,
      { 'is-detail-open': detailOpen }
    ]"
    :aria-label="label"
  >
    <div class="settings-master-detail__sidebar">
      <slot name="sidebar" />
    </div>
    <div class="settings-master-detail__detail">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.settings-master-detail {
  --settings-master-sidebar: 240px;

  display: grid;
  min-width: 0;
  min-height: 520px;
  grid-template-columns: var(--settings-master-sidebar) minmax(0, 1fr);
  background: var(--surface);
  border-top: 1px solid var(--border);
}

.settings-master-detail__sidebar,
.settings-master-detail__detail {
  min-width: 0;
}

.settings-master-detail__sidebar {
  border-right: 1px solid var(--border);
}

.settings-master-detail__sidebar :deep(> *),
.settings-master-detail__detail :deep(> *) {
  min-height: 100%;
}

@media (max-width: 720px) {
  .settings-master-detail {
    min-height: 0;
    grid-template-columns: minmax(0, 1fr);
    border-top: 0;
  }

  .settings-master-detail__sidebar {
    border-right: 0;
  }

  .settings-master-detail--stack .settings-master-detail__sidebar {
    border-bottom: 1px solid var(--border);
  }

  .settings-master-detail--drilldown:not(.is-detail-open)
    .settings-master-detail__detail,
  .settings-master-detail--drilldown.is-detail-open
    .settings-master-detail__sidebar {
    display: none;
  }
}
</style>
