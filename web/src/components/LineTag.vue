<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LineSummary } from '../api/types'
import { lineTone } from '../utils/lineTone'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    line: Pick<LineSummary, 'id' | 'iccid' | 'line_label' | 'line_color'>
    fallback?: string
  }>(),
  {
    fallback: ''
  }
)

const label = computed(() => props.line.line_label.trim() || props.fallback.trim() || t('lines.line'))
const tone = computed(() => lineTone(props.line, label.value))
const toneStyle = computed(() => ({
  color: tone.value.foreground,
  backgroundColor: tone.value.background,
  borderColor: tone.value.border
}))
</script>

<template>
  <span
    class="line-tag"
    :style="toneStyle"
    :title="label"
    :aria-label="t('lines.communicationLine', { label })"
  >
    {{ label }}
  </span>
</template>

<style scoped>
.line-tag {
  display: inline-flex;
  width: fit-content;
  max-width: 104px;
  height: 22px;
  flex: 0 0 auto;
  align-items: center;
  padding: 0 7px;
  overflow: hidden;
  font-size: 12px;
  font-weight: 700;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: 4px;
}

</style>
