<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Circle,
  Delete,
  LoaderCircle,
  Minus,
  Phone,
  X
} from '@lucide/vue'
import {
  callState,
  dial,
  lineHasActiveCall,
  occupiedLineIDs as collectOccupiedLineIDs,
  showActiveCallForLine
} from '../state/call'
import {
  closeDialer,
  minimizeCallSurface,
  openDialer,
  showCallSurface,
  uiState
} from '../state/ui'
import {
  dialerRecordingState,
  rememberCallRecordingPreference,
  resetDialerRecording,
  setDialerRecording
} from '../state/recording'
import {
  bootstrapResource,
  capabilityReason,
  contactsResource,
  lineCanPlaceVoiceCall,
  lineKey,
  loadContacts,
  resolveLine
} from '../state/workspace'
import { playDTMFTone } from '../state/dtmfAudio'
import { normalizeDialTarget } from '../utils/dialTarget'
import { phoneDestination } from '../utils/format'
import { phoneKeypad } from '../utils/phoneKeypad'
import BaseAvatar from './BaseAvatar.vue'
import ContactSuggestInput from './ContactSuggestInput.vue'
import CallSurface from './CallSurface.vue'
import LineSelector from './LineSelector.vue'

const { t } = useI18n()
const props = withDefaults(
  defineProps<{
    permanent?: boolean
    nonModal?: boolean
  }>(),
  {
    permanent: false,
    nonModal: false
  }
)

const number = ref('')
const contactLabel = ref('')
const selectedLineId = ref('')
const selectedContactPreferredLineID = ref('')
const inputAutofocus = ref(false)
const draftContextLineKey = ref('')
const lineSelectionOverridden = ref(false)
const numberFocused = ref(false)
const numberTouched = ref(false)
const validationAttempted = ref(false)
const panelRef = ref<HTMLElement>()
const validationMessageId = `dialer-validation-${useId()}`
let resettingDraft = false
let zeroHoldTimer: number | undefined
let zeroPointerId: number | undefined
let zeroLongPressTriggered = false
let callReturnFocus: HTMLElement | null = null
let dialerReturnFocus: HTMLElement | null = null

const zeroLongPressDelay = 500

