<script setup lang="ts">
import { computed } from 'vue'
import PageContentFrame from '../PageContentFrame.vue'
import SettingsSkeleton from './SettingsSkeleton.vue'
import type { SettingsSkeletonShape } from './settingsSkeleton'

const props = defineProps<{
  loadingTitle: string
  loadingShape: SettingsSkeletonShape
}>()

const flush = computed(
  () => props.loadingShape === 'master-detail' || props.loadingShape === 'workbench'
)
const frameMode = computed(() =>
  props.loadingShape === 'diagnostics' ? 'fluid' : 'reading'
)
</script>

<template>
  <div class="settings-async-boundary">
    <Suspense>
      <slot />
      <template #fallback>
        <div
          class="settings-async-boundary__fallback"
          :class="{ 'settings-async-boundary__fallback--flush': flush }"
        >
          <SettingsSkeleton
            v-if="flush"
            :label="loadingTitle"
            :shape="loadingShape"
          />
          <PageContentFrame v-else :mode="frameMode">
            <SettingsSkeleton :label="loadingTitle" :shape="loadingShape" />
          </PageContentFrame>
        </div>
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

.settings-async-boundary__fallback {
  width: 100%;
  min-width: 0;
  min-height: 0;
  flex: 1;
  padding: 26px;
  overflow-y: auto;
}

.settings-async-boundary__fallback--flush {
  display: flex;
  padding: 0;
  overflow: hidden;
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
