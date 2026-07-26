<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LineSummary } from '../api/types'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    line: Pick<LineSummary, 'id' | 'iccid' | 'line_label'>
    fallback?: string
  }>(),
  {
    fallback: ''
  }
)

function stableHash(value: string): number {
  let hash = 2166136261
  for (const character of value) {
    hash ^= character.codePointAt(0) || 0
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

const label = computed(() => props.line.line_label.trim() || props.fallback.trim() || t('lines.line'))
const tone = computed(() => {
  const stableKey = props.line.id?.trim() || props.line.iccid.trim() || label.value
  return `line-tag--tone-${stableHash(stableKey) % 6}`
})
</script>

<template>
  <span
    class="line-tag"
    :class="tone"
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

.line-tag--tone-0 {
  color: #075e54;
  background: #e0f2ef;
  border-color: #a7d8d1;
}

.line-tag--tone-1 {
  color: #20558c;
  background: #e7f0fa;
  border-color: #b8d0e9;
}

.line-tag--tone-2 {
  color: #7a4b00;
  background: #fff2d6;
  border-color: #e9cf93;
}

.line-tag--tone-3 {
  color: #8a3448;
  background: #fae9ed;
  border-color: #e4b9c3;
}

.line-tag--tone-4 {
  color: #3e6a25;
  background: #eaf3e4;
  border-color: #bed4ae;
}

.line-tag--tone-5 {
  color: #5c4b8a;
  background: #efebf8;
  border-color: #cbc1e2;
}
</style>
