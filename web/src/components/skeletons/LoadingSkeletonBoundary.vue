<script setup lang="ts">
import { computed } from 'vue'
import { useLoadingVisibility } from '../../composables/useLoadingVisibility'
import { skeletonPreviewEnabled } from '../../composables/useSkeletonPreview'

const props = defineProps<{
  loading: boolean
}>()

const sourceLoading = computed(() => props.loading || skeletonPreviewEnabled)
const { active, visible } = useLoadingVisibility(sourceLoading, {
  revealDelay: skeletonPreviewEnabled ? 0 : undefined
})
</script>

<template>
  <div
    v-if="active"
    class="loading-skeleton-boundary"
    :class="{ 'loading-skeleton-boundary--pending': !visible }"
    :aria-hidden="visible ? undefined : 'true'"
  >
    <slot name="skeleton" />
  </div>
  <slot v-else />
</template>

<style scoped>
.loading-skeleton-boundary {
  display: contents;
}

.loading-skeleton-boundary--pending {
  visibility: hidden;
}
</style>
