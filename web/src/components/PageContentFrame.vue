<script setup lang="ts">
withDefaults(
  defineProps<{
    mode?: 'reading' | 'dashboard' | 'fluid'
    as?: 'div' | 'section'
  }>(),
  {
    mode: 'reading',
    as: 'div'
  }
)
</script>

<template>
  <component
    :is="as"
    class="page-content-frame"
    :class="`page-content-frame--${mode}`"
  >
    <slot />
  </component>
</template>

<style scoped>
.page-content-frame {
  width: 100%;
  min-width: 0;
  margin-inline: auto;
}

.page-content-frame--reading {
  max-width: var(--page-content-reading-max);
}

.page-content-frame--dashboard {
  max-width: var(--page-content-dashboard-max);
}

.page-content-frame--fluid {
  max-width: none;
  margin-inline: 0;
}
</style>
