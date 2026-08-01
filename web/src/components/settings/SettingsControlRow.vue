<script setup lang="ts">
withDefaults(
  defineProps<{
    title: string
    description?: string
    as?: 'div' | 'label'
  }>(),
  {
    description: '',
    as: 'div'
  }
)
</script>

<template>
  <component :is="as" class="settings-control-row">
    <span class="settings-control-row__identity">
      <slot name="icon" />
      <span>
        <strong>{{ title }}</strong>
        <small v-if="description">{{ description }}</small>
      </span>
    </span>
    <span class="settings-control-row__control">
      <slot />
    </span>
  </component>
</template>

<style scoped>
.settings-control-row {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--border);
}

.settings-control-row__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 11px;
}

.settings-control-row__identity > :deep(svg) {
  flex: 0 0 auto;
  color: var(--muted);
}

.settings-control-row__identity > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.settings-control-row strong {
  color: var(--text);
  font-size: 13px;
}

.settings-control-row small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings-control-row__control {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
}

@media (max-width: 860px) {
  .settings-control-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
    padding: 13px 0;
  }

  .settings-control-row__control {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>
