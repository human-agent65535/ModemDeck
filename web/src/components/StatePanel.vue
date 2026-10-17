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
    <span class="state-panel__icon" :class="`state-panel__icon--${state}`" aria-hidden="true">
      <AlertCircle v-if="state === 'error'" :size="24" />
      <ShieldAlert v-else-if="state === 'forbidden'" :size="24" />
      <slot v-else name="icon"><Inbox :size="24" /></slot>
    </span>
    <strong>{{ title }}</strong>
    <p v-if="detail">{{ detail }}</p>
    <button v-if="state === 'error' && retryable" class="secondary-button" type="button" @click="emit('retry')">
      {{ t('common.retry') }}
    </button>
    <div v-if="$slots.actions" class="state-panel__actions"><slot name="actions" /></div>
  </div>
</template>
