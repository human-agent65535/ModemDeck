<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  hasMore: boolean
  loading: boolean
  error?: string
  loadingLabel: string
  retryLabel: string
}>()

const emit = defineEmits<{
  load: []
}>()

const target = ref<HTMLElement | null>(null)
const active = computed(
  () => props.hasMore && !props.loading && !props.error
)
let observer: IntersectionObserver | undefined

function reconnect(): void {
  observer?.disconnect()
  observer = undefined
  if (!active.value || !target.value) return
  observer = new IntersectionObserver(
    entries => {
      if (entries.some(entry => entry.isIntersecting) && active.value) {
        emit('load')
      }
    },
    { rootMargin: '240px 0px' }
  )
  observer.observe(target.value)
}

watch([target, active], reconnect, { flush: 'post' })
onMounted(reconnect)
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="target" class="infinite-scroll-trigger" role="status">
    <template v-if="loading">
      <LoaderCircle class="spin" :size="16" />
      <span>{{ loadingLabel }}</span>
    </template>
    <template v-else-if="error">
      <span>{{ error }}</span>
      <button type="button" @click="emit('load')">
        {{ retryLabel }}
      </button>
    </template>
  </div>
</template>
