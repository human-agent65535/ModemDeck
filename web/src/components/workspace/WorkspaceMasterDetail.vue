<script setup lang="ts">
withDefaults(
  defineProps<{
    hasSelection?: boolean
    batchSelecting?: boolean
    embedded?: boolean
    listClass?: string
  }>(),
  {
    hasSelection: false,
    batchSelecting: false,
    embedded: false,
    listClass: ''
  }
)
</script>

<template>
  <section
    class="workspace workspace-master-detail"
    :class="{
      'has-selection': hasSelection,
      'is-batch-selecting': batchSelecting,
      'is-embedded': embedded
    }"
  >
    <aside v-if="!embedded" class="list-pane" :class="listClass">
      <slot name="list" />
    </aside>
    <slot name="detail" />
  </section>
</template>