const lines = computed(() => bootstrapResource.data?.lines || [])
const voiceCallingAvailable = computed(
  () =>
    bootstrapResource.data?.capabilities.dial === true &&
    bootstrapResource.data?.capabilities.webrtc_audio === true
)
const dialLines = computed(() =>
  voiceCallingAvailable.value
    ? lines.value.filter(lineCanPlaceVoiceCall)
    : []
)
const occupiedLineIDs = computed(() =>
  collectOccupiedLineIDs()
)
const occupiedLineCount = computed(() => occupiedLineIDs.value.size)
const availableDialLines = computed(() =>
  dialLines.value.filter(line => !occupiedLineIDs.value.has(lineKey(line)))
)
const unavailableDialLineIDs = computed(() =>
  lines.value
    .filter(
      line =>
        !voiceCallingAvailable.value || !lineCanPlaceVoiceCall(line)
    )
    .map(lineKey)
)
const activeCallPresent = computed(() => Boolean(callState.session))
const showingCall = computed(
  () => activeCallPresent.value && !uiState.callMinimized
)
const showLineSwitcher = computed(
  () =>
    !showingCall.value || callState.session?.control_state === 'occupied'
)
const lineSwitcherID = computed({
  get: () =>
    (showingCall.value || callState.owned)
      ? callState.session?.line_id || selectedLineId.value
      : selectedLineId.value,
  set: (lineID: string) => {
    if (callState.owned) return
    if (lineHasActiveCall(lineID)) {
      showActiveCallForLine(lineID)
      return
    }
    selectedLineId.value = lineID
    lineSelectionOverridden.value = true
    if (showingCall.value) openDialer('', '', lineID)
  }
})
const callSurfaceVisible = computed(() => showingCall.value)
const defaultLineID = computed(
  () => bootstrapResource.data?.line_settings.default_line_id || ''
)
const selectedLine = computed(() =>
  dialLines.value.find(line => lineKey(line) === selectedLineId.value)
)
const selectedLineBusy = computed(() =>
  selectedLine.value
    ? occupiedLineIDs.value.has(lineKey(selectedLine.value))
    : false
)
const dialUnavailable = computed(() => capabilityReason('dial'))
const activeCallUnavailable = computed(() => {
  return callState.owned ? t('dialer.activeCall') : ''
})
const dialTarget = computed(() => normalizeDialTarget(number.value))
const validationError = computed(() => {
  switch (dialTarget.value.error) {
    case 'too_long':
      return t('dialer.numberTooLong')
    case 'invalid_character':
      return t('dialer.useDialableCharacters')
    case 'invalid_length':
      return t('dialer.enterDialableNumber')
    default:
      return ''
  }
})
const validationVisible = computed(
  () =>
    Boolean(validationError.value) &&
    (validationAttempted.value || (numberTouched.value && !numberFocused.value))
)
const callUnavailableReason = computed(
  () =>
    dialUnavailable.value ||
    activeCallUnavailable.value ||
    (dialLines.value.length === 0 ? t('dialer.noLines') : '') ||
    (!selectedLineId.value ? t('dialer.selectLine') : '') ||
    (selectedLineBusy.value ? t('calls.lineInUse') : '') ||
    (!lineCanPlaceVoiceCall(selectedLine.value)
      ? t('dialer.selectedLineUnsupported')
      : '')
)
const disabledReason = computed(
  () =>
    callUnavailableReason.value ||
    validationError.value ||
    (!number.value.trim() ? t('dialer.enterNumber') : '')
)
const callButtonDisabled = computed(
  () =>
    callState.busy ||
    Boolean(callUnavailableReason.value) ||
    !number.value.trim()
)
function focusNumber(): void {
  inputAutofocus.value = false
  void nextTick(() => {
    inputAutofocus.value = true
  })
}

function syncResolvedLine(force = false): void {
  const selectedStillExists = dialLines.value.some(
    line => lineKey(line) === selectedLineId.value
  )
  if (!selectedStillExists) lineSelectionOverridden.value = false
  if (
    !force &&
    selectedStillExists &&
    (lineSelectionOverridden.value || Boolean(number.value.trim()))
  ) {
    return
  }

  const resolved = resolveLine('dial', {
    contextKey: draftContextLineKey.value,
    preferredLineID: selectedContactPreferredLineID.value,
    number: number.value
  })
  const supportedResolved =
    resolved &&
    lineCanPlaceVoiceCall(resolved) &&
    !occupiedLineIDs.value.has(lineKey(resolved))
      ? resolved
      : availableDialLines.value[0] || dialLines.value[0]
  selectedLineId.value = supportedResolved ? lineKey(supportedResolved) : ''
}

function beginDraft(target = '', label = '', focus = false, contextLineKey = ''): void {
  resettingDraft = true
  number.value = target
  contactLabel.value = label
  selectedContactPreferredLineID.value = ''
  draftContextLineKey.value = contextLineKey
  lineSelectionOverridden.value = false
  numberTouched.value = false
  validationAttempted.value = false
  syncResolvedLine(true)
  resettingDraft = false
  void resetDialerRecording()
  if (focus) focusNumber()
}

function applyDialRequest(focus: boolean): void {
  const dialImmediately = uiState.dialImmediately
  uiState.dialImmediately = false
  beginDraft(uiState.dialTarget, uiState.dialLabel, focus, uiState.dialLineKey)
  if (dialImmediately) void nextTick(placeCall)
}

watch(
  () => uiState.dialRequestRevision,
  revision => {
    if (revision > 0) applyDialRequest(true)
    void loadContacts()
  }
)

watch(
  () => uiState.dialerOpen,
  (open, previous) => {
    if (open) {
      if (
        !props.permanent &&
        !props.nonModal &&
        !previous &&
        document.activeElement instanceof HTMLElement
      ) {
        dialerReturnFocus = document.activeElement
      }
      focusNumber()
      void loadContacts()
      return
    }
    if (
      previous &&
      !activeCallPresent.value &&
      !props.permanent &&
      !props.nonModal
    ) {
      restoreDialogFocus()
    }
  }
)

