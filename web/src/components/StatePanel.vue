<script setup lang="ts">
import { AlertCircle, Inbox, LoaderCircle, ShieldAlert } from '@lucide/vue'

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
    <LoaderCircle v-if="state === 'loading'" class="spin" :size="26" aria-hidden="true" />
    <AlertCircle v-else-if="state === 'error'" :size="26" aria-hidden="true" />
    <ShieldAlert v-else-if="state === 'forbidden'" :size="26" aria-hidden="true" />
    <Inbox v-else :size="26" aria-hidden="true" />
    <strong>{{ title }}</strong>
    <p v-if="detail">{{ detail }}</p>
    <button v-if="state === 'error' && retryable" class="text-button" type="button" @click="emit('retry')">
      重试
    </button>
  </div>
</template>
