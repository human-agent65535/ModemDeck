<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { Delete, Phone, X } from '@lucide/vue'
import { callState, dial } from '../state/call'
import { closeDialer, uiState } from '../state/ui'
import {
  bootstrapResource,
  capabilityReason,
  contactsResource,
  lineKey,
  lineLabel,
  loadContacts
} from '../state/workspace'
import ContactSuggestInput from './ContactSuggestInput.vue'

const number = ref('')
const contactLabel = ref('')
const selectedLineId = ref('')
const inputAutofocus = ref(false)

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
    validationError.value ||
    (!number.value.trim() ? '请输入号码' : '')
)

watch(
  () => uiState.dialerOpen,
  open => {
    if (!open) return
    number.value = uiState.dialTarget
    contactLabel.value = uiState.dialLabel
    if (!lines.value.some(line => lineKey(line) === selectedLineId.value)) {
      selectedLineId.value = lines.value[0] ? lineKey(lines.value[0]) : ''
    }
    inputAutofocus.value = false
    void nextTick(() => {
      inputAutofocus.value = true
    })
    void loadContacts()
  }
)

watch(
  lines,
  value => {
    if (!value.some(line => lineKey(line) === selectedLineId.value)) {
      selectedLineId.value = value[0] ? lineKey(value[0]) : ''
    }
  },
  { immediate: true }
)

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
  await dial(number.value.trim(), selectedLineId.value)
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="uiState.dialerOpen" class="drawer-backdrop" @mousedown.self="closeDialer">
        <section class="dialer-panel" role="dialog" aria-modal="true" aria-label="拨号" @keydown.esc="closeDialer">
          <header class="tool-header">
            <div>
              <h2>拨号</h2>
              <p v-if="contactLabel">{{ contactLabel }}</p>
            </div>
            <button class="icon-button" type="button" title="关闭" aria-label="关闭拨号盘" @click="closeDialer">
              <X :size="19" />
            </button>
          </header>

          <div class="dialer-panel__body">
            <ContactSuggestInput
              v-model="number"
              :contacts="contactsResource.data"
              :autofocus="inputAutofocus"
              @select="chooseContact"
            />

            <label v-if="lines.length > 1" class="field">
              <span>线路</span>
              <select v-model="selectedLineId">
                <option v-for="line in lines" :key="lineKey(line)" :value="lineKey(line)">
                  {{ lineLabel(line) }}{{ line.phone_number ? ` · ${line.phone_number}` : '' }}
                </option>
              </select>
            </label>
            <div v-else-if="lines.length === 1" class="dialer-line">
              <span>线路</span>
              <strong>{{ lines[0] ? lineLabel(lines[0]) : '' }}</strong>
              <small v-if="lines[0]?.phone_number">{{ lines[0]?.phone_number }}</small>
            </div>

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
                <Phone :size="22" />
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
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
