<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LineSummary } from '../api/types'
import { lineTone } from '../utils/lineTone'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    line: Pick<LineSummary, 'id' | 'line_label' | 'line_color'>
    fallback?: string
    /** `dot` is the quiet list-row form: a color dot and the line name. */
    variant?: 'tag' | 'dot'
  }>(),
  {
    fallback: '',
    variant: 'tag'
  }
)

const label = computed(() => props.line.line_label.trim() || props.fallback.trim() || t('lines.line'))
const tone = computed(() => lineTone(props.line))
const toneStyle = computed(() =>
  props.variant === 'dot'
    ? { '--line-tag-dot': tone.value.dot }
    : {
        color: tone.value.foreground,
        backgroundColor: tone.value.background,
        borderColor: tone.value.border
      }
)
</script>

<template>
  <span
    class="line-tag"
    :class="{ 'line-tag--dot': variant === 'dot' }"
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
  font-size: var(--text-xs);
  font-weight: 700;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: var(--radius-xs);
}

.line-tag--dot {
  max-width: 120px;
  height: auto;
  gap: 5px;
  padding: 0;
  color: var(--muted);
  font-weight: var(--weight-medium);
  line-height: var(--leading-xs);
  background: none;
  border: 0;
  border-radius: 0;
}

.line-tag--dot::before {
  width: 6px;
  height: 6px;
  flex: 0 0 6px;
  content: "";
  background: var(--line-tag-dot);
  border-radius: 50%;
}
</style>
