<script setup lang="ts">
import { AlertCircle, Inbox, LoaderCircle, ShieldAlert } from '@lucide/vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

withDefaults(
  defineProps<{
    state: 'loading' | 'error' | 'forbidden' | 'empty'
    title: string
    detail?: string
    retryable?: boolean
  }>(),
  {
    detail: '',
    retryable: false
  }
)

const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div class="state-panel" :role="state === 'error' || state === 'forbidden' ? 'alert' : 'status'">
    <span v-if="state === 'loading'" class="state-panel__loading-mark" aria-hidden="true">
      <LoaderCircle class="spin" :size="24" />
    </span>
    <AlertCircle v-else-if="state === 'error'" :size="26" aria-hidden="true" />
    <ShieldAlert v-else-if="state === 'forbidden'" :size="26" aria-hidden="true" />
    <Inbox v-else :size="26" aria-hidden="true" />
    <strong>{{ title }}</strong>
    <p v-if="detail">{{ detail }}</p>
    <button v-if="state === 'error' && retryable" class="text-button" type="button" @click="emit('retry')">
      {{ t('common.retry') }}
    </button>
  </div>
</template>
