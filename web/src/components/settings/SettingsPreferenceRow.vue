<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string
    titleId: string
    description?: string
    iconTone?: 'accent' | 'danger' | 'warning' | 'neutral' | 'bare'
    controlSize?: 'auto' | 'wide'
  }>(),
  {
    description: '',
    iconTone: 'accent',
    controlSize: 'auto'
  }
)
</script>

<template>
  <section class="settings-preference-row" :aria-labelledby="titleId">
    <div class="settings-preference-row__main">
      <span
        v-if="$slots.icon"
        class="settings-preference-row__icon"
        :class="`settings-preference-row__icon--${iconTone}`"
        aria-hidden="true"
      >
        <slot name="icon" />
      </span>
      <div class="settings-preference-row__copy">
        <h3 :id="titleId">{{ title }}</h3>
        <p v-if="description">{{ description }}</p>
      </div>
      <div
        class="settings-preference-row__control"
        :class="`settings-preference-row__control--${controlSize}`"
      >
        <slot name="control" />
      </div>
    </div>
    <div v-if="$slots.feedback" class="settings-preference-row__feedback">
      <slot name="feedback" />
    </div>
  </section>
</template>

<style scoped>
.settings-preference-row {
  width: 100%;
  max-width: var(--settings-preference-content-max);
  padding-bottom: 22px;
  border-bottom: 1px solid var(--border);
}

.settings-preference-row__main {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 11px;
}

.settings-preference-row__icon {
  display: inline-grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.settings-preference-row__icon--danger {
  color: var(--danger);
  background: var(--danger-soft);
}

.settings-preference-row__icon--warning {
  color: var(--warning);
  background: var(--warning-soft);
}

.settings-preference-row__icon--neutral {
  color: var(--muted);
  background: var(--surface-subtle);
}

.settings-preference-row__icon--bare {
  color: inherit;
  background: transparent;
}

.settings-preference-row__copy {
  min-width: 0;
  flex: 1;
}

.settings-preference-row__copy h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  text-transform: none;
}

.settings-preference-row__copy p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.4;
}

.settings-preference-row__control {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
}

.settings-preference-row__control--wide {
  width: var(--settings-control-column);
}

.settings-preference-row__feedback {
  margin-top: 10px;
  font-size: 11px;
}

@media (max-width: 860px) {
  .settings-preference-row__main {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .settings-preference-row__control--wide {
    width: calc(100% - 47px);
    margin-left: 47px;
  }
}
</style>
