<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MicOff, PhoneIncoming } from '@lucide/vue'
import { callMediaState } from '../state/callMedia'
import { presentedCallSession } from '../state/callTestRuntime'
import { showCallSurface } from '../state/ui'
import { contactForNumber, displayPhoneNumber } from '../state/workspace'
import { formatCallClock } from '../utils/format'

// A minimized call stays visible in the header, like Meet's or Slack's call pill,
// so returning to it is one click instead of reopening the dialer.
const props = defineProps<{ ringing: boolean }>()
const { t } = useI18n()
const now = ref(Date.now())
let timer: number | undefined

const session = computed(() => presentedCallSession.value)
const peer = computed(() => {
  const call = session.value
  if (!call) return ''
  return (
    call.display_name ||
    contactForNumber(call.remote_number)?.display_name ||
    displayPhoneNumber(call.remote_number, call.line_id) ||
    t('calls.unknownNumber')
  )
})
const clock = computed(() => {
  const start = session.value?.active_at
  if (!start) return ''
  return formatCallClock(Math.max(0, Math.floor((now.value - Date.parse(start)) / 1000)))
})
const status = computed(() => {
  if (props.ringing) return t('calls.incoming')
  return clock.value || t('calls.connectingCall')
})
const label = computed(() =>
  [props.ringing ? t('shell.openIncomingCall') : t('shell.returnToCall'), peer.value, status.value]
    .filter(Boolean)
    .join(' · ')
)

onMounted(() => {
  timer = window.setInterval(() => {
    now.value = Date.now()
  }, 1000)
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <button
    class="active-call-pill"
    :class="{ 'is-ringing': ringing }"
    type="button"
    :title="label"
    :aria-label="label"
    @click="showCallSurface()"
  >
    <PhoneIncoming v-if="ringing" :size="16" aria-hidden="true" />
    <span v-else class="active-call-pill__dot" aria-hidden="true" />
    <span class="active-call-pill__peer">{{ peer }}</span>
    <span class="active-call-pill__status">{{ status }}</span>
    <MicOff
      v-if="!ringing && callMediaState.muted"
      class="active-call-pill__muted"
      :size="15"
      aria-hidden="true"
    />
  </button>
</template>

<style scoped>
.active-call-pill {
  /* Display comes from the shell header, which shows the pill only where the dialer is not docked. */
  max-width: 260px;
  height: 36px;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-3);
  color: var(--success);
  font-size: var(--text-sm);
  font-weight: 500;
  background: var(--success-soft);
  border: 1px solid var(--success-border-strong);
  border-radius: var(--radius-pill);
  transition: background var(--motion-fast) var(--ease-standard);
}

.active-call-pill:hover {
  background: color-mix(in srgb, var(--success-soft) 70%, var(--success-border));
}

.active-call-pill.is-ringing {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: var(--accent-border-strong);
}

.active-call-pill__dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  background: currentColor;
  border-radius: 50%;
  animation: active-call-pulse 1.6s var(--ease-standard) infinite;
}

.active-call-pill__peer {
  min-width: 0;
  overflow: hidden;
  color: var(--text);
  font-weight: var(--weight-strong);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.active-call-pill__status {
  flex: 0 0 auto;
  font-variant-numeric: tabular-nums;
}

.active-call-pill__muted {
  flex: 0 0 auto;
  color: var(--muted);
}

@keyframes active-call-pulse {
  50% {
    opacity: 0.35;
  }
}

@media (prefers-reduced-motion: reduce) {
  .active-call-pill__dot {
    animation: none;
  }
}
</style>
