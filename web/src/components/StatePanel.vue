<script setup lang="ts">
import { AlertCircle, Inbox, ShieldAlert } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import ListSkeleton from './ListSkeleton.vue'

const { t } = useI18n()

withDefaults(
  defineProps<{
    state: 'loading' | 'error' | 'forbidden' | 'empty'
    title: string
    detail?: string
    retryable?: boolean
    loadingRows?: number
  }>(),
  {
    detail: '',
    retryable: false,
    loadingRows: 5
  }
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <ListSkeleton
    v-if="state === 'loading'"
    :label="title"
    :rows="loadingRows"
    variant="content"
  />
  <div v-else class="state-panel" :role="state === 'error' || state === 'forbidden' ? 'alert' : 'status'">
    <AlertCircle v-if="state === 'error'" :size="26" aria-hidden="true" />
    <ShieldAlert v-else-if="state === 'forbidden'" :size="26" aria-hidden="true" />
    <Inbox v-else :size="26" aria-hidden="true" />
    <strong>{{ title }}</strong>
    <p v-if="detail">{{ detail }}</p>
    <button v-if="state === 'error' && retryable" class="text-button" type="button" @click="emit('retry')">
      {{ t('common.retry') }}
    </button>
  </div>
</template>
