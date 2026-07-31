<script setup lang="ts">
import { CircleAlert, CircleCheck, Info, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  dismissFeedback,
  feedbackState,
  type FeedbackTone
} from '../state/feedback'

const { t } = useI18n()

function iconFor(tone: FeedbackTone): typeof CircleCheck {
  if (tone === 'error') return CircleAlert
  if (tone === 'info') return Info
  return CircleCheck
}
</script>

<template>
  <Teleport to="body">
    <div class="feedback-host" aria-live="polite" aria-relevant="additions">
      <TransitionGroup name="feedback">
        <article
          v-for="item in feedbackState.items"
          :key="item.id"
          class="feedback-toast"
          :class="`is-${item.tone}`"
          :role="item.tone === 'error' ? 'alert' : 'status'"
          aria-atomic="true"
        >
          <component :is="iconFor(item.tone)" :size="19" aria-hidden="true" />
          <p>{{ item.message }}</p>
          <button
            type="button"
            :title="t('common.close')"
            :aria-label="t('common.close')"
            @click="dismissFeedback(item.id)"
          >
            <X :size="17" />
          </button>
        </article>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.feedback-host {
  position: fixed;
  z-index: var(--layer-feedback);
  top: calc(var(--header-height) + 14px);
  right: 18px;
  display: flex;
  width: min(380px, calc(100vw - 32px));
  flex-direction: column;
  gap: 9px;
  pointer-events: none;
}

.feedback-toast {
  display: grid;
  min-height: 52px;
  align-items: center;
  gap: 10px;
  padding: 10px 10px 10px 14px;
  color: var(--text);
  background: rgb(255 255 255 / 98%);
  border: 1px solid var(--border);
  border-left: 4px solid var(--accent);
  border-radius: 10px;
  box-shadow: var(--shadow-lg);
  grid-template-columns: auto minmax(0, 1fr) auto;
  pointer-events: auto;
}

.feedback-toast > svg {
  color: var(--success);
}

.feedback-toast.is-error {
  border-left-color: var(--danger);
}

.feedback-toast.is-error > svg {
  color: var(--danger);
}

.feedback-toast.is-info {
  border-left-color: var(--blue);
}

.feedback-toast.is-info > svg {
  color: var(--blue);
}

.feedback-toast p {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
}

.feedback-toast button {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--muted);
  background: transparent;
  border-radius: 50%;
}

.feedback-toast button:hover {
  color: var(--text);
  background: var(--surface-hover);
}

.feedback-enter-active,
.feedback-leave-active {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-base) var(--ease-standard);
}

.feedback-enter-from,
.feedback-leave-to {
  opacity: 0;
  transform: translateY(-8px) scale(0.98);
}

.feedback-move {
  transition: transform var(--motion-base) var(--ease-standard);
}

@media (max-width: 860px) {
  .feedback-host {
    top: auto;
    right: 50%;
    bottom: calc(var(--mobile-nav-height) + 14px);
    transform: translateX(50%);
  }
}
</style>
