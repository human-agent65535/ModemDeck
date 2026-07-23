<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Circle,
  Delete,
  LoaderCircle,
  Phone,
  X
} from '@lucide/vue'
import { callState, dial } from '../state/call'
import { closeDialer, uiState } from '../state/ui'
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
  lineKey,
  lineSupports,
  loadContacts,
  resolveLine
} from '../state/workspace'
import ContactSuggestInput from './ContactSuggestInput.vue'
import LineSelector from './LineSelector.vue'

withDefaults(
  defineProps<{
    permanent?: boolean
  }>(),
  {
    permanent: false
  }
)

const number = ref('')
const contactLabel = ref('')
const selectedLineId = ref('')
const inputAutofocus = ref(false)
const draftContextLineKey = ref('')
const lineSelectionOverridden = ref(false)
let resettingDraft = false
let zeroHoldTimer: number | undefined
let zeroPointerId: number | undefined
let zeroLongPressTriggered = false

const zeroLongPressDelay = 500

const keypad = [
  { digit: '1', letters: '' },
  { digit: '2', letters: 'ABC' },
  { digit: '3', letters: 'DEF' },
  { digit: '4', letters: 'GHI' },
  { digit: '5', letters: 'JKL' },
  { digit: '6', letters: 'MNO' },
  { digit: '7', letters: 'PQRS' },
  { digit: '8', letters: 'TUV' },
  { digit: '9', letters: 'WXYZ' },
  { digit: '*', letters: '' },
  { digit: '0', letters: '+' },
  { digit: '#', letters: '' }
]

