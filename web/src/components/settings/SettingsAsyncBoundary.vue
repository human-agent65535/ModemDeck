<script setup lang="ts">
import { computed, ref } from 'vue'
import PageContentFrame from '../PageContentFrame.vue'
import { useLoadingVisibility } from '../../composables/useLoadingVisibility'
import { skeletonPreviewEnabled } from '../../composables/useSkeletonPreview'
import SettingsSkeleton from './SettingsSkeleton.vue'
import type { SettingsSkeletonShape } from './settingsSkeleton'

const props = defineProps<{
  loadingTitle: string
  loadingShape: SettingsSkeletonShape
  hasSelection?: boolean
}>()

const flush = computed(
  () => props.loadingShape === 'master-detail' || props.loadingShape === 'workbench'
)
const frameMode = computed(() =>
  props.loadingShape === 'diagnostics' ? 'fluid' : 'reading'
)
const suspensePending = ref(true)
const sourceLoading = computed(
  () => suspensePending.value || skeletonPreviewEnabled
)
const { active, visible } = useLoadingVisibility(sourceLoading, {
  revealDelay: skeletonPreviewEnabled ? 0 : undefined
})

function markPending(): void {
  suspensePending.value = true
}

function markResolved(): void {
  suspensePending.value = false
}
</script>

<template>
  <div class="settings-async-boundary">
    <Suspense @pending="markPending" @resolve="markResolved">
      <slot />
      <template #fallback>
        <div class="settings-async-boundary__placeholder" />
      </template>
    </Suspense>
    <div
      v-if="active"
      class="settings-async-boundary__fallback"
      :class="{
        'settings-async-boundary__fallback--flush': flush,
        'settings-async-boundary__fallback--pending': !visible
      }"
      :aria-hidden="visible ? undefined : 'true'"
    >
      <SettingsSkeleton
        v-if="flush"
        :label="loadingTitle"
        :shape="loadingShape"
        :has-selection="hasSelection"
      />
      <PageContentFrame v-else :mode="frameMode">
        <SettingsSkeleton
          :label="loadingTitle"
          :shape="loadingShape"
          :has-selection="hasSelection"
        />
      </PageContentFrame>
    </div>
  </div>
</template>

<style scoped>
.settings-async-boundary {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1;
  flex-direction: column;
}

.settings-async-boundary__placeholder {
  min-height: 0;
  flex: 1;
}

.settings-async-boundary__fallback {
  position: absolute;
  inset: 0;
  display: block;
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1;
  padding: 26px;
  overflow-y: auto;
  background: var(--surface);
}

.settings-async-boundary__fallback--flush {
  display: flex;
  padding: 0;
  overflow: hidden;
}

.settings-async-boundary__fallback--pending {
  visibility: hidden;
}

@media (max-width: 860px) {
  .settings-async-boundary__fallback {
    padding: 18px 16px;
  }

  .settings-async-boundary__fallback--flush {
    padding: 0;
  }
}
</style>
