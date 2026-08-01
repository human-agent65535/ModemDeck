<script setup lang="ts">
import StatePanel from '../StatePanel.vue'
import LoadingSkeletonBoundary from '../skeletons/LoadingSkeletonBoundary.vue'
import SettingsSkeleton from './SettingsSkeleton.vue'
import type { SettingsSkeletonShape } from './settingsSkeleton'

const props = withDefaults(
  defineProps<{
    loading?: boolean
    error?: boolean
    forbidden?: boolean
    loadingTitle: string
    loadingShape: SettingsSkeletonShape
    hasSelection?: boolean
    errorTitle?: string
    forbiddenTitle?: string
    detail?: string
    retryable?: boolean
  }>(),
  {
    loading: false,
    hasSelection: false,
    error: false,
    forbidden: false,
    errorTitle: '',
    forbiddenTitle: '',
    detail: '',
    retryable: false
  }
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <LoadingSkeletonBoundary :loading="props.loading">
    <template #skeleton>
      <SettingsSkeleton
        :label="loadingTitle"
        :shape="loadingShape"
        :has-selection="hasSelection"
      />
    </template>
    <StatePanel
      v-if="forbidden"
      state="forbidden"
      :title="forbiddenTitle || errorTitle || loadingTitle"
      :detail="detail"
    />
    <StatePanel
      v-else-if="error"
      state="error"
      :title="errorTitle || loadingTitle"
      :detail="detail"
      :retryable="retryable"
      @retry="emit('retry')"
    />
    <slot v-else />
  </LoadingSkeletonBoundary>
</template>
