<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string
    titleId: string
    description?: string
    iconTone?: 'accent' | 'blue' | 'danger' | 'warning' | 'neutral'
    mode?: 'preferences' | 'modules'
  }>(),
  {
    description: '',
    iconTone: 'accent',
    mode: 'preferences'
  }
)
</script>

<template>
  <section
    class="settings-section"
    :class="`settings-section--${mode}`"
    :aria-labelledby="titleId"
  >
    <header class="settings-section__header">
      <span
        v-if="$slots.icon"
        class="settings-section__icon"
        :class="`settings-section__icon--${iconTone}`"
        aria-hidden="true"
      >
        <slot name="icon" />
      </span>
      <div class="settings-section__copy">
        <h3 :id="titleId">{{ title }}</h3>
        <p v-if="description">{{ description }}</p>
      </div>
      <slot name="status" />
    </header>
    <div class="settings-section__body">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.settings-section {
  width: 100%;
  max-width: var(--settings-preference-content-max);
  min-width: 0;
}

.settings-section--modules {
  max-width: none;
}

.settings-section__header {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.settings-section__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.settings-section__icon--blue {
  color: var(--blue);
  background: var(--blue-soft);
}

.settings-section__icon--danger {
  color: var(--danger);
  background: var(--danger-soft);
}

.settings-section__icon--warning {
  color: var(--warning-strong);
  background: var(--warning-soft);
}

.settings-section__icon--neutral {
  color: var(--muted);
  background: var(--surface-hover);
}

.settings-section__copy {
  min-width: 0;
  flex: 1;
}

.settings-section__copy h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.settings-section__copy p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.45;
}
</style>
