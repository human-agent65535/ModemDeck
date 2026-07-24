<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { Plus, Star, Trash2, X } from '@lucide/vue'
import type { Contact, ContactInput, LineSummary } from '../api/types'
import LineSelector from './LineSelector.vue'

type PhoneDraft = {
  id?: string
  label: string
  number: string
  primary: boolean
}

const props = defineProps<{
  open: boolean
  contact?: Contact
  lines?: LineSummary[]
  saving?: boolean
  error?: string
}>()

const emit = defineEmits<{
  close: []
  save: [input: ContactInput]
}>()

const draft = reactive<{
  name: string
  notes: string
  favorite: boolean
  preferredDeviceIMEI: string
  phones: PhoneDraft[]
}>({
  name: '',
  notes: '',
  favorite: false,
  preferredDeviceIMEI: '',
  phones: []
})

const valid = computed(
  () =>
    Boolean(draft.name.trim()) &&
    draft.phones.length > 0 &&
    draft.phones.every(phone => Boolean(phone.number.trim())) &&
    draft.phones.filter(phone => phone.primary).length === 1
)

watch(
  () => [props.open, props.contact] as const,
  ([open, contact]) => {
    if (!open) return
    draft.name = contact?.display_name || ''
    draft.notes = contact?.notes || ''
    draft.favorite = contact?.favorite || false
    draft.preferredDeviceIMEI = contact?.preferred_device_imei || ''
    draft.phones = contact?.phones.length
      ? contact.phones.map(phone => ({
          id: phone.id,
          label: phone.label,
          number: phone.number,
          primary: phone.primary
        }))
      : [{ label: '手机', number: '', primary: true }]
  },
  { immediate: true }
)

function addPhone(): void {
  draft.phones.push({
    label: draft.phones.length === 0 ? '手机' : '其他',
    number: '',
    primary: draft.phones.length === 0
  })
}

function removePhone(index: number): void {
  const wasPrimary = draft.phones[index]?.primary
  draft.phones.splice(index, 1)
  if (wasPrimary && draft.phones[0]) draft.phones[0].primary = true
}

function setPrimary(index: number): void {
  draft.phones.forEach((phone, phoneIndex) => {
    phone.primary = phoneIndex === index
  })
}

function submit(): void {
  if (!valid.value || props.saving) return
  emit('save', {
    display_name: draft.name.trim(),
    favorite: draft.favorite,
    notes: draft.notes.trim() || undefined,
    preferred_device_imei: draft.preferredDeviceIMEI || undefined,
    revision: props.contact?.revision,
    phones: draft.phones.map(phone => ({
      id: phone.id,
      label: phone.label.trim() || '电话',
      number: phone.number.trim(),
      primary: phone.primary
    }))
  })
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="modal-backdrop" @mousedown.self="emit('close')">
        <section
          class="editor-dialog"
          role="dialog"
          aria-modal="true"
          :aria-label="contact ? '编辑联系人' : '新建联系人'"
          @keydown.esc="emit('close')"
        >
          <header class="tool-header">
            <h2>{{ contact ? '编辑联系人' : '新建联系人' }}</h2>
            <button class="icon-button" type="button" title="关闭" @click="emit('close')">
              <X :size="19" />
            </button>
          </header>

          <form class="editor-form" @submit.prevent="submit">
            <label class="field">
              <span>姓名</span>
              <input v-model="draft.name" autocomplete="name" required />
            </label>

            <label class="contact-favorite-toggle">
              <span>
                <Star :size="18" :fill="draft.favorite ? 'currentColor' : 'none'" />
                <strong>收藏联系人</strong>
              </span>
              <input v-model="draft.favorite" type="checkbox" role="switch" />
            </label>

            <fieldset class="phone-fields">
              <legend>电话号码</legend>
              <div v-for="(phone, index) in draft.phones" :key="phone.id || index" class="phone-row">
                <input v-model="phone.label" class="phone-row__label" aria-label="号码类型" />
                <input v-model="phone.number" type="tel" autocomplete="tel" aria-label="电话号码" required />
                <label class="primary-radio" :title="phone.primary ? '主要号码' : '设为主要号码'">
                  <input
                    type="radio"
                    name="primary-phone"
                    :checked="phone.primary"
                    @change="setPrimary(index)"
                  />
                  <span>主要</span>
                </label>
                <button
                  class="icon-button icon-button--quiet"
                  type="button"
                  title="移除号码"
                  :disabled="draft.phones.length === 1"
                  @click="removePhone(index)"
                >
                  <Trash2 :size="17" />
                </button>
              </div>
              <button class="text-button" type="button" @click="addPhone">
                <Plus :size="16" />添加号码
              </button>
            </fieldset>

            <LineSelector
              v-if="lines?.length"
              v-model="draft.preferredDeviceIMEI"
              :lines="lines"
              label="首选线路"
              value-field="device_imei"
              include-all
              all-value=""
              all-label="跟随默认线路"
              all-description="未指定时使用全局默认线路"
            />

            <label class="field">
              <span>备注</span>
              <textarea v-model="draft.notes" rows="3" />
            </label>

            <p v-if="error" class="field-error" role="alert">{{ error }}</p>
            <footer class="dialog-actions">
              <button class="secondary-button" type="button" @click="emit('close')">取消</button>
              <button class="primary-button" type="submit" :disabled="!valid || saving">
                {{ saving ? '正在保存…' : '保存' }}
              </button>
            </footer>
          </form>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.contact-favorite-toggle {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid var(--border);
}

.contact-favorite-toggle > span {
  display: flex;
  align-items: center;
  gap: 9px;
}

.contact-favorite-toggle input {
  position: relative;
  width: 42px;
  height: 24px;
  flex: 0 0 auto;
  appearance: none;
  background: #d8dde2;
  border-radius: 12px;
  cursor: pointer;
}

.contact-favorite-toggle input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.contact-favorite-toggle input:checked {
  color: var(--accent-strong);
  background: var(--accent);
}

.contact-favorite-toggle input:checked::before {
  transform: translateX(18px);
}

.contact-favorite-toggle input:focus-visible {
  outline: 3px solid rgb(17 120 100 / 18%);
  outline-offset: 2px;
}
</style>
