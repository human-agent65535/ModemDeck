<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from '@lucide/vue'
import { callState, isLiveCallSession } from '../state/call'
import {
  applicationUpdateState,
  refreshApplication
} from '../state/staleAssetRecovery'

const { t } = useI18n()
const activeCallPresent = computed(() =>
  callState.sessions.some(
    session => isLiveCallSession(session) && session.control_state === 'owned'
  )
)
const message = computed(() =>
  t(
    activeCallPresent.value
      ? 'shell.applicationUpdateReadyDuringCall'
      : 'shell.applicationUpdateReady',
    { version: applicationUpdateState.serverVersion }
  )
)
</script>

<template>
  <Teleport to="body">
    <Transition name="application-update-bar">
      <aside
        v-if="applicationUpdateState.available"
        class="application-update-bar"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        <RefreshCw :size="19" aria-hidden="true" />
        <p>{{ message }}</p>
        <button type="button" @click="refreshApplication()">
          {{ t('shell.refreshApplication') }}
        </button>
      </aside>
    </Transition>
  </Teleport>
</template>

<style scoped>
.application-update-bar {
  position: fixed;
  z-index: var(--layer-feedback);
  bottom: var(--space-5);
  left: calc((100vw + var(--rail-width)) / 2);
  display: grid;
  width: min(620px, calc(100vw - var(--rail-width) - 32px));
  min-height: 54px;
  align-items: center;
  gap: var(--space-3);
  padding: 8px 8px 8px 16px;
  color: var(--text);
  background: rgb(255 255 255 / 98%);
  border: 1px solid var(--accent-border);
  border-radius: var(--radius-surface);
  box-shadow: var(--shadow-lg);
  transform: translateX(-50%);
  grid-template-columns: auto minmax(0, 1fr) auto;
}

.application-update-bar > svg {
  color: var(--accent-strong);
}

.application-update-bar p {
  min-width: 0;
  overflow: hidden;
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.45;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.application-update-bar button {
  min-height: 38px;
  padding: 0 14px;
  color: var(--on-accent);
  font-size: 13px;
  font-weight: 700;
  background: var(--accent);
  border-radius: var(--radius-control);
}

.application-update-bar button:hover {
  background: var(--accent-strong);
}

.application-update-bar-enter-active,
.application-update-bar-leave-active {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-slow) var(--ease-emphasized);
}

.application-update-bar-enter-from,
.application-update-bar-leave-to {
  opacity: 0;
  transform: translate(-50%, var(--space-2));
}

@media (min-width: 1480px) {
  .application-update-bar {
    left: calc((100vw + var(--rail-width) - var(--dialer-width)) / 2);
    width: min(
      620px,
      calc(100vw - var(--rail-width) - var(--dialer-width) - 32px)
    );
  }
}

@media (max-width: 860px) {
  .application-update-bar {
    top: calc(var(--header-height) + var(--space-2));
    bottom: auto;
    left: 50%;
    width: calc(100vw - 24px);
    padding-left: 12px;
  }

  .application-update-bar button {
    padding-inline: 10px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .application-update-bar-enter-active,
  .application-update-bar-leave-active {
    transition: none;
  }
}
</style>
