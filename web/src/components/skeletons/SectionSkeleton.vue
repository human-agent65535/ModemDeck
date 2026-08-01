<script setup lang="ts">
import SkeletonBlock from '../SkeletonBlock.vue'

withDefaults(
  defineProps<{
    label: string
    variant?: 'facts' | 'form' | 'cards' | 'rows' | 'lines'
    rows?: number
  }>(),
  {
    variant: 'rows',
    rows: 4
  }
)
</script>

<template>
  <div
    class="section-skeleton"
    :class="`section-skeleton--${variant}`"
    role="status"
    aria-live="polite"
  >
    <span class="sr-only">{{ label }}</span>
    <template v-if="variant === 'lines'">
      <SkeletonBlock
        v-for="row in rows"
        :key="row"
        class="section-skeleton__line"
        aria-hidden="true"
      />
    </template>
    <template v-else-if="variant === 'cards'">
      <article v-for="row in rows" :key="row" aria-hidden="true">
        <SkeletonBlock class="section-skeleton__icon" />
        <span>
          <SkeletonBlock />
          <SkeletonBlock />
        </span>
        <SkeletonBlock class="section-skeleton__status" />
      </article>
    </template>
    <template v-else-if="variant === 'facts'">
      <span v-for="row in rows" :key="row" class="section-skeleton__fact" aria-hidden="true">
        <SkeletonBlock />
        <SkeletonBlock />
      </span>
    </template>
    <template v-else>
      <span v-for="row in rows" :key="row" class="section-skeleton__row" aria-hidden="true">
        <span>
          <SkeletonBlock />
          <SkeletonBlock />
        </span>
        <SkeletonBlock class="section-skeleton__control" />
      </span>
    </template>
  </div>
</template>

<style scoped>
.section-skeleton {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 0;
}

.section-skeleton--lines {
  gap: 9px;
}

.section-skeleton__line {
  width: 100%;
  height: 13px;
}

.section-skeleton__line:nth-child(even) {
  width: 82%;
}

.section-skeleton--cards {
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(280px, 420px));
}

.section-skeleton--cards article {
  display: flex;
  min-height: 102px;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
}

.section-skeleton--cards article > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 9px;
}

.section-skeleton--cards article > span:nth-child(2) > span:first-child {
  width: min(150px, 54%);
  height: 13px;
}

.section-skeleton--cards article > span:nth-child(2) > span:last-child {
  width: min(230px, 82%);
  height: 9px;
}

.section-skeleton__icon {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  border-radius: 9px;
}

.section-skeleton__status {
  width: 62px;
  height: 22px;
  border-radius: 999px;
}

.section-skeleton--facts {
  gap: 20px 28px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.section-skeleton__fact {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 8px;
}

.section-skeleton__fact > span:first-child {
  width: min(110px, 42%);
  height: 9px;
}

.section-skeleton__fact > span:last-child {
  width: min(230px, 78%);
  height: 13px;
}

.section-skeleton__row {
  display: flex;
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--border);
}

.section-skeleton__row > span:first-child {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 8px;
}

.section-skeleton__row > span:first-child > span:first-child {
  width: min(170px, 38%);
  height: 11px;
}

.section-skeleton__row > span:first-child > span:last-child {
  width: min(300px, 68%);
  height: 9px;
}

.section-skeleton__control {
  width: min(220px, 34%);
  height: 38px;
  border-radius: var(--radius-control);
}

@media (max-width: 560px) {
  .section-skeleton--facts {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .section-skeleton__row {
    align-items: flex-start;
    flex-direction: column;
    padding: 12px 0;
  }

  .section-skeleton__control {
    width: min(240px, 72%);
    align-self: flex-end;
  }
}
</style>
