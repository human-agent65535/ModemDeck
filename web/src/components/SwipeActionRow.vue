<script setup lang="ts">
import { Mail, MailOpen, Trash2 } from '@lucide/vue'
import { computed, onBeforeUnmount, ref } from 'vue'
import { activateSwipeRow, clearSwipeRow } from '../state/swipeActions'

const ACTION_WIDTH = 84
const GESTURE_SLOP = 10
const HORIZONTAL_DOMINANCE = 1.25
type OpenSide = '' | 'read' | 'delete'

const props = withDefaults(
  defineProps<{
    readLabel?: string
    deleteLabel: string
    canRead?: boolean
    readMode?: 'read' | 'unread'
    disabled?: boolean
  }>(),
  {
    readLabel: '',
    canRead: false,
    readMode: 'read',
    disabled: false
  }
)
const emit = defineEmits<{
  read: []
  delete: []
}>()

const surface = ref<HTMLElement | null>(null)
const offset = ref(0)
const openSide = ref<OpenSide>('')
const revealedSide = ref<OpenSide>('')
const dragging = ref(false)
const transitioning = ref(false)

let pointerID = -1
let startX = 0
let startY = 0
let startOffset = 0
let axis: '' | 'horizontal' | 'vertical' = ''
let suppressClick = false

const surfaceStyle = computed(() => ({
  transform: offset.value === 0 ? 'none' : `translate3d(${offset.value}px, 0, 0)`
}))

const visibleSide = computed(() => openSide.value || revealedSide.value)

function mobileActionsAvailable(): boolean {
  return (
    !props.disabled &&
    typeof window !== 'undefined' &&
    window.matchMedia('(max-width: 1100px)').matches
  )
}

