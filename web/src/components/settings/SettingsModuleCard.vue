<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    title: string
    titleId: string
    description?: string
    iconTone?: 'accent' | 'blue' | 'brand' | 'danger' | 'warning' | 'neutral'
    surface?: 'default' | 'subtle'
    hasBody?: boolean
    headingLevel?: 3 | 4
  }>(),
  {
    description: '',
    iconTone: 'accent',
    surface: 'default',
    hasBody: true,
    headingLevel: 3
  }
)

const headingTag = computed(() => `h${props.headingLevel}`)
</script>

<template>
  <section
    class="settings-module-card"
    :class="`settings-module-card--${surface}`"
    :aria-labelledby="titleId"
  >
    <header class="settings-module-card__header">
      <span
        v-if="$slots.icon"
        class="settings-module-card__icon"
        :class="`settings-module-card__icon--${iconTone}`"
        aria-hidden="true"
      >
        <slot name="icon" />
      </span>
      <div class="settings-module-card__copy">
        <component
          :is="headingTag"
          :id="titleId"
          class="settings-module-card__title"
        >
          {{ title }}
        </component>
        <p v-if="description">{{ description }}</p>
      </div>
      <slot name="status" />
    </header>
    <div v-if="hasBody" class="settings-module-card__body">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.settings-module-card {
  padding: 18px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.settings-module-card--subtle {
  background: var(--surface-subtle);
}

.settings-module-card__header {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 11px;
}

.settings-module-card__icon {
  display: inline-grid;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: var(--radius-sm);
}

.settings-module-card__icon--danger {
  color: var(--danger);
  background: var(--danger-soft);
}

.settings-module-card__icon--blue {
  color: var(--blue);
  background: var(--blue-soft);
}

.settings-module-card__icon--brand {
  color: var(--on-accent);
  font-size: var(--text-lg);
  font-weight: 750;
  background: linear-gradient(145deg, var(--accent), var(--accent-strong));
  box-shadow: 0 6px 18px color-mix(in srgb, var(--accent) 18%, transparent);
}

.settings-module-card__icon--warning {
  color: var(--warning);
  background: var(--warning-soft);
}

.settings-module-card__icon--neutral {
  color: var(--muted);
  background: var(--surface);
}

.settings-module-card__copy {
  min-width: 0;
  flex: 1;
}

.settings-module-card__title {
  margin: 0;
  color: var(--text);
  font-size: var(--text-md);
  text-transform: none;
}

.settings-module-card__copy p {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.5;
}

.settings-module-card__body {
  margin-top: 16px;
}

@media (max-width: 640px) {
  .settings-module-card__header {
    align-items: flex-start;
    flex-wrap: wrap;
  }
}
</style>