watch(
  [callSurfaceVisible, () => props.permanent, () => props.nonModal],
  async (
    [showing, permanent, nonModal],
    [previous, wasPermanent, wasNonModal]
  ) => {
    if (
      showing &&
      !permanent &&
      !nonModal &&
      (!previous || wasPermanent || wasNonModal)
    ) {
      callReturnFocus =
        dialerReturnFocus?.isConnected
          ? dialerReturnFocus
          : document.activeElement instanceof HTMLElement
            ? document.activeElement
            : null
      await nextTick()
      panelRef.value?.focus()
      return
    }
    if (!showing && previous && !nonModal) restoreDialogFocus()
    if (permanent || nonModal) {
      callReturnFocus = null
      dialerReturnFocus = null
    }
  }
)

watch(
  [dialLines, occupiedLineIDs, defaultLineID, () => contactsResource.data, number],
  () => syncResolvedLine(),
  { immediate: true }
)

watch(number, (value, previous) => {
  numberTouched.value = false
  validationAttempted.value = false
  if (resettingDraft || !previous.trim() || value.trim()) return
  contactLabel.value = ''
  void resetDialerRecording()
})

function appendDigit(digit: string): void {
  number.value += digit
  contactLabel.value = ''
  selectedContactPreferredLineID.value = ''
  void playDTMFTone(digit)
}

function clearZeroHold(): void {
  if (zeroHoldTimer !== undefined) window.clearTimeout(zeroHoldTimer)
  zeroHoldTimer = undefined
}

function startZeroHold(event: PointerEvent): void {
  if (event.pointerType === 'mouse' && event.button !== 0) return

  clearZeroHold()
  zeroPointerId = event.pointerId
  zeroLongPressTriggered = false
  ;(event.currentTarget as HTMLButtonElement).setPointerCapture(event.pointerId)
  zeroHoldTimer = window.setTimeout(() => {
    if (zeroPointerId !== event.pointerId) return
    zeroLongPressTriggered = true
    appendDigit('+')
  }, zeroLongPressDelay)
}

function finishZeroHold(event: PointerEvent): void {
  if (zeroPointerId !== event.pointerId) return

  clearZeroHold()
  if (!zeroLongPressTriggered) appendDigit('0')
  const button = event.currentTarget as HTMLButtonElement
  if (button.hasPointerCapture(event.pointerId)) button.releasePointerCapture(event.pointerId)
  zeroPointerId = undefined
}

function cancelZeroHold(event: PointerEvent): void {
  if (zeroPointerId !== event.pointerId) return
  clearZeroHold()
  zeroPointerId = undefined
  zeroLongPressTriggered = false
}

function handleKeyClick(digit: string, event: MouseEvent): void {
  if (digit === '0') {
    if (event.detail === 0) appendDigit('0')
    return
  }
  appendDigit(digit)
}

function preventZeroContextMenu(digit: string, event: MouseEvent): void {
  if (digit === '0') event.preventDefault()
}

function removeDigit(): void {
  number.value = Array.from(number.value).slice(0, -1).join('')
  contactLabel.value = ''
  selectedContactPreferredLineID.value = ''
}

function editNumber(value: string): void {
  number.value = value
  contactLabel.value = ''
  selectedContactPreferredLineID.value = ''
}

function chooseContact(suggestion: {
  contact: { display_name: string; preferred_line_id?: string }
  phone: { number: string; normalized_number?: string }
}): void {
  number.value = phoneDestination(suggestion.phone)
  contactLabel.value = suggestion.contact.display_name
  selectedContactPreferredLineID.value = suggestion.contact.preferred_line_id || ''
  numberTouched.value = false
  validationAttempted.value = false
  syncResolvedLine(true)
}

function focusNumberInput(): void {
  numberFocused.value = true
}

