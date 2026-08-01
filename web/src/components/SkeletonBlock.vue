<template>
  <span class="skeleton-block" aria-hidden="true" />
</template>

<style scoped>
.skeleton-block {
  position: relative;
  display: block;
  overflow: hidden;
  background: var(--skeleton);
  border-radius: 6px;
}

.skeleton-block::after {
  position: absolute;
  inset: 0;
  content: "";
  background: linear-gradient(
    90deg,
    transparent,
    rgb(255 255 255 / 68%),
    transparent
  );
  animation: skeleton-block-shimmer 1.25s ease-in-out infinite;
  transform: translateX(-100%);
}

@keyframes skeleton-block-shimmer {
  to {
    transform: translateX(100%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .skeleton-block::after {
    animation: none;
    transform: none;
  }
}
</style>
