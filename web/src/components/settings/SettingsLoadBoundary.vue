<script setup lang="ts">
import StatePanel from '../StatePanel.vue'

withDefaults(
  defineProps<{
    loading?: boolean
    error?: boolean
    forbidden?: boolean
    loadingTitle: string
    errorTitle?: string
    forbiddenTitle?: string
    detail?: string
    retryable?: boolean
    loadingRows?: number
  }>(),
  {
    loading: false,
    error: false,
    forbidden: false,
    errorTitle: '',
    forbiddenTitle: '',
    detail: '',
    retryable: false,
    loadingRows: 5
  }
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <StatePanel
    v-if="loading"
    state="loading"
    :title="loadingTitle"
    :loading-rows="loadingRows"
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
