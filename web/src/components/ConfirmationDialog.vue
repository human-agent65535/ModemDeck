<script setup lang="ts">
import { AlertTriangle } from '@lucide/vue'
import { useId } from 'vue'
import { answerConfirmation, confirmationState } from '../state/confirmation'
import OverlayDialog from './OverlayDialog.vue'

const titleID = `confirmation-title-${useId()}`
const messageID = `confirmation-message-${useId()}`
</script>

<template>
  <OverlayDialog
    :open="Boolean(confirmationState.request)"
    size="small"
    layer="critical"
    :labelledby="titleID"
    :describedby="confirmationState.request?.message ? messageID : undefined"
    initial-focus="[data-overlay-initial]"
    @close="answerConfirmation(false)"
  >
    <div v-if="confirmationState.request" class="confirmation-dialog">
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
          class="confirmation-action is-secondary"
          type="button"
          data-overlay-initial
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
    </div>
  </OverlayDialog>
</template>

<style scoped>
.confirmation-dialog {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 14px;
  padding: 22px;
}

.confirmation-dialog__icon {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  color: var(--warning);
  background: var(--warning-soft);
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
  color: var(--on-accent);
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

@media (max-width: 560px) {
  .confirmation-dialog {
    padding: 20px;
  }
}
</style>
