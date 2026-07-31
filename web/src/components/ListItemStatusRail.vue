<script setup lang="ts">
defineProps<{
  date?: string
  dateTime?: string
}>()
</script>

<template>
  <span
    class="list-item-status-rail"
    :class="{ 'has-date': date }"
  >
    <time v-if="date" :datetime="dateTime">{{ date }}</time>
    <span class="list-item-status-rail__icons">
      <slot />
      <slot name="favorite" />
    </span>
  </span>
</template>

<style scoped>
.list-item-status-rail {
  display: flex;
  min-width: 42px;
  align-self: stretch;
  flex: 0 0 auto;
  flex-direction: column;
  align-items: flex-end;
  justify-content: center;
  gap: 8px;
  padding: 1px 0;
}

.list-item-status-rail.has-date {
  justify-content: space-between;
}

.list-item-status-rail time {
  color: var(--faint);
  font-size: 11px;
  line-height: 1.25;
  white-space: nowrap;
}

.list-item-status-rail__icons {
  display: inline-flex;
  min-height: 18px;
  align-items: center;
  justify-content: flex-end;
  gap: 7px;
}
</style>
