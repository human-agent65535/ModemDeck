<script setup lang="ts">
import SkeletonBlock from './SkeletonBlock.vue'

withDefaults(
  defineProps<{
    label: string
    rows?: number
    variant?: 'list' | 'content'
  }>(),
  {
    rows: 6,
    variant: 'list'
  }
)
</script>

<template>
  <div
    class="list-skeleton"
    :class="`list-skeleton--${variant}`"
    role="status"
    aria-live="polite"
  >
    <span class="sr-only">{{ label }}</span>
    <div v-for="row in rows" :key="row" class="list-skeleton__row" aria-hidden="true">
      <SkeletonBlock class="list-skeleton__avatar" />
      <span class="list-skeleton__copy">
        <SkeletonBlock />
        <SkeletonBlock />
      </span>
    </div>
  </div>
</template>

<style scoped>
.list-skeleton {
  flex: 1;
  overflow: hidden;
}

.list-skeleton__row {
  display: grid;
  min-height: 66px;
  align-items: center;
  gap: 11px;
  padding: 10px 14px;
  grid-template-columns: 40px minmax(0, 1fr);
  border-bottom: 1px solid var(--border);
}

.list-skeleton__avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
}

.list-skeleton--content {
  width: 100%;
  max-width: 860px;
  margin-inline: auto;
  padding-block: 10px;
}

.list-skeleton--content .list-skeleton__row {
  min-height: 76px;
  padding-inline: 16px;
  grid-template-columns: 44px minmax(0, 1fr);
}

.list-skeleton--content .list-skeleton__avatar {
  width: 44px;
  height: 44px;
  border-radius: var(--radius-surface);
}

.list-skeleton--content .list-skeleton__copy > span:first-child {
  width: min(42%, 220px);
  height: 12px;
}

.list-skeleton--content .list-skeleton__copy > span:last-child {
  width: min(76%, 460px);
}

.list-skeleton--content
  .list-skeleton__row:nth-child(even)
  .list-skeleton__copy
  > span:first-child {
  width: min(34%, 180px);
}

.list-skeleton--content
  .list-skeleton__row:nth-child(even)
  .list-skeleton__copy
  > span:last-child {
  width: min(62%, 380px);
}

.list-skeleton__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 8px;
}

.list-skeleton__copy > span {
  width: min(62%, 190px);
  height: 11px;
  border-radius: 6px;
}

.list-skeleton__copy > span:last-child {
  width: min(84%, 250px);
  height: 9px;
}

</style>
