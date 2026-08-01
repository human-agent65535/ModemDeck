<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  contentKey?: string | number | null
}>()

const hasContent = computed(
  () =>
    props.contentKey !== undefined &&
    props.contentKey !== null &&
    String(props.contentKey).length > 0
)
</script>

<template>
  <article class="detail-pane workspace-detail-pane">
    <div
      v-if="hasContent"
      :key="`detail:${String(contentKey)}`"
      class="workspace-detail-pane__content"
    >
      <slot />
    </div>
    <div
      v-else
      key="empty"
      class="workspace-detail-pane__empty"
    >
      <slot name="empty" />
    </div>
  </article>
</template>

<style scoped>
.workspace-detail-pane__content,
.workspace-detail-pane__empty {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
}
</style>
