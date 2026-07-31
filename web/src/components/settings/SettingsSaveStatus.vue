<script setup lang="ts">
import { Check, CircleAlert, LoaderCircle } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SettingsMutationStatus } from '../../composables/useSettingsMutation'

const props = withDefaults(
  defineProps<{
    status: SettingsMutationStatus
    error?: string
    compact?: boolean
    savedLabel?: string
    savingLabel?: string
  }>(),
  {
    error: '',
    compact: false,
    savedLabel: '',
    savingLabel: ''
  }
)

const { t } = useI18n()
const label = computed(() => {
  if (props.status === 'saving') return props.savingLabel || t('common.saving')
  if (props.status === 'saved') return props.savedLabel || t('common.saved')
  if (props.status === 'error') return props.error
  return ''
})
</script>

<template>
  <span
    v-if="status !== 'idle'"
    class="settings-save-status"
    :class="[`is-${status}`, { 'is-compact': compact }]"
    :role="status === 'error' ? 'alert' : 'status'"
    :title="compact ? label : undefined"
    aria-live="polite"
  >
    <LoaderCircle v-if="status === 'saving'" class="spin" :size="15" />
    <Check v-else-if="status === 'saved'" :size="15" />
    <CircleAlert v-else :size="15" />
    <span v-if="!compact">{{ label }}</span>
    <span v-else class="sr-only">{{ label }}</span>
  </span>
</template>

<style scoped>
.settings-save-status {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
  line-height: 1.35;
}

.settings-save-status.is-saved {
  color: var(--accent-strong);
}

.settings-save-status.is-error {
  color: var(--danger);
}

.settings-save-status.is-compact {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  justify-content: center;
  border-radius: 50%;
}

.settings-save-status.is-compact.is-saved {
  background: var(--accent-soft);
  animation: settings-save-status-pop 180ms ease-out;
}

@keyframes settings-save-status-pop {
  from {
    opacity: 0;
    transform: scale(0.78);
  }
}

@media (prefers-reduced-motion: reduce) {
  .settings-save-status.is-compact.is-saved {
    animation: none;
  }
}
</style>
