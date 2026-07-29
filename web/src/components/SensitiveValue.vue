<script setup lang="ts">
import { Eye, EyeOff } from '@lucide/vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { maskIdentifier } from '../utils/privacy'

const props = withDefaults(
  defineProps<{
    value?: string
    label: string
    remaskAfterMs?: number
  }>(),
  {
    value: '',
    remaskAfterMs: 30_000
  }
)

const { t } = useI18n()
const revealed = ref(false)
let remaskTimer = 0

const displayValue = computed(() => {
  const normalized = props.value.trim()
  if (!normalized) return '—'
  return revealed.value ? normalized : maskIdentifier(normalized)
})

function clearRemaskTimer(): void {
  if (remaskTimer) window.clearTimeout(remaskTimer)
  remaskTimer = 0
}

function toggleReveal(): void {
  revealed.value = !revealed.value
  clearRemaskTimer()
  if (revealed.value) {
    remaskTimer = window.setTimeout(() => {
      revealed.value = false
      remaskTimer = 0
    }, props.remaskAfterMs)
  }
}

watch(
  () => props.value,
  () => {
    revealed.value = false
    clearRemaskTimer()
  }
)

onBeforeUnmount(clearRemaskTimer)
</script>

<template>
  <span class="sensitive-value">
    <code>{{ displayValue }}</code>
    <button
      v-if="value.trim().length > 8"
      type="button"
      :title="
        t(
          revealed
            ? 'diagnostics.hideIdentifier'
            : 'diagnostics.showIdentifier',
          { label }
        )
      "
      :aria-label="
        t(
          revealed
            ? 'diagnostics.hideIdentifier'
            : 'diagnostics.showIdentifier',
          { label }
        )
      "
      :aria-pressed="revealed"
      @click="toggleReveal"
    >
      <EyeOff v-if="revealed" :size="14" aria-hidden="true" />
      <Eye v-else :size="14" aria-hidden="true" />
    </button>
  </span>
</template>

<style scoped>
.sensitive-value {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
}

.sensitive-value code {
  min-width: 0;
  overflow-wrap: anywhere;
  color: inherit;
  font: inherit;
}

.sensitive-value button {
  display: inline-grid;
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  place-items: center;
  color: var(--muted);
  background: transparent;
  border-radius: 4px;
}

.sensitive-value button:hover,
.sensitive-value button:focus-visible {
  color: var(--accent-strong);
  background: var(--surface-hover);
}

.sensitive-value button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
</style>
