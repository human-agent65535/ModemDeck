<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Circle, Delete, LoaderCircle, Phone, RotateCcw, X } from '@lucide/vue'
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
  lineLabel,
  lineSupports,
  loadContacts
} from '../state/workspace'
import ContactSuggestInput from './ContactSuggestInput.vue'

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
let resettingDraft = false

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
const recordingStatusLabel = computed(() => {
  if (dialerRecordingState.status === 'loading') return '正在读取默认设置'
  if (dialerRecordingState.status === 'error') return '默认设置不可用'
  if (dialerRecordingState.overridden) return '已覆盖默认设置'
  return dialerRecordingState.enabled ? '跟随默认：开启' : '跟随默认：关闭'
})

function focusNumber(): void {
  inputAutofocus.value = false
  void nextTick(() => {
    inputAutofocus.value = true
  })
}

function beginDraft(target = '', label = '', focus = false): void {
  resettingDraft = true
  number.value = target
  contactLabel.value = label
  if (!lines.value.some(line => lineKey(line) === selectedLineId.value)) {
    selectedLineId.value = ''
  }
  resettingDraft = false
  void resetDialerRecording()
  if (focus) focusNumber()
}

watch(
  () => uiState.dialRequestRevision,
  revision => {
    if (revision > 0) beginDraft(uiState.dialTarget, uiState.dialLabel, true)
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
  lines,
  value => {
    if (!value.some(line => lineKey(line) === selectedLineId.value)) {
      const hadLine = Boolean(selectedLineId.value)
      selectedLineId.value = ''
      if (hadLine) void resetDialerRecording()
    }
  },
  { immediate: true }
)

watch(number, (value, previous) => {
  if (resettingDraft || !previous.trim() || value.trim()) return
  contactLabel.value = ''
  void resetDialerRecording()
})

function appendDigit(digit: string): void {
  if (digit === '0' && number.value === '') {
    number.value = '0'
    return
  }
  number.value += digit
  contactLabel.value = ''
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

function resetDraft(): void {
  beginDraft('', '', true)
}

function changeRecording(event: Event): void {
  setDialerRecording((event.target as HTMLInputElement).checked)
}

onMounted(() => {
  beginDraft()
  void loadContacts()
})
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
          <header class="tool-header">
            <div>
              <h2>拨号</h2>
              <p v-if="contactLabel">{{ contactLabel }}</p>
            </div>
            <span class="dialer-header-actions">
              <button
                class="icon-button"
                type="button"
                title="新建拨号"
                aria-label="清空并新建拨号"
                @click="resetDraft"
              >
                <RotateCcw :size="18" />
              </button>
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
            <ContactSuggestInput
              v-model="number"
              :contacts="contactsResource.data"
              :autofocus="inputAutofocus"
              @select="chooseContact"
            />

            <label v-if="lines.length > 0" class="field">
              <span>线路</span>
              <select v-model="selectedLineId" aria-label="通话线路">
                <option value="" disabled>选择线路</option>
                <option v-for="line in lines" :key="lineKey(line)" :value="lineKey(line)">
                  {{ lineLabel(line) }}{{ line.phone_number ? ` · ${line.phone_number}` : '' }}{{
                    lineSupports(line, 'dial') === false ? ' · 不支持拨号' : ''
                  }}
                </option>
              </select>
            </label>

            <label class="dialer-recording">
              <span class="dialer-recording__identity">
                <Circle :size="16" fill="currentColor" aria-hidden="true" />
                <span>
                  <strong>本次通话录音</strong>
                  <small>{{ recordingStatusLabel }}</small>
                </span>
              </span>
              <input
                type="checkbox"
                role="switch"
                :checked="dialerRecordingState.enabled"
                :disabled="dialerRecordingState.status !== 'ready'"
                aria-label="本次通话录音"
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
                @click="appendDigit(key.digit)"
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
            <p v-else-if="!selectedLineId" class="unavailable-note">选择线路</p>
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

.dialer-panel__body {
  min-height: 0;
  flex: 1;
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

.dialer-recording__identity > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.dialer-recording__identity strong {
  color: var(--text);
  font-size: 12px;
}

.dialer-recording__identity small {
  color: var(--muted);
  font-size: 10px;
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
  font-size: 10px;
}

</style>
