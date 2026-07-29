<script setup lang="ts">
import { Mail, MailOpen, Trash2 } from '@lucide/vue'
import { computed, onBeforeUnmount, ref } from 'vue'
import { activateSwipeRow, clearSwipeRow } from '../state/swipeActions'

const ACTION_WIDTH = 84
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
const dragging = ref(false)
const transitioning = ref(false)

let pointerID = -1
let startX = 0
let startY = 0
let startOffset = 0
let axis: '' | 'horizontal' | 'vertical' = ''
let suppressClick = false

const surfaceStyle = computed(() => ({
  transform: `translate3d(${offset.value}px, 0, 0)`
}))

function mobileActionsAvailable(): boolean {
  return (
    !props.disabled &&
    typeof window !== 'undefined' &&
    window.matchMedia('(max-width: 1100px)').matches
  )
}

function close(): void {
  offset.value = 0
  openSide.value = ''
  transitioning.value = true
  clearSwipeRow(close)
}

function open(side: Exclude<OpenSide, ''>): void {
  activateSwipeRow(close)
  openSide.value = side
  offset.value = side === 'read' ? ACTION_WIDTH : -ACTION_WIDTH
  transitioning.value = true
}

function finishPointer(): void {
  pointerID = -1
  dragging.value = false
  axis = ''
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
  dragging.value = true
  transitioning.value = false
  surface.value?.setPointerCapture(event.pointerId)
}

function onPointerMove(event: PointerEvent): void {
  if (event.pointerId !== pointerID || !dragging.value) return
  const deltaX = event.clientX - startX
  const deltaY = event.clientY - startY
  if (!axis && Math.max(Math.abs(deltaX), Math.abs(deltaY)) >= 6) {
    axis = Math.abs(deltaX) > Math.abs(deltaY) ? 'horizontal' : 'vertical'
  }
  if (axis === 'vertical') return
  if (axis !== 'horizontal') return
  event.preventDefault()
  suppressClick = true
  const maximum = props.canRead ? ACTION_WIDTH : 0
  offset.value = Math.max(-ACTION_WIDTH, Math.min(maximum, startOffset + deltaX))
}

function onPointerEnd(event: PointerEvent): void {
  if (event.pointerId !== pointerID) return
  if (axis === 'horizontal') {
    if (offset.value <= -ACTION_WIDTH * 0.44) open('delete')
    else if (props.canRead && offset.value >= ACTION_WIDTH * 0.44) open('read')
    else close()
  }
  finishPointer()
  if (suppressClick) {
    window.setTimeout(() => {
      suppressClick = false
    }, 0)
  }
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
      'is-open': openSide
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
      @pointercancel="onPointerEnd"
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
  color: #fff;
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
    transition: transform 180ms ease-out;
  }

  .swipe-action-row.is-dragging .swipe-action-row__surface {
    transition: none;
  }

  .swipe-action-row__action {
    display: flex;
  }
}

@media (prefers-reduced-motion: reduce) {
  .swipe-action-row.is-transitioning .swipe-action-row__surface {
    transition: none;
  }
}
</style>
