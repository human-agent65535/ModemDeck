<script setup lang="ts">
withDefaults(
  defineProps<{
    label: string
    detailOpen?: boolean
  }>(),
  {
    detailOpen: false
  }
)
</script>

<template>
  <section
    class="device-workspace mobile-drilldown"
    :class="{ 'is-detail-open': detailOpen }"
    :aria-label="label"
  >
    <div class="device-workspace__selector mobile-drilldown__list">
      <slot name="selector" />
    </div>
    <div class="device-workspace__detail mobile-drilldown__detail">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.device-workspace {
  display: block;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  background: var(--surface);
  overscroll-behavior: contain;
}

.device-workspace__selector,
.device-workspace__detail {
  min-width: 0;
  min-height: 0;
}

.device-workspace__selector {
  border-bottom: 1px solid var(--border);
}

.device-workspace__detail {
  overflow: visible;
}

@media (max-width: 860px) {
  .device-workspace {
    display: grid;
    overflow: hidden;
    grid-template-rows: minmax(0, 1fr);
  }

  .device-workspace__selector,
  .device-workspace__detail {
    overflow-y: auto;
    overscroll-behavior: contain;
  }
}
</style>
