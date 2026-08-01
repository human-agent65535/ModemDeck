<script setup lang="ts">
import StatePanel from '../StatePanel.vue'
import SettingsSkeleton from './SettingsSkeleton.vue'
import type { SettingsSkeletonShape } from './settingsSkeleton'

withDefaults(
  defineProps<{
    loading?: boolean
    error?: boolean
    forbidden?: boolean
    loadingTitle: string
    loadingShape: SettingsSkeletonShape
    errorTitle?: string
    forbiddenTitle?: string
    detail?: string
    retryable?: boolean
  }>(),
  {
    loading: false,
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
  <SettingsSkeleton
    v-if="loading"
    :label="loadingTitle"
    :shape="loadingShape"
  />
  <StatePanel
    v-else-if="forbidden"
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
</template>
