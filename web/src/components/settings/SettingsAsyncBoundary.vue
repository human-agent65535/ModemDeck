<script setup lang="ts">
import StatePanel from '../StatePanel.vue'

withDefaults(
  defineProps<{
    loadingTitle: string
    loadingRows?: number
  }>(),
  {
    loadingRows: 6
  }
)
</script>

<template>
  <div class="settings-async-boundary">
    <Suspense>
      <slot />
      <template #fallback>
        <StatePanel
          state="loading"
          :title="loadingTitle"
          :loading-rows="loadingRows"
        />
      </template>
    </Suspense>
  </div>
</template>

<style scoped>
.settings-async-boundary {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
}
</style>
