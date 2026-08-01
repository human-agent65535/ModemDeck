<script setup lang="ts">
import { MoreHorizontal } from '@lucide/vue'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const overflowOpen = ref(false)

function closeOverflow(): void {
  overflowOpen.value = false
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (root.value?.contains(event.target as Node)) return
  closeOverflow()
}

function onDocumentFocusIn(event: FocusEvent): void {
  if (root.value?.contains(event.target as Node)) return
  closeOverflow()
}

function onDocumentKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || !overflowOpen.value) return
  event.preventDefault()
  closeOverflow()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown)
  document.addEventListener('focusin', onDocumentFocusIn)
  document.addEventListener('keydown', onDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown)
  document.removeEventListener('focusin', onDocumentFocusIn)
  document.removeEventListener('keydown', onDocumentKeydown)
})
</script>

<template>
  <div ref="root" class="workspace-detail-actions">
    <div class="workspace-detail-actions__primary">
      <slot name="primary" />
    </div>
    <button
      v-if="$slots.secondary"
      class="icon-button workspace-detail-actions__more"
      type="button"
      :title="t('common.more')"
      :aria-label="t('common.more')"
      :aria-expanded="overflowOpen"
      @click="overflowOpen = !overflowOpen"
    >
      <MoreHorizontal :size="20" />
    </button>
    <div
      v-if="$slots.secondary"
      class="workspace-detail-actions__secondary"
      :class="{ 'is-open': overflowOpen }"
      @click="closeOverflow"
    >
      <slot name="secondary" />
    </div>
  </div>
</template>

<style scoped>
.workspace-detail-actions,
.workspace-detail-actions__primary,
.workspace-detail-actions__secondary {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--detail-action-gap);
}

.workspace-detail-actions {
  position: relative;
}

.workspace-detail-actions__more {
  display: none;
}

@media (max-width: 560px) {
  .workspace-detail-actions__more {
    display: inline-grid;
  }

  .workspace-detail-actions__secondary {
    position: absolute;
    z-index: var(--layer-popover);
    top: calc(100% + var(--space-2));
    right: 0;
    display: flex;
    min-width: 190px;
    align-items: stretch;
    flex-direction: column;
    padding: var(--space-2);
    visibility: hidden;
    opacity: 0;
    pointer-events: none;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-control);
    box-shadow: var(--shadow);
    transform: translateY(-4px) scale(0.985);
    transform-origin: top right;
    transition:
      opacity var(--motion-fast) var(--ease-standard),
      transform var(--motion-base) var(--ease-emphasized),
      visibility 0s linear var(--motion-base);
  }

  .workspace-detail-actions__secondary.is-open {
    visibility: visible;
    opacity: 1;
    pointer-events: auto;
    transform: none;
    transition-delay: 0s;
  }

}

@media (prefers-reduced-motion: reduce) {
  .workspace-detail-actions__secondary {
    transform: none;
    transition: none;
  }
}
</style>

<style>
.workspace-detail-command {
  display: inline-flex;
  min-width: 72px;
  min-height: var(--detail-action-size);
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: 0 var(--space-3);
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 650;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-control);
}

.workspace-detail-command:hover:not(:disabled) {
  background: var(--surface-hover);
}

.workspace-detail-command--primary {
  color: var(--on-accent);
  background: var(--accent);
  border-color: var(--accent);
}

.workspace-detail-command--primary:hover:not(:disabled) {
  background: var(--accent-strong);
  border-color: var(--accent-strong);
}

@container (max-width: 760px) {
  .workspace-detail-command {
    width: var(--detail-action-size);
    min-width: var(--detail-action-size);
    height: var(--detail-action-size);
    min-height: var(--detail-action-size);
    padding: 0;
    border-radius: 50%;
  }

  .workspace-detail-command span {
    display: none;
  }
}

@media (max-width: 560px) {
  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    .contact-number-actions {
    width: 100%;
    align-items: stretch;
    flex-direction: column;
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    .contact-number-actions.is-compact
    .secondary-button {
    width: 100%;
    min-width: 0;
    justify-content: flex-start;
    gap: var(--space-2);
    padding-inline: var(--space-3);
    background: transparent;
    border-color: transparent;
    border-radius: var(--radius-control);
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    .contact-number-actions.is-compact
    .secondary-button:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    .contact-number-actions.is-compact
    .contact-number-action__label {
    display: inline;
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    :is(
      .secondary-button,
      .workspace-detail-command,
      .workspace-favorite-action
    ) {
    width: 100%;
    min-width: 0;
    min-height: var(--touch-target);
    justify-content: flex-start;
    gap: var(--space-2);
    padding-inline: var(--space-3);
    border-radius: var(--radius-control);
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    .workspace-favorite-action {
    display: flex;
    align-items: center;
  }

  .workspace-detail-actions
    .workspace-detail-actions__secondary.is-open
    :is(
      .contact-number-action__label,
      .workspace-detail-command span,
      .workspace-favorite-action__label
    ) {
    display: inline;
  }
}
</style>