function blurNumberInput(): void {
  numberFocused.value = false
  numberTouched.value = true
}

async function placeCall(): Promise<void> {
  validationAttempted.value = true
  if (
    callState.busy ||
    callUnavailableReason.value ||
    !number.value.trim() ||
    dialTarget.value.error
  ) {
    return
  }
  const recordingReady = dialerRecordingState.status === 'ready'
  const recordingEnabled = dialerRecordingState.enabled
  const placed = await dial(
    dialTarget.value.original,
    selectedLineId.value,
    recordingReady ? recordingEnabled : undefined
  )
  if (!placed) return
  if (recordingReady && callState.session) {
    rememberCallRecordingPreference(callState.session.id, recordingEnabled)
  }
  beginDraft()
}

function changeRecording(): void {
  setDialerRecording(!dialerRecordingState.enabled)
}

function restoreDialogFocus(): void {
  const target = callReturnFocus?.isConnected
    ? callReturnFocus
    : dialerReturnFocus?.isConnected
      ? dialerReturnFocus
      : null
  callReturnFocus = null
  dialerReturnFocus = null
  target?.focus()
}

function trapCallFocus(event: KeyboardEvent): void {
  if (
    props.permanent ||
    props.nonModal ||
    !callSurfaceVisible.value ||
    event.key !== 'Tab' ||
    !panelRef.value
  ) {
    return
  }

  const focusable = Array.from(
    panelRef.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])'
    )
  ).filter(element => !element.hidden && element.getClientRects().length > 0)
  if (focusable.length === 0) {
    event.preventDefault()
    panelRef.value.focus()
    return
  }

  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (!first || !last) return
  const current = document.activeElement
  if (event.shiftKey && (current === first || current === panelRef.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (current === last || current === panelRef.value)) {
    event.preventDefault()
    first.focus()
  }
}

onMounted(() => {
  applyDialRequest(false)
  void loadContacts()
})

onBeforeUnmount(() => {
  clearZeroHold()
  restoreDialogFocus()
})
</script>

