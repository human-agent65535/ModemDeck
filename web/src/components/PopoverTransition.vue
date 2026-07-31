<template>
  <Transition name="popover-motion">
    <slot />
  </Transition>
</template>

<style>
.popover-motion-enter-active,
.popover-motion-leave-active {
  transform-origin: top center;
  transition:
    opacity var(--motion-fast) var(--ease-standard),
    transform var(--motion-base) var(--ease-emphasized);
}

.popover-motion-enter-active.is-up,
.popover-motion-leave-active.is-up {
  transform-origin: bottom center;
}

.popover-motion-enter-from,
.popover-motion-leave-to {
  opacity: 0;
  transform: translateY(-4px) scale(0.985);
}

.popover-motion-enter-from.is-up,
.popover-motion-leave-to.is-up {
  transform: translateY(4px) scale(0.985);
}

.popover-motion-leave-active {
  pointer-events: none;
}

@media (prefers-reduced-motion: reduce) {
  .popover-motion-enter-active,
  .popover-motion-leave-active {
    transition: none;
  }

  .popover-motion-enter-from,
  .popover-motion-leave-to {
    transform: none;
  }
}
</style>
