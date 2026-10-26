<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { callMediaState } from '../state/callMedia'
import { audioState } from '../state/audio'
import { callTestState } from '../state/callTestRuntime'

const { t } = useI18n()
const audio = computed(() => callTestState.session?.test_audio)
const hint = computed(() => {
  if (callMediaState.muted) return t('callTest.muted')
  if (['playback', 'playback_prompt', 'pause'].includes(audio.value?.phase || '')) {
    if (!audio.value?.received_packets || !audio.value.captured_frames) return t('callTest.noCapture')
    if (audio.value.captured_peak_dbfs <= -80) return t('callTest.noSound')
    if (audio.value.captured_peak_dbfs < -40) return t('callTest.quiet')
    return t('callTest.checkPlayback')
  }
  return t('callTest.speakAfterTone')
})
</script>

<template>
  <div class="call-test-feedback">
    <label for="call-test-microphone">{{ t('audio.microphone') }}</label>
    <meter id="call-test-microphone" min="0" max="1" :value="callMediaState.muted ? 0 : callMediaState.microphoneLevel" />
    <p>{{ hint }}</p>
    <span v-if="audio && ['speak', 'playback'].includes(audio.phase)">
      {{ t('callTest.remaining', { seconds: Math.ceil(audio.remaining_ms / 1000) }) }}
    </span>
    <dl v-if="audio">
      <div><dt>{{ t('callTest.serverReceived') }}</dt><dd>{{ audio.received_packets }}</dd></div>
      <div><dt>{{ t('callTest.serverSent') }}</dt><dd>{{ audio.sent_packets }}</dd></div>
    </dl>
    <p v-if="audioState.inputRoutingError || audioState.outputRoutingError" class="call-test-feedback__error" role="alert">
      {{ audioState.inputRoutingError || audioState.outputRoutingError }}
    </p>
  </div>
</template>

<style scoped>
.call-test-feedback {
  display: grid;
  width: 100%;
  gap: 8px;
  margin: 16px 0;
  text-align: center;
}
.call-test-feedback label,
.call-test-feedback span {
  font-size: var(--text-xs);
  color: var(--muted);
}
.call-test-feedback meter {
  width: 100%;
  height: 14px;
  accent-color: var(--accent);
}
.call-test-feedback p {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.6;
}
.call-test-feedback dl {
  display: grid;
  gap: 4px;
  margin: 4px 0 0;
  font-size: var(--text-2xs);
}
.call-test-feedback dl > div {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.call-test-feedback dt {
  color: var(--muted);
  text-align: left;
}
.call-test-feedback dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}
.call-test-feedback p.call-test-feedback__error {
  color: var(--danger);
}
</style>
