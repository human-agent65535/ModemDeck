<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from '@lucide/vue'
import OverlayDialog from './OverlayDialog.vue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const applePlatform = /mac|iphone|ipad/i.test(globalThis.navigator?.platform || '')

const shortcuts = computed(() => [
  { keys: [applePlatform ? '⌘' : 'Ctrl', 'K'], label: t('shortcuts.search') },
  { keys: ['/'], label: t('shortcuts.search') },
  { keys: ['↑', '↓'], label: t('shortcuts.moveInList') },
  { keys: ['Enter'], label: t('shortcuts.openItem') },
  { keys: ['C'], label: t('shortcuts.newMessage') },
  { keys: ['D'], label: t('shortcuts.dial') },
  { keys: ['?'], label: t('shortcuts.showShortcuts') }
])
</script>

<template>
  <OverlayDialog
    :open="open"
    size="small"
    placement="center"
    labelledby="keyboard-shortcuts-title"
    @close="emit('close')"
  >
    <section class="keyboard-shortcuts">
      <header>
        <h2 id="keyboard-shortcuts-title">{{ t('shortcuts.title') }}</h2>
        <button
          class="icon-button"
          type="button"
          :title="t('common.close')"
          :aria-label="t('common.close')"
          @click="emit('close')"
        >
          <X :size="19" />
        </button>
      </header>
      <dl>
        <div v-for="shortcut in shortcuts" :key="shortcut.keys.join('+')">
          <dt>
            <kbd v-for="key in shortcut.keys" :key="key">{{ key }}</kbd>
          </dt>
          <dd>{{ shortcut.label }}</dd>
        </div>
      </dl>
    </section>
  </OverlayDialog>
</template>

<style scoped>
.keyboard-shortcuts {
  padding: var(--space-2) var(--space-4) var(--space-4);
}

.keyboard-shortcuts header {
  display: flex;
  min-height: 52px;
  align-items: center;
  justify-content: space-between;
}

.keyboard-shortcuts h2 {
  font-size: var(--text-md);
}

.keyboard-shortcuts dl {
  display: grid;
  margin: 0;
}

.keyboard-shortcuts dl > div {
  display: flex;
  min-height: 40px;
  align-items: center;
  gap: var(--space-4);
  border-top: 1px solid var(--border);
}

.keyboard-shortcuts dt {
  display: flex;
  min-width: 88px;
  gap: var(--space-1);
}

.keyboard-shortcuts dd {
  margin: 0;
  color: var(--muted-strong);
  font-size: var(--text-base);
}

.keyboard-shortcuts kbd {
  min-width: 24px;
  padding: 1px var(--space-2);
  color: var(--text);
  font: inherit;
  font-size: var(--text-xs);
  text-align: center;
  background: var(--surface-subtle);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-xs);
}
</style>
