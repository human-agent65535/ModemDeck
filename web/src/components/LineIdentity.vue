<script setup lang="ts">
import { computed } from 'vue'
import { CardSim, ListFilter, Star } from '@lucide/vue'
import type { LineSummary } from '../api/types'
import { lineTone } from '../utils/lineTone'

const props = withDefaults(
  defineProps<{
    name: string
    details: string
    line?: LineSummary
    status?: string
    defaultLabel?: string
    isDefault?: boolean
    all?: boolean
    variant?: 'control' | 'option' | 'compact' | 'filter'
  }>(),
  {
    line: undefined,
    status: '',
    defaultLabel: '',
    isDefault: false,
    all: false,
    variant: 'control'
  }
)

const toneStyle = computed<Record<string, string> | undefined>(() => {
  if (!props.line) return undefined
  const tone = lineTone(props.line)
  return {
    '--line-identity-color': tone.foreground,
    '--line-identity-background': tone.background,
    '--line-identity-border': tone.border
  }
})
</script>

<template>
  <span
    class="line-identity"
    :class="[
      `line-identity--${variant}`,
      {
        'has-line-tone': Boolean(line),
        'is-all': all
      }
    ]"
    :style="toneStyle"
  >
    <span class="line-identity__icon" aria-hidden="true">
      <ListFilter v-if="all" :size="variant === 'control' ? 20 : 17" />
      <CardSim v-else :size="variant === 'control' ? 20 : 17" />
    </span>
    <span class="line-identity__copy">
      <span class="line-identity__name-row">
        <strong>{{ name }}</strong>
        <span v-if="isDefault" class="line-identity__default">
          <Star :size="12" fill="currentColor" aria-hidden="true" />
          {{ defaultLabel }}
        </span>
      </span>
      <small>
        <span>{{ details }}</span>
        <em v-if="status">{{ status }}</em>
      </small>
    </span>
  </span>
</template>

<style scoped>
.line-identity {
  display: grid;
  min-width: 0;
  align-items: center;
  grid-template-columns: 38px minmax(0, 1fr);
  gap: 10px;
}

.line-identity__icon {
  display: inline-flex;
  width: 38px;
  height: 38px;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border: 1px solid transparent;
  border-radius: 50%;
}

.line-identity.has-line-tone .line-identity__icon {
  color: var(--line-identity-color);
  background: var(--line-identity-background);
  border-color: var(--line-identity-border);
}

.line-identity__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.line-identity__name-row {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
}

.line-identity__name-row strong,
.line-identity__copy small > span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-identity.is-all .line-identity__copy small > span {
  overflow: visible;
  line-height: 1.3;
  text-overflow: clip;
  white-space: normal;
}

.line-identity__name-row strong {
  color: var(--text);
  font-size: 15px;
  font-weight: 700;
}

.line-identity__copy small {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
}

.line-identity__copy small > em {
  flex: 0 0 auto;
  color: var(--danger);
  font-style: normal;
  font-weight: 700;
}

.line-identity__default {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  padding: 4px 6px;
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 700;
  background: var(--accent-soft);
  border-radius: 5px;
}

.line-identity--option {
  grid-template-columns: 32px minmax(0, 1fr);
}

.line-identity--option .line-identity__icon {
  width: 32px;
  height: 32px;
  background: var(--surface);
  border-color: var(--border);
}

.line-identity--option .line-identity__name-row strong {
  font-size: 14px;
}

.line-identity--option .line-identity__copy small {
  font-size: 12px;
}

.line-identity--option .line-identity__default {
  padding: 0;
  font-size: 11px;
  background: transparent;
}

.line-identity--compact {
  grid-template-columns: 26px minmax(0, 1fr);
  gap: 6px;
}

.line-identity--compact .line-identity__icon,
.line-identity--filter .line-identity__icon {
  width: 26px;
  height: 26px;
  border-radius: 5px;
}

.line-identity--compact .line-identity__name-row strong {
  font-size: 12px;
}

.line-identity--compact .line-identity__copy small,
.line-identity--compact .line-identity__default,
.line-identity--filter .line-identity__copy {
  display: none;
}

.line-identity--filter {
  display: block;
}

.line-identity--filter .line-identity__icon {
  color: var(--muted);
  background: transparent;
}

@media (max-width: 420px) {
  .line-identity__name-row {
    align-items: flex-start;
  }

  .line-identity__name-row strong {
    display: -webkit-box;
    overflow: hidden;
    white-space: normal;
    overflow-wrap: anywhere;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }
}
</style>
