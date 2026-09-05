<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import {
  overlayIsTopmost,
  registerOverlay,
  unregisterOverlay
} from '../state/overlay'

const props = withDefaults(
  defineProps<{
    open: boolean
    label?: string
    labelledby?: string
    describedby?: string
    size?: 'small' | 'medium' | 'large'
    placement?: 'center' | 'responsive-sheet'
    layer?: 'dialog' | 'critical'
    initialFocus?: string
    closeOnBackdrop?: boolean
    closeOnEscape?: boolean
    surfaceClass?: string
  }>(),
  {
    label: '',
    labelledby: '',
    describedby: '',
    size: 'medium',
    placement: 'responsive-sheet',
    layer: 'dialog',
    initialFocus: '',
    closeOnBackdrop: true,
    closeOnEscape: true,
    surfaceClass: ''
  }
)

const emit = defineEmits<{
  close: []
}>()

const overlayID = `overlay-${useId()}`
const surface = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null
let registered = false

function focusableElements(): HTMLElement[] {
  if (!surface.value) return []
  return Array.from(
    surface.value.querySelectorAll<HTMLElement>(
      [
        'a[href]',
        'button:not([disabled])',
        'input:not([disabled])',
        'select:not([disabled])',
        'textarea:not([disabled])',
        '[tabindex]:not([tabindex="-1"])'
      ].join(', ')
    )
  ).filter(element => !element.hidden && element.getClientRects().length > 0)
}

function focusInitialControl(): void {
  if (!surface.value) return
  const requested = props.initialFocus
    ? surface.value.querySelector<HTMLElement>(props.initialFocus)
    : null
  const target =
    requested ||
    surface.value.querySelector<HTMLElement>('[data-overlay-initial]') ||
    focusableElements()[0] ||
    surface.value
  target.focus()
}

function requestClose(): void {
  if (!overlayIsTopmost(overlayID)) return
  emit('close')
}

function onKeydown(event: KeyboardEvent): void {
  if (!overlayIsTopmost(overlayID)) return
  if (event.key === 'Escape' && props.closeOnEscape && !event.defaultPrevented) {
    event.preventDefault()
    event.stopPropagation()
    emit('close')
    return
  }
  if (event.key !== 'Tab') return

  const elements = focusableElements()
  const first = elements[0]
  const last = elements[elements.length - 1]
  if (!first || !last) {
    event.preventDefault()
    surface.value?.focus()
    return
  }
  const active = document.activeElement
  if (event.shiftKey && (active === first || active === surface.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

watch(
  () => props.open,
  async (open, wasOpen) => {
    if (open) {
      previousFocus =
        typeof document === 'undefined'
          ? null
          : (document.activeElement as HTMLElement | null)
      registerOverlay(overlayID)
      registered = true
      await nextTick()
      focusInitialControl()
      return
    }
    if (!wasOpen) return
    if (registered) {
      unregisterOverlay(overlayID)
      registered = false
    }
    await nextTick()
    if (previousFocus?.isConnected) previousFocus.focus()
    previousFocus = null
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  if (registered) unregisterOverlay(overlayID)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="overlay">
      <div
        v-if="open"
        class="overlay-backdrop"
        :class="[
          `overlay-backdrop--${placement}`,
          `overlay-backdrop--${layer}`
        ]"
        role="presentation"
        @mousedown.self="closeOnBackdrop && requestClose()"
      >
        <section
          ref="surface"
          class="overlay-dialog"
          :class="[
            `overlay-dialog--${size}`,
            `overlay-dialog--${placement}`,
            surfaceClass
          ]"
          role="dialog"
          aria-modal="true"
          :aria-label="label || undefined"
          :aria-labelledby="labelledby || undefined"
          :aria-describedby="describedby || undefined"
          tabindex="-1"
          @keydown="onKeydown"
        >
          <slot />
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay-backdrop {
  position: fixed;
  z-index: var(--layer-dialog);
  inset: 0;
  display: grid;
  padding: var(--space-5);
  overflow: hidden;
  place-items: center;
  background: rgb(16 24 40 / 46%);
  backdrop-filter: blur(1.5px);
}

.overlay-backdrop--critical {
  z-index: var(--layer-critical);
}

.overlay-dialog {
  width: min(100%, var(--overlay-width));
  max-height: calc(100dvh - (var(--space-5) * 2));
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-dialog);
  box-shadow: var(--shadow-lg);
}

.overlay-dialog--small {
  --overlay-width: 420px;
}

.overlay-dialog--medium {
  --overlay-width: 560px;
}

.overlay-dialog--large {
  --overlay-width: 620px;
}

.overlay-enter-active,
.overlay-leave-active {
  transition: opacity var(--motion-base) var(--ease-standard);
}

.overlay-enter-active .overlay-dialog,
.overlay-leave-active .overlay-dialog {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-slow) var(--ease-emphasized);
}

.overlay-enter-from,
.overlay-leave-to,
.overlay-enter-from .overlay-dialog,
.overlay-leave-to .overlay-dialog {
  opacity: 0;
}

.overlay-enter-from .overlay-dialog,
.overlay-leave-to .overlay-dialog {
  transform: translateY(var(--space-3)) scale(0.985);
}

.overlay-leave-active {
  pointer-events: none;
}

@media (max-width: 560px) {
  .overlay-backdrop--responsive-sheet {
    align-items: end;
    padding: 0 0 var(--mobile-nav-height);
  }

  .overlay-dialog--responsive-sheet {
    width: 100%;
    max-height: calc(100dvh - var(--mobile-nav-height));
    border-right: 0;
    border-bottom: 0;
    border-left: 0;
    border-radius: var(--radius-dialog) var(--radius-dialog) 0 0;
  }

  .overlay-enter-from .overlay-dialog--responsive-sheet,
  .overlay-leave-to .overlay-dialog--responsive-sheet {
    transform: translateY(var(--space-5));
  }
}

@media (prefers-reduced-motion: reduce) {
  .overlay-enter-active,
  .overlay-leave-active,
  .overlay-enter-active .overlay-dialog,
  .overlay-leave-active .overlay-dialog {
    transition: none;
  }
}
</style>
