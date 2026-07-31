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
  display: grid;
  min-width: 0;
  min-height: 520px;
  grid-template-rows: auto minmax(0, 1fr);
  background: var(--surface);
  border-top: 1px solid var(--border);
}

.device-workspace__selector,
.device-workspace__detail {
  min-width: 0;
}

.device-workspace__selector {
  border-bottom: 1px solid var(--border);
}

@media (max-width: 860px) {
  .device-workspace {
    min-height: 0;
    grid-template-rows: minmax(0, 1fr);
    border-top: 0;
  }

}
</style>