function motionAllowed(): boolean {
  return (
    typeof window === 'undefined' ||
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

function close(): void {
  const wasOffset = offset.value
  const shouldAnimate = wasOffset !== 0 && motionAllowed()
  offset.value = 0
  openSide.value = ''
  transitioning.value = shouldAnimate
  if (!shouldAnimate) revealedSide.value = ''
  clearSwipeRow(close)
}

function cancel(): void {
  offset.value = 0
  openSide.value = ''
  revealedSide.value = ''
  transitioning.value = false
  clearSwipeRow(close)
}

function open(side: Exclude<OpenSide, ''>): void {
  activateSwipeRow(close)
  openSide.value = side
  revealedSide.value = side
  offset.value = side === 'read' ? ACTION_WIDTH : -ACTION_WIDTH
  transitioning.value = motionAllowed()
}

function finishPointer(event: PointerEvent): void {
  if (surface.value?.hasPointerCapture(event.pointerId)) {
    surface.value.releasePointerCapture(event.pointerId)
  }
  pointerID = -1
  dragging.value = false
  axis = ''
}

function releaseClickSuppression(): void {
  if (!suppressClick) return
  window.setTimeout(() => {
    suppressClick = false
  }, 0)
}

function onPointerDown(event: PointerEvent): void {
  if (
    !mobileActionsAvailable() ||
    (event.pointerType !== 'touch' && event.pointerType !== 'pen') ||
    event.button !== 0
  ) return
  pointerID = event.pointerId
  startX = event.clientX
  startY = event.clientY
  startOffset = offset.value
  axis = ''
  dragging.value = false
  transitioning.value = false
}

function onPointerMove(event: PointerEvent): void {
  if (event.pointerId !== pointerID) return
  const deltaX = event.clientX - startX
  const deltaY = event.clientY - startY
  const absoluteX = Math.abs(deltaX)
  const absoluteY = Math.abs(deltaY)
  if (!axis) {
    if (
      absoluteX >= GESTURE_SLOP &&
      absoluteX >= absoluteY * HORIZONTAL_DOMINANCE
    ) {
      axis = 'horizontal'
      dragging.value = true
      openSide.value = ''
      activateSwipeRow(close)
      surface.value?.setPointerCapture(event.pointerId)
    } else if (absoluteY >= GESTURE_SLOP && absoluteY >= absoluteX) {
      axis = 'vertical'
      cancel()
      finishPointer(event)
      return
    } else {
      return
    }
  }
  if (axis === 'vertical') return
  if (axis !== 'horizontal') return
  event.preventDefault()
  suppressClick = true
  const maximum = props.canRead ? ACTION_WIDTH : 0
  offset.value = Math.max(-ACTION_WIDTH, Math.min(maximum, startOffset + deltaX))
  revealedSide.value =
    offset.value < 0 ? 'delete' : offset.value > 0 && props.canRead ? 'read' : ''
}

function onPointerEnd(event: PointerEvent): void {
  if (event.pointerId !== pointerID) return
  if (axis === 'horizontal') {
    if (offset.value <= -ACTION_WIDTH * 0.44) open('delete')
    else if (props.canRead && offset.value >= ACTION_WIDTH * 0.44) open('read')
    else close()
  }
  finishPointer(event)
  releaseClickSuppression()
}

function onPointerCancel(event: PointerEvent): void {
  if (event.pointerId !== pointerID) return
  cancel()
  finishPointer(event)
  releaseClickSuppression()
}

function onSurfaceTransitionEnd(event: TransitionEvent): void {
  if (event.target !== surface.value || event.propertyName !== 'transform') return
  transitioning.value = false
  if (offset.value === 0) revealedSide.value = ''
}

function onClickCapture(event: MouseEvent): void {
  if (suppressClick) {
    event.preventDefault()
    event.stopPropagation()
    return
  }
  if (openSide.value) {
    event.preventDefault()
    event.stopPropagation()
    close()
  }
}

function triggerRead(): void {
  if (!props.canRead || props.disabled) return
  close()
  emit('read')
}

function triggerDelete(): void {
  if (props.disabled) return
  close()
  emit('delete')
}

onBeforeUnmount(() => {
  clearSwipeRow(close)
})
</script>

<template>
  <div
    class="swipe-action-row"
    :class="{
      'is-dragging': dragging,
      'is-transitioning': transitioning,
      'is-open': openSide,
      'is-revealing-read': visibleSide === 'read',
      'is-revealing-delete': visibleSide === 'delete'
    }"
  >
    <button
      v-if="canRead"
      class="swipe-action-row__action swipe-action-row__action--read"
      type="button"
      :aria-label="readLabel"
      :title="readLabel"
      :tabindex="openSide === 'read' ? 0 : -1"
      :disabled="disabled"
      @click="triggerRead"
    >
      <Mail v-if="readMode === 'unread'" :size="20" aria-hidden="true" />
      <MailOpen v-else :size="20" aria-hidden="true" />
      <span>{{ readLabel }}</span>
    </button>
    <button
      class="swipe-action-row__action swipe-action-row__action--delete"
      type="button"
      :aria-label="deleteLabel"
      :title="deleteLabel"
      :tabindex="openSide === 'delete' ? 0 : -1"
      :disabled="disabled"
      @click="triggerDelete"
    >
      <Trash2 :size="20" aria-hidden="true" />
      <span>{{ deleteLabel }}</span>
    </button>
    <div
      ref="surface"
      class="swipe-action-row__surface"
      :style="surfaceStyle"
      @click.capture="onClickCapture"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerEnd"
      @pointercancel="onPointerCancel"
      @transitionend="onSurfaceTransitionEnd"
    >
      <slot />
    </div>
  </div>
</template>

<style scoped>
.swipe-action-row {
  position: relative;
  min-width: 0;
  background: var(--surface);
}

.swipe-action-row__surface {
  position: relative;
  z-index: 1;
  min-width: 0;
  background: var(--surface);
}

.swipe-action-row__action {
  position: absolute;
  z-index: 0;
  top: 0;
  bottom: 0;
  display: none;
  width: 84px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  color: var(--on-accent);
  font-size: 11px;
  font-weight: 700;
}

.swipe-action-row__action--read {
  left: 0;
  background: var(--accent);
}

.swipe-action-row__action--delete {
  right: 0;
  background: var(--danger);
}

@media (max-width: 1100px) {
  .swipe-action-row {
    overflow: hidden;
  }

  .swipe-action-row__surface {
    touch-action: pan-y;
  }

  .swipe-action-row.is-transitioning .swipe-action-row__surface {
    transition: transform var(--motion-slow) var(--ease-emphasized);
  }

  .swipe-action-row.is-dragging .swipe-action-row__surface {
    transition: none;
  }

  .swipe-action-row.is-revealing-read .swipe-action-row__action--read,
  .swipe-action-row.is-revealing-delete .swipe-action-row__action--delete {
    display: flex;
  }
}

@media (prefers-reduced-motion: reduce) {
  .swipe-action-row.is-transitioning .swipe-action-row__surface {
    transition: none;
  }
}
</style>
