<script setup lang="ts">
import SkeletonBlock from '../SkeletonBlock.vue'

defineProps<{ label: string }>()
</script>

<template>
  <div class="traffic-skeleton" role="status" aria-live="polite">
    <span class="sr-only">{{ label }}</span>
    <div class="traffic-skeleton__summary" aria-hidden="true">
      <SkeletonBlock v-for="card in 4" :key="card" />
    </div>
    <div class="traffic-skeleton__filters" aria-hidden="true">
      <SkeletonBlock v-for="filter in 3" :key="filter" />
    </div>
    <section v-for="section in 2" :key="section" class="traffic-skeleton__section" aria-hidden="true">
      <header>
        <SkeletonBlock />
        <SkeletonBlock />
      </header>
      <div class="traffic-skeleton__grid">
        <SkeletonBlock v-for="card in section === 1 ? 2 : 3" :key="card" />
      </div>
    </section>
  </div>
</template>

<style scoped>
.traffic-skeleton {
  display: grid;
  width: 100%;
  gap: 22px;
}

.traffic-skeleton__summary,
.traffic-skeleton__grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.traffic-skeleton__summary > span {
  height: 92px;
  border-radius: 10px;
}

.traffic-skeleton__filters {
  display: flex;
  gap: 8px;
}

.traffic-skeleton__filters > span {
  width: 104px;
  height: 36px;
  border-radius: 999px;
}

.traffic-skeleton__section {
  display: grid;
  gap: 14px;
  padding-top: 18px;
  border-top: 1px solid var(--border);
}

.traffic-skeleton__section header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.traffic-skeleton__section header > span:first-child {
  width: 130px;
  height: 14px;
}

.traffic-skeleton__section header > span:last-child {
  width: 28px;
  height: 18px;
  border-radius: 999px;
}

.traffic-skeleton__grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.traffic-skeleton__grid > span {
  height: 184px;
  border-radius: 10px;
}

@media (max-width: 860px) {
  .traffic-skeleton__summary,
  .traffic-skeleton__grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 560px) {
  .traffic-skeleton__summary,
  .traffic-skeleton__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
