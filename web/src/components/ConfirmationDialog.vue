<script setup lang="ts">
import { AlertTriangle } from '@lucide/vue'
import { nextTick, ref, useId, watch } from 'vue'
import { answerConfirmation, confirmationState } from '../state/confirmation'

const dialog = ref<HTMLElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
const titleID = `confirmation-title-${useId()}`
const messageID = `confirmation-message-${useId()}`
let previousFocus: HTMLElement | null = null

function focusableElements(): HTMLElement[] {
  if (!dialog.value) return []
  return Array.from(
    dialog.value.querySelectorAll<HTMLElement>('button:not([disabled]), [tabindex="0"]')
  )
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    answerConfirmation(false)
    return
  }
  if (event.key !== 'Tab') return

  const elements = focusableElements()
  const first = elements[0]
  const last = elements[elements.length - 1]
  if (!first || !last) {
    event.preventDefault()
    dialog.value?.focus()
    return
  }
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

watch(
  () => confirmationState.request,
  async (request, previousRequest) => {
    if (request) {
      previousFocus =
        typeof document === 'undefined'
          ? null
          : (document.activeElement as HTMLElement | null)
      await nextTick()
      cancelButton.value?.focus()
      return
    }
    if (previousRequest) {
      await nextTick()
      previousFocus?.focus()
      previousFocus = null
    }
  }
)
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="confirmationState.request"
        class="confirmation-backdrop"
        role="presentation"
        @mousedown.self="answerConfirmation(false)"
      >
        <section
          ref="dialog"
          class="confirmation-dialog"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleID"
          :aria-describedby="confirmationState.request.message ? messageID : undefined"
          tabindex="-1"
          @keydown="onKeydown"
        >
          <div
            class="confirmation-dialog__icon"
            :class="{ 'is-danger': confirmationState.request.tone === 'danger' }"
            aria-hidden="true"
          >
            <AlertTriangle :size="21" />
          </div>
          <div class="confirmation-dialog__body">
            <h2 :id="titleID">{{ confirmationState.request.title }}</h2>
            <p v-if="confirmationState.request.message" :id="messageID">
              {{ confirmationState.request.message }}
            </p>
          </div>
          <footer>
            <button
              ref="cancelButton"
              class="confirmation-action is-secondary"
              type="button"
              @click="answerConfirmation(false)"
            >
              {{ confirmationState.request.cancelLabel }}
            </button>
            <button
              class="confirmation-action"
              :class="{ 'is-danger': confirmationState.request.tone === 'danger' }"
              type="button"
              @click="answerConfirmation(true)"
            >
              {{ confirmationState.request.confirmLabel }}
            </button>
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.confirmation-backdrop {
  position: fixed;
  z-index: 180;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(16 24 40 / 46%);
}

.confirmation-dialog {
  display: grid;
  width: min(420px, 100%);
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  padding: 22px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
}

.confirmation-dialog__icon {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  color: #8a5a25;
  background: #fff3da;
  border-radius: 50%;
}

.confirmation-dialog__icon.is-danger {
  color: var(--danger);
  background: var(--danger-soft);
}

.confirmation-dialog__body {
  min-width: 0;
  padding-top: 1px;
}

.confirmation-dialog h2 {
  font-size: 17px;
  line-height: 1.35;
}

.confirmation-dialog p {
  margin-top: 7px;
  color: var(--muted);
  font-size: 13px;
  line-height: 1.55;
}

.confirmation-dialog footer {
  display: flex;
  grid-column: 1 / -1;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 6px;
}

.confirmation-action {
  min-width: 76px;
  min-height: 38px;
  padding: 0 14px;
  color: #fff;
  font-size: 13px;
  font-weight: 650;
  background: var(--accent);
  border-radius: 6px;
}

.confirmation-action.is-secondary {
  color: var(--text);
  background: var(--surface);
  border: 1px solid var(--border-strong);
}

.confirmation-action.is-danger {
  background: var(--danger);
}

@media (max-width: 520px) {
  .confirmation-backdrop {
    align-items: end;
    padding: 0 0 var(--mobile-nav-height);
  }

  .confirmation-dialog {
    width: 100%;
    padding: 20px;
    border-radius: 8px 8px 0 0;
  }
}
</style>