<template>
  <Teleport to="body" :disabled="permanent">
    <Transition name="dialer-surface">
      <div
        v-if="permanent || uiState.dialerOpen || callSurfaceVisible"
        :class="[
          permanent ? 'dialer-host dialer-host--permanent' : 'drawer-backdrop',
          {
            'drawer-backdrop--call': !permanent && callSurfaceVisible,
            'drawer-backdrop--nonmodal': !permanent && nonModal
          }
        ]"
        @mousedown.self="
          !permanent && !nonModal && !showingCall && closeDialer()
        "
      >
        <aside
          ref="panelRef"
          class="dialer-panel"
          :class="{
            'dialer-panel--permanent': permanent,
            'dialer-panel--call': showingCall
          }"
          :role="permanent ? undefined : nonModal ? 'complementary' : 'dialog'"
          :aria-modal="!permanent && !nonModal ? true : undefined"
          :aria-label="showingCall ? t('shell.calls') : t('dialer.title')"
          :tabindex="!permanent && !nonModal && callSurfaceVisible ? -1 : undefined"
          @keydown="trapCallFocus"
          @keydown.esc="
            !permanent && (showingCall ? minimizeCallSurface() : closeDialer())
          "
        >
          <header class="tool-header dialer-toolbar">
            <span class="dialer-toolbar__title">
              <h2>{{ showingCall ? t('shell.calls') : t('dialer.title') }}</h2>
              <span class="dialer-status-slot">
                <button
                  v-if="!showingCall && activeCallPresent"
                  class="dialer-active-calls dialer-active-calls--busy"
                  type="button"
                  :title="t('shell.returnToCall')"
                  :aria-label="`${t('shell.returnToCall')} · ${occupiedLineCount}`"
                  @click="showCallSurface"
                >
                  <Phone :size="15" />
                  <span>{{ occupiedLineCount }}</span>
                </button>
                <span
                  v-else-if="occupiedLineCount > 0"
                  class="dialer-active-calls dialer-active-calls--busy dialer-active-calls--static"
                  :aria-label="`${occupiedLineCount} ${t('calls.lineInUse')}`"
                >
                  <Phone :size="15" />
                  <span>{{ occupiedLineCount }}</span>
                </span>
              </span>
            </span>
            <span class="dialer-header-actions">
              <button
                v-if="!permanent"
                class="icon-button"
                type="button"
                :title="showingCall ? t('calls.minimize') : t('common.close')"
                :aria-label="showingCall ? t('calls.minimizeCall') : t('dialer.close')"
                @click="showingCall ? minimizeCallSurface() : closeDialer()"
              >
                <Minus v-if="showingCall" :size="20" />
                <X v-else :size="19" />
              </button>
            </span>
          </header>

          <div v-if="showLineSwitcher" class="dialer-line-switcher">
            <LineSelector
              v-model="lineSwitcherID"
              :lines="lines"
              :placeholder="dialLines.length > 0 ? t('dialer.selectLine') : t('dialer.noLines')"
              :default-line-id="defaultLineID"
              :disabled-values="unavailableDialLineIDs"
              :disabled-value-label="t('dialer.voiceUnavailable')"
              :status-values="[...occupiedLineIDs]"
              :status-value-label="t('calls.lineInUse')"
              :disabled="callState.owned"
              :label="t('dialer.line')"
              :unavailable-label="t('dialer.voiceUnavailable')"
            />
          </div>

          <CallSurface v-if="showingCall" />

          <div v-else class="dialer-panel__body">
            <div class="dialer-panel__scroll">
              <div class="dialer-number-entry">
                <div
                  class="dialer-number-control"
                  :class="{
                    'has-inline-validation': validationVisible,
                    'is-invalid': validationVisible
                  }"
                >
                  <ContactSuggestInput
                    :model-value="number"
                    :contacts="contactsResource.data"
                    :autofocus="inputAutofocus"
                    :invalid="validationVisible"
                    :described-by="validationVisible ? validationMessageId : ''"
                    @update:model-value="editNumber"
                    @select="chooseContact"
                    @submit="placeCall"
                    @focus="focusNumberInput"
                    @blur="blurNumberInput"
                  />
                  <span v-if="number" class="dialer-number-trailing">
                    <span
                      v-if="validationVisible"
                      :id="validationMessageId"
                      class="dialer-inline-validation"
                      role="status"
                      :title="validationError"
                    >
                      <span aria-hidden="true">{{ t('dialer.invalidShort') }}</span>
                      <span class="sr-only">{{ validationError }}</span>
                    </span>
                    <button
                      class="icon-button dialer-backspace-button"
                      type="button"
                      :title="t('dialer.backspace')"
                      :aria-label="t('dialer.backspace')"
                      @mousedown.prevent
                      @click="removeDigit"
                    >
                      <Delete :size="18" />
                    </button>
                  </span>
                </div>
                <div v-if="contactLabel" class="dialer-contact-match">
                  <BaseAvatar :name="contactLabel" size="small" />
                  <span class="dialer-contact-match__copy">
                    <small>{{ t('dialer.matchedContact') }}</small>
                    <strong>{{ contactLabel }}</strong>
                  </span>
                </div>
              </div>

              <p v-if="dialerRecordingState.error" class="dialer-recording__error" role="alert">
                {{ dialerRecordingState.error }}
              </p>

              <p v-if="dialUnavailable || dialLines.length === 0" class="unavailable-note">
                {{ dialUnavailable || t('dialer.noLines') }}
              </p>
              <p v-if="callState.error" class="field-error">{{ callState.error }}</p>

              <div class="dialer-keypad-stage">
                <div class="keypad" role="group" :aria-label="t('dialer.keypad')">
                  <button
                    v-for="key in phoneKeypad"
                    :key="key.digit"
                    class="keypad__key"
                    :class="{ 'keypad__key--zero': key.digit === '0' }"
                    type="button"
                    :aria-label="key.digit"
                    @click="handleKeyClick(key.digit, $event)"
                    @contextmenu="preventZeroContextMenu(key.digit, $event)"
                    @pointerdown="key.digit === '0' && startZeroHold($event)"
                    @pointerup="key.digit === '0' && finishZeroHold($event)"
                    @pointercancel="key.digit === '0' && cancelZeroHold($event)"
                  >
                    <strong>{{ key.digit }}</strong>
                    <small v-if="key.letters">{{ key.letters }}</small>
                  </button>
                </div>
              </div>
            </div>

            <div class="dialer-primary-actions keypad-action-grid">
              <button
                class="dialer-recording-action keypad-action-item"
                :class="{ 'is-active': dialerRecordingState.enabled }"
                type="button"
                :disabled="dialerRecordingState.status !== 'ready'"
                :title="dialerRecordingState.error || t('dialer.recording')"
                :aria-label="t('dialer.recording')"
                :aria-pressed="dialerRecordingState.enabled"
                @click="changeRecording"
              >
                <span class="dialer-recording-action__icon" aria-hidden="true">
                  <Circle :size="20" fill="currentColor" aria-hidden="true" />
                </span>
                <small>{{ t('calls.record') }}</small>
              </button>
              <span class="dialer-primary-action keypad-action-item">
                <button
                  class="call-button"
                  type="button"
                  :disabled="callButtonDisabled"
                  :title="disabledReason || t('calls.dial')"
                  :aria-label="t('calls.dial')"
                  @click="placeCall"
                >
                  <LoaderCircle v-if="callState.pendingAction === 'dial'" class="spin" :size="22" />
                  <Phone v-else :size="22" />
                </button>
                <small>{{ t('calls.dial') }}</small>
              </span>
            </div>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.dialer-surface-enter-active,