const lines = computed(() => bootstrapResource.data?.lines || [])
const defaultLineDeviceIMEI = computed(
  () => bootstrapResource.data?.line_settings.default_device_imei || ''
)
const selectedLine = computed(() => lines.value.find(line => lineKey(line) === selectedLineId.value))
const dialUnavailable = computed(() => capabilityReason('dial'))
const activeCallUnavailable = computed(() => {
  const phase = callState.session?.phase
  return phase && phase !== 'ended' && phase !== 'failed' ? '已有通话正在进行' : ''
})
const validationError = computed(() => {
  if (!number.value.trim()) return ''
  return /^\+?[\d*#][\d\s().*#-]{2,}$/.test(number.value.trim()) ? '' : '号码格式无效'
})
const disabledReason = computed(
  () =>
    dialUnavailable.value ||
    activeCallUnavailable.value ||
    (lines.value.length === 0 ? '没有可用线路' : '') ||
    (!selectedLineId.value ? '选择线路' : '') ||
    (lineSupports(selectedLine.value, 'dial') === false ? '所选线路不支持拨号' : '') ||
    validationError.value ||
    (!number.value.trim() ? '请输入号码' : '')
)
function focusNumber(): void {
  inputAutofocus.value = false
  void nextTick(() => {
    inputAutofocus.value = true
  })
}

function syncResolvedLine(force = false): void {
  const selectedStillExists = lines.value.some(line => lineKey(line) === selectedLineId.value)
  if (!selectedStillExists) lineSelectionOverridden.value = false
  if (!force && lineSelectionOverridden.value) return

  const resolved = resolveLine('dial', {
    contextKey: draftContextLineKey.value,
    number: number.value
  })
  selectedLineId.value = resolved ? lineKey(resolved) : ''
}

function beginDraft(target = '', label = '', focus = false, contextLineKey = ''): void {
  resettingDraft = true
  number.value = target
  contactLabel.value = label
  draftContextLineKey.value = contextLineKey
  lineSelectionOverridden.value = false
  syncResolvedLine(true)
  resettingDraft = false
  void resetDialerRecording()
  if (focus) focusNumber()
}

watch(
  () => uiState.dialRequestRevision,
  revision => {
    if (revision > 0) {
      beginDraft(uiState.dialTarget, uiState.dialLabel, true, uiState.dialLineKey)
    }
    void loadContacts()
  }
)

watch(
  () => uiState.dialerOpen,
  open => {
    if (!open) return
    focusNumber()
    void loadContacts()
  }
)

watch(
  [lines, defaultLineDeviceIMEI, () => contactsResource.data, number],
  () => syncResolvedLine(),
  { immediate: true }
)

watch(number, (value, previous) => {
  if (resettingDraft || !previous.trim() || value.trim()) return
  contactLabel.value = ''
  void resetDialerRecording()
})

function appendDigit(digit: string): void {
  number.value += digit
  contactLabel.value = ''
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
}

function chooseContact(suggestion: {
  contact: { display_name: string }
  phone: { number: string }
}): void {
  number.value = suggestion.phone.number
  contactLabel.value = suggestion.contact.display_name
  syncResolvedLine()
}

function changeLine(): void {
  lineSelectionOverridden.value = true
}

async function placeCall(): Promise<void> {
  if (disabledReason.value) return
  const recordingReady = dialerRecordingState.status === 'ready'
  const recordingEnabled = dialerRecordingState.enabled
  const placed = await dial(
    number.value.trim(),
    selectedLineId.value,
    recordingReady ? recordingEnabled : undefined
  )
  if (!placed) return
  if (recordingReady && callState.session) {
    rememberCallRecordingPreference(callState.session.id, recordingEnabled)
  }
  beginDraft()
}

function changeRecording(event: Event): void {
  setDialerRecording((event.target as HTMLInputElement).checked)
}

onMounted(() => {
  beginDraft(uiState.dialTarget, uiState.dialLabel, false, uiState.dialLineKey)
  void loadContacts()
})

onBeforeUnmount(clearZeroHold)
</script>

<template>
  <Teleport to="body" :disabled="permanent">
    <Transition name="fade">
      <div
        v-if="permanent || uiState.dialerOpen"
        :class="permanent ? 'dialer-host dialer-host--permanent' : 'drawer-backdrop'"
        @mousedown.self="!permanent && closeDialer()"
      >
        <aside
          class="dialer-panel"
          :class="{ 'dialer-panel--permanent': permanent }"
          :role="permanent ? undefined : 'dialog'"
          :aria-modal="permanent ? undefined : true"
          aria-label="拨号"
          @keydown.esc="!permanent && closeDialer()"
        >
          <header class="tool-header dialer-toolbar">
            <h2>拨号</h2>
            <span class="dialer-header-actions">
              <button
                v-if="!permanent"
                class="icon-button"
                type="button"
                title="关闭"
                aria-label="关闭拨号盘"
                @click="closeDialer"
              >
                <X :size="19" />
              </button>
            </span>
          </header>

          <div class="dialer-panel__body">
            <div v-if="lines.length > 0" class="dialer-line-switcher">
              <LineSelector
                v-model="selectedLineId"
                :lines="lines"
                :default-device-imei="defaultLineDeviceIMEI"
                label="通话线路"
                capability="dial"
                unavailable-label="不支持拨号"
                @change="changeLine"
              />
            </div>

            <div class="dialer-number-entry">
              <ContactSuggestInput
                v-model="number"
                :contacts="contactsResource.data"
                :autofocus="inputAutofocus"
                @select="chooseContact"
              />
              <span v-if="contactLabel" class="dialer-contact-name">{{ contactLabel }}</span>
            </div>

            <label class="dialer-recording">
              <span class="dialer-recording__identity">
                <Circle :size="16" fill="currentColor" aria-hidden="true" />
                <strong>通话录音</strong>
              </span>
              <input
                type="checkbox"
                role="switch"
                :checked="dialerRecordingState.enabled"
                :disabled="dialerRecordingState.status !== 'ready'"
                aria-label="通话录音"
                @change="changeRecording"
              />
            </label>
            <p v-if="dialerRecordingState.error" class="dialer-recording__error" role="alert">
              {{ dialerRecordingState.error }}
            </p>

            <div class="keypad" aria-label="拨号键盘">
              <button
                v-for="key in keypad"
                :key="key.digit"
                class="keypad__key"
                type="button"
                :aria-label="key.digit"
                @click="handleKeyClick(key.digit, $event)"
                @contextmenu="preventZeroContextMenu(key.digit, $event)"
                @pointerdown="key.digit === '0' && startZeroHold($event)"
                @pointerup="key.digit === '0' && finishZeroHold($event)"
                @pointercancel="key.digit === '0' && cancelZeroHold($event)"
              >
                <strong>{{ key.digit }}</strong>
                <small>{{ key.letters }}</small>
              </button>
            </div>

            <div class="dialer-actions">
              <span class="dialer-actions__spacer" />
              <button
                class="call-button"
                type="button"
                :disabled="Boolean(disabledReason) || callState.busy"
                :title="disabledReason || '拨打'"
                aria-label="拨打"
                @click="placeCall"
              >
                <LoaderCircle v-if="callState.pendingAction === 'dial'" class="spin" :size="22" />
                <Phone v-else :size="22" />
              </button>
              <button
                class="icon-button"
                type="button"
                :disabled="!number"
                title="退格"
                aria-label="退格"
                @click="removeDigit"
              >
                <Delete :size="21" />
              </button>
            </div>

            <p v-if="dialUnavailable || lines.length === 0" class="unavailable-note">
              {{ dialUnavailable || '没有可用线路' }}
            </p>
            <p v-else-if="validationError" class="field-error">{{ validationError }}</p>
            <p v-if="callState.error" class="field-error">{{ callState.error }}</p>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
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

.dialer-header-actions {
  display: flex;
  align-items: center;
  gap: 3px;
}

.dialer-toolbar {
  display: flex;
}

.dialer-line-switcher {
  padding-bottom: 13px;
  border-bottom: 1px solid var(--border);
}

.dialer-panel__body {
  min-height: 0;
  flex: 1;
}

.dialer-number-entry {
  position: relative;
  padding: 14px 0 8px;
}

.dialer-number-entry :deep(.suggest-input__field) {
  height: 58px;
  padding: 0 14px;
  background: var(--background);
}

.dialer-number-entry :deep(.suggest-input__field input) {
  font-size: 20px;
  font-weight: 600;
}

.dialer-contact-name {
  display: block;
  margin: 6px 2px 0;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dialer-recording {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 10px;
  padding: 8px 2px;
  border-bottom: 1px solid var(--border);
}

.dialer-recording__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  color: var(--danger);
}

.dialer-recording__identity strong {
  color: var(--text);
  font-size: 13px;
}

.keypad__key {
  touch-action: manipulation;
  user-select: none;
}

.dialer-recording > input {
  position: relative;
  width: 38px;
  height: 22px;
  flex: 0 0 38px;
  appearance: none;
  background: #d8dde2;
  border-radius: 11px;
  cursor: pointer;
}

.dialer-recording > input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 16px;
  height: 16px;
  content: "";
  background: #ffffff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.dialer-recording > input:checked {
  background: var(--danger);
}

.dialer-recording > input:checked::before {
  transform: translateX(16px);
}

.dialer-recording > input:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.dialer-recording__error {
  margin-top: 7px;
  color: var(--danger);
  font-size: 12px;
}

</style>
