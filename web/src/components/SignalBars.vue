<script setup lang="ts">
import { Plane } from '@lucide/vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    value: number | null | undefined
    flightMode?: boolean
  }>(),
  {
    flightMode: false
  }
)

const barCount = 4
const isKnown = computed(
  () => props.value !== null && props.value !== undefined && Number.isFinite(props.value)
)
const activeBars = computed(() => {
  if (!isKnown.value || props.value === null || props.value === undefined || props.value <= 0) {
    return 0
  }
  return Math.ceil(Math.min(props.value, 100) / (100 / barCount))
})
const stateClass = computed(() => {
  if (props.flightMode) return 'is-flight-mode'
  if (!isKnown.value) return 'is-unknown'
  if (activeBars.value === 0) return 'is-zero'
  return `is-level-${activeBars.value}`
})
const accessibleLabel = computed(() => {
  if (props.flightMode) return t('device.flightMode')
  if (!isKnown.value) return t('signal.unknown')
  if (activeBars.value === 0) return t('signal.none', { value: props.value })
  return t('signal.level', {
    value: props.value,
    total: barCount,
    active: activeBars.value
  })
})
</script>

<template>
  <span
    class="signal-bars"
    :class="stateClass"
    role="img"
    :aria-label="accessibleLabel"
  >
    <Plane
      v-if="flightMode"
      class="signal-bars__flight-icon"
      :size="17"
      :stroke-width="1.8"
      aria-hidden="true"
    />
    <template v-else>
      <span
        v-for="bar in barCount"
        :key="bar"
        class="signal-bars__bar"
        :class="{ 'is-active': bar <= activeBars }"
        aria-hidden="true"
      />
    </template>
  </span>
</template>

<style scoped>
.signal-bars {
  position: relative;
  display: inline-flex;
  width: 19px;
  height: 14px;
  flex: 0 0 19px;
  align-items: flex-end;
  justify-content: space-between;
}

.signal-bars__bar {
  width: 3px;
  background: color-mix(in srgb, var(--muted) 24%, transparent);
  border-radius: 1px 1px 0 0;
}

.signal-bars__bar:nth-child(1) {
  height: 4px;
}

.signal-bars__bar:nth-child(2) {
  height: 7px;
}

.signal-bars__bar:nth-child(3) {
  height: 10px;
}

.signal-bars__bar:nth-child(4) {
  height: 14px;
}

.signal-bars__bar.is-active {
  background: var(--accent-strong);
}

.signal-bars.is-unknown .signal-bars__bar {
  background: transparent;
  border: 1px dashed color-mix(in srgb, var(--muted) 62%, transparent);
}

.signal-bars.is-zero .signal-bars__bar {
  background: color-mix(in srgb, var(--danger) 28%, transparent);
}

.signal-bars.is-flight-mode {
  height: 17px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
}

.signal-bars__flight-icon {
  display: block;
}
</style>