.dialer-surface-leave-active {
  transition: opacity var(--motion-base) var(--ease-standard);
}

.dialer-surface-enter-active .dialer-panel,
.dialer-surface-leave-active .dialer-panel {
  transition:
    opacity var(--motion-base) var(--ease-standard),
    transform var(--motion-slow) var(--ease-emphasized);
}

.dialer-surface-enter-from,
.dialer-surface-leave-to,
.dialer-surface-enter-from .dialer-panel,
.dialer-surface-leave-to .dialer-panel {
  opacity: 0;
}

.dialer-surface-enter-from .dialer-panel,
.dialer-surface-leave-to .dialer-panel {
  transform: translateY(var(--space-2)) scale(0.99);
}

.dialer-host--permanent {
  min-width: 0;
  min-height: 0;
  background: var(--surface);
  border-left: 1px solid var(--border);
}

.dialer-panel--permanent {
  display: flex;
  width: 100%;
  height: 100dvh;
  max-height: none;
  flex-direction: column;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

.dialer-panel--call {
  display: flex;
  min-height: 0;
  flex-direction: column;
}

.dialer-panel {
  display: flex;
  min-height: 0;
  flex-direction: column;
}

.drawer-backdrop--call {
  z-index: 105;
  align-items: center;
  justify-content: center;
}

.drawer-backdrop--call .dialer-panel--call {
  width: min(420px, calc(100vw - 40px));
  height: min(720px, calc(100dvh - 40px));
  max-height: none;
}

.dialer-header-actions {
  display: flex;
  margin-left: auto;
  align-items: center;
  gap: 3px;
}

.dialer-status-slot {
  display: flex;
  width: 52px;
  align-items: center;
  justify-content: flex-start;
}

.dialer-toolbar__title {
  display: grid;
  width: 104px;
  min-width: 0;
  grid-template-columns: minmax(0, 52px) 52px;
  align-items: center;
}

.dialer-toolbar__title h2 {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dialer-active-calls {
  display: inline-flex;
  min-width: 28px;
  height: 28px;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 7px;
  color: var(--accent-strong);
  font: inherit;
  font-size: 12px;
  font-weight: 800;
  background: var(--accent-soft);
  border: 1px solid transparent;
  border-radius: 14px;
  cursor: pointer;
}

.dialer-active-calls--static {
  cursor: default;
}

.dialer-active-calls--busy {
  color: var(--danger);
  background: var(--danger-soft);
}

.dialer-toolbar {
  display: flex;
}

.dialer-line-switcher {
  flex: 0 0 auto;
  margin: 16px 20px 0;
  padding-bottom: 13px;
  border-bottom: 1px solid var(--border);
}

.dialer-panel__body {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  padding: 0;
  overflow: hidden;
}

.dialer-panel__scroll {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  padding: 0 20px 14px;
  overflow-y: auto;
}

.dialer-number-entry {
  position: relative;
  padding: 14px 0 8px;
}

.dialer-number-control {
  position: relative;
}

.dialer-number-entry :deep(.suggest-input__field) {
  height: 68px;
  justify-content: center;
  padding: 0 50px;
  color: var(--muted);
  background: transparent;
  border: 0;
  border-bottom: 2px solid var(--border);
  border-radius: 0;
  box-shadow: none;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard);
}

.dialer-number-entry :deep(.suggest-input__field:focus-within) {
  color: var(--accent);
  background: rgb(17 120 100 / 4%);
}

.dialer-number-entry :deep(.suggest-input__field > svg) {
  display: none;
}

.dialer-number-entry :deep(.suggest-input__field input) {
  padding: 0;
  color: var(--text);
  font-size: clamp(22px, 2vw, 28px);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.035em;
  text-align: center;
}

.dialer-number-entry :deep(.suggest-input__field input:focus-visible) {
  outline: none;
  box-shadow: none;
}

.dialer-number-entry :deep(.suggest-input__field input::placeholder) {
  color: var(--muted);
  font-size: 14px;
  font-weight: 500;
  letter-spacing: 0;
}

.dialer-number-control.has-inline-validation :deep(.suggest-input__field input) {
  padding: 0;
}

.dialer-number-control.is-invalid :deep(.suggest-input__field),
.dialer-number-control.is-invalid :deep(.suggest-input__field:focus-within) {
  border-color: var(--danger);
  box-shadow: 0 0 0 3px rgb(196 53 74 / 11%);
}

.dialer-number-entry :deep(.suggest-menu) {
  max-height: min(220px, 28dvh);
}

.dialer-number-trailing {
  position: absolute;
  z-index: 2;
  top: 50%;
  right: 7px;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  transform: translateY(-50%);
}

.dialer-inline-validation {
  max-width: 52px;
  overflow: hidden;
  color: var(--danger);
  font-size: 11px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dialer-number-trailing .dialer-backspace-button {
  width: 38px;
  height: 38px;
  color: var(--muted);
}

.dialer-number-trailing .dialer-backspace-button:hover {
  color: var(--text);
}

.dialer-contact-match {
  display: flex;
  width: fit-content;
  max-width: calc(100% - 16px);
  align-items: center;
  gap: 8px;
  margin: 9px auto 0;
  padding: 5px 11px 5px 6px;
  background: var(--accent-soft);
  border: 1px solid rgb(17 120 100 / 12%);
  border-radius: 999px;
}

.dialer-contact-match__copy {
  display: grid;
  min-width: 0;
  line-height: 1.15;
}

.dialer-contact-match__copy small {
  color: var(--muted);
  font-size: 10px;
}

.dialer-contact-match__copy strong {
  overflow: hidden;
  color: var(--text);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.keypad__key {
  touch-action: manipulation;
  user-select: none;
}

.dialer-recording__error {
  margin-top: 7px;
  color: var(--danger);
  font-size: 12px;
}

.dialer-keypad-stage {
  display: flex;
  min-height: 311px;
  flex: 1 0 311px;
  align-items: flex-end;
  justify-content: center;
  padding: 24px 0 12px;
}

.dialer-keypad-stage > .keypad {
  margin: 0;
}

.dialer-primary-actions {
  min-width: 0;
}

.dialer-recording-action {
  grid-column: 1;
  color: var(--muted);
  font-size: 11px;
  background: transparent;
}

.dialer-recording-action__icon {
  display: inline-flex;
  width: var(--keypad-action-secondary-size);
  height: var(--keypad-action-secondary-size);
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  color: var(--text);
  background: var(--surface-hover);
  border: 1px solid var(--border);
  border-radius: 50%;
  transition:
    color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard),
    border-color var(--motion-base) var(--ease-standard);
}

.dialer-recording-action:not(.is-active):hover:not(:disabled)
  .dialer-recording-action__icon {
  background: var(--control-muted);
}

.dialer-recording-action.is-active .dialer-recording-action__icon {
  color: var(--on-accent);
  background: var(--danger);
  border-color: var(--danger);
}

.dialer-recording-action:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.dialer-recording-action > small {
  color: inherit;
  font-size: 11px;
  white-space: nowrap;
}

.dialer-primary-action {
  grid-column: 2;
}

.dialer-primary-action .call-button {
  width: var(--keypad-action-primary-size);
  height: var(--keypad-action-primary-size);
  flex-basis: var(--keypad-action-primary-size);
}

.dialer-primary-action small {
  color: var(--muted);
  font-size: 11px;
}

@media (max-height: 760px) {
  .dialer-panel__scroll {
    padding-block: 0 8px;
  }

  .dialer-line-switcher {
    margin-top: 10px;
    padding-bottom: 8px;
  }

  .dialer-line-switcher :deep(.line-selector) {
    gap: 4px;
  }

  .dialer-line-switcher :deep(.line-selector__control) {
    min-height: 52px;
    padding-block: 4px;
  }

  .dialer-line-switcher :deep(.line-identity__icon) {
    width: 32px;
    height: 32px;
  }

  .dialer-number-entry {
    padding: 8px 0 4px;
  }

  .dialer-number-entry :deep(.suggest-input__field) {
    height: 58px;
  }

  .dialer-keypad-stage {
    min-height: 255px;
    flex-basis: 255px;
    padding: 0 0 4px;
  }

  .dialer-keypad-stage .keypad__key {
    width: 56px;
    height: 56px;
    justify-self: center;
  }

  .dialer-primary-actions {
    --keypad-action-primary-size: 56px;
    --keypad-action-secondary-size: 48px;

    min-height: 96px;
    padding-block: 8px 10px;
  }
}

@media (max-width: 1100px) {
  .drawer-backdrop--call {
    padding: 12px;
  }

  .drawer-backdrop--call .dialer-panel--call {
    width: 100%;
    height: calc(100dvh - 24px);
    max-height: none;
  }
}

@media (max-width: 860px) {
  .drawer-backdrop {
    align-items: flex-end;
    justify-content: flex-end;
    padding: 0 0 var(--mobile-nav-height);
  }

  .drawer-backdrop .dialer-panel {
    width: 100%;
    height: min(720px, calc(100dvh - var(--mobile-nav-height) - 8px));
    max-height: calc(100dvh - var(--mobile-nav-height) - 8px);
    border-right: 0;
    border-bottom: 0;
    border-left: 0;
    border-radius: 8px 8px 0 0;
  }

  .dialer-surface-enter-from .dialer-panel,
  .dialer-surface-leave-to .dialer-panel {
    transform: translateY(var(--space-3));
  }
}

@media (min-width: 861px) and (max-width: 1479px) {
  .drawer-backdrop--nonmodal {
    z-index: 65;
    align-items: flex-end;
    justify-content: flex-end;
    pointer-events: none;
    background: transparent;
  }

  .drawer-backdrop--nonmodal .dialer-panel {
    pointer-events: auto;
  }

  .drawer-backdrop--nonmodal .dialer-panel--call {
    width: min(420px, calc(100vw - 40px));
    height: min(720px, calc(100dvh - 40px));
    max-height: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dialer-surface-enter-active,
  .dialer-surface-leave-active,
  .dialer-surface-enter-active .dialer-panel,
  .dialer-surface-leave-active .dialer-panel {
    transition: none;
  }

  .dialer-surface-enter-from .dialer-panel,
  .dialer-surface-leave-to .dialer-panel {
    transform: none;
  }
}

</style>
