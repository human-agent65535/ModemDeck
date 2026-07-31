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
    <Transition name="workspace-detail-content" mode="out-in">
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
    </Transition>
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

.workspace-detail-content-enter-active,
.workspace-detail-content-leave-active {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-base) var(--ease-standard);
}

.workspace-detail-content-enter-from {
  opacity: 0;
  transform: translateX(8px);
}

.workspace-detail-content-leave-to {
  opacity: 0;
  transform: translateX(-8px);
}

@media (prefers-reduced-motion: reduce) {
  .workspace-detail-content-enter-active,
  .workspace-detail-content-leave-active {
    transition: none;
  }

  .workspace-detail-content-enter-from,
  .workspace-detail-content-leave-to {
    transform: none;
  }
}
</style>
