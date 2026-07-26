<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, LoaderCircle, Plus, Save, Trash2, X } from '@lucide/vue'
import type { LineSummary, TelegramUnit } from '../api/types'
import { ApiError } from '../api/types'
import {
  bootstrapResource,
  deleteTelegramUnit,
  lineKey,
  lineLabel,
  loadBootstrap,
  loadTelegramUnits,
  saveTelegramUnit,
  telegramResource
} from '../state/workspace'
import StatePanel from './StatePanel.vue'

const { t } = useI18n()
const selectedID = ref('')
const creating = ref(false)
const draftReturnID = ref('')
const displayName = ref('')
const enabled = ref(true)
const chatID = ref('')
const adminID = ref('')
const allLines = ref(true)
const lineScopes = ref<string[]>([])
const incomingSMS = ref(true)
const missedCalls = ref(true)
const botToken = ref('')
const saving = ref(false)
const deleting = ref(false)
const deleteConfirm = ref(false)
const saveError = ref('')
const saved = ref(false)

const selectedUnit = computed(() =>
  telegramResource.data.find(unit => unit.id === selectedID.value)
)
const lines = computed(() => bootstrapResource.data?.lines || [])

function telegramLineIdentity(line: LineSummary): string {
  const alias = lineLabel(line)
  const phoneNumber = line.phone_number.trim()
  return phoneNumber ? `${alias} · ${phoneNumber}` : alias
}

const scopeOptions = computed(() => {
  const options = lines.value.map(line => ({
    id: lineKey(line),
    label: telegramLineIdentity(line)
  }))
  for (const scope of lineScopes.value) {
    if (!options.some(option => option.id === scope)) {
      options.push({ id: scope, label: t('telegram.unknownLine') })
    }
  }
  return options
})
const tokenConfigured = computed(() => selectedUnit.value?.token_configured === true)
const validationError = computed(() => {
  if (!displayName.value.trim()) return t('telegram.enterName')
  if (!chatID.value.trim()) return t('telegram.enterChatID')
  if (!adminID.value.trim()) return t('telegram.enterAdminID')
  if (creating.value && !botToken.value.trim()) return t('telegram.tokenRequired')
  if (!allLines.value && lineScopes.value.length === 0) return t('telegram.selectLine')
  return ''
})

function applyUnit(unit?: TelegramUnit): void {
  const scopes = [...new Set(unit?.line_scopes || [])]
  displayName.value = unit?.display_name || ''
  enabled.value = unit?.enabled ?? true
  chatID.value = unit?.chat_id || ''
  adminID.value = unit?.admin_id || ''
  allLines.value = scopes.length === 0
  lineScopes.value = scopes
  incomingSMS.value = unit?.incoming_sms ?? true
  missedCalls.value = unit?.missed_calls ?? true
  botToken.value = ''
  saveError.value = ''
  saved.value = false
  deleteConfirm.value = false
}

watch(
  selectedUnit,
  unit => {
    if (!creating.value) applyUnit(unit)
  },
  { immediate: true }
)

watch(
  () => telegramResource.status,
  status => {
    if (status !== 'ready' || creating.value) return
    if (!selectedUnit.value && telegramResource.data[0]) {
      selectedID.value = telegramResource.data[0].id
    }
  },
  { immediate: true }
)

watch(
  [displayName, enabled, chatID, adminID, allLines, lineScopes, incomingSMS, missedCalls, botToken],
  () => {
    saved.value = false
    saveError.value = ''
    deleteConfirm.value = false
  },
  { deep: true }
)

function selectAllLines(): void {
  allLines.value = true
  lineScopes.value = []
}

function toggleLineScope(lineID: string, event: Event): void {
  const checked = (event.currentTarget as HTMLInputElement).checked
  const scopes = checked
    ? [...new Set([...lineScopes.value, lineID])]
    : lineScopes.value.filter(scope => scope !== lineID)

  lineScopes.value = scopes
  allLines.value = scopes.length === 0
}

function normalizedLineScopes(): string[] {
  const scopes = [...new Set(lineScopes.value.filter(Boolean))]
  if (allLines.value || scopes.length === 0) {
    selectAllLines()
    return []
  }
  allLines.value = false
  lineScopes.value = scopes
  return scopes
}

function selectUnit(id: string): void {
  if (saving.value || deleting.value) return
  discardDraft(id)
}

function startCreate(): void {
  if (saving.value || deleting.value) return
  if (!creating.value) {
    draftReturnID.value = selectedUnit.value?.id || telegramResource.data[0]?.id || ''
  }
  creating.value = true
  selectedID.value = '__telegram-bot-draft__'
  applyUnit()
}

function discardDraft(nextID?: string): void {
  const returnID =
    nextID ||
    (telegramResource.data.some(unit => unit.id === draftReturnID.value)
      ? draftReturnID.value
      : telegramResource.data[0]?.id || '')

  creating.value = false
  draftReturnID.value = ''
  selectedID.value = returnID
  applyUnit(telegramResource.data.find(unit => unit.id === returnID))
}

function cancelCreate(): void {
  if (!creating.value || saving.value || deleting.value) return
  discardDraft()
}

async function submit(): Promise<void> {
  if (saving.value || deleting.value || validationError.value) return
  saving.value = true
  saveError.value = ''
  saved.value = false
  try {
    const current = selectedUnit.value
    const unit = await saveTelegramUnit(
      {
        display_name: displayName.value,
        enabled: enabled.value,
        chat_id: chatID.value,
        admin_id: adminID.value,
        line_scopes: normalizedLineScopes(),
        incoming_sms: incomingSMS.value,
        missed_calls: missedCalls.value,
        ...(botToken.value.trim() ? { bot_token: botToken.value.trim() } : {}),
        ...(current ? { revision: current.revision } : {})
      },
      current?.id
    )
    creating.value = false
    draftReturnID.value = ''
    selectedID.value = unit.id
    applyUnit(unit)
    await nextTick()
    saved.value = true
  } catch (error) {
    saveError.value =
      error instanceof ApiError && error.status === 403
        ? t('telegram.updateForbidden')
        : error instanceof Error
          ? error.message
          : t('telegram.saveFailed')
  } finally {
    saving.value = false
  }
}

async function remove(): Promise<void> {
  const unit = selectedUnit.value
  if (!unit || deleting.value || saving.value) return
  if (!deleteConfirm.value) {
    deleteConfirm.value = true
    return
  }

  deleting.value = true
  saveError.value = ''
  try {
    await deleteTelegramUnit(unit)
    selectedID.value = telegramResource.data[0]?.id || ''
    creating.value = false
    if (!selectedID.value) applyUnit()
  } catch (error) {
    saveError.value =
      error instanceof ApiError && error.status === 403
        ? t('telegram.deleteForbidden')
        : error instanceof Error
          ? error.message
          : t('telegram.deleteFailed')
  } finally {
    deleting.value = false
    deleteConfirm.value = false
  }
}

onMounted(() => {
  void Promise.all([loadTelegramUnits(), loadBootstrap()])
})
</script>

<template>
  <StatePanel
    v-if="telegramResource.status === 'loading' || telegramResource.status === 'idle'"
    state="loading"
    :title="t('telegram.loading')"
  />
  <StatePanel
    v-else-if="telegramResource.status === 'forbidden'"
    state="forbidden"
    :title="t('telegram.viewForbidden')"
    :detail="telegramResource.error"
  />
  <StatePanel
    v-else-if="telegramResource.status === 'error'"
    state="error"
    :title="t('telegram.loadFailed')"
    :detail="telegramResource.error"
    retryable
    @retry="loadTelegramUnits(true)"
  />
  <div v-else class="telegram-settings">
    <aside class="telegram-unit-list" aria-label="Telegram Bot">
      <header>
        <h3>Bot</h3>
        <button
          class="icon-button"
          type="button"
          :title="t('telegram.newBot')"
          :aria-label="t('telegram.newTelegramBot')"
          @click="startCreate"
        >
          <Plus :size="18" />
        </button>
      </header>
      <div v-if="telegramResource.data.length === 0 && !creating" class="telegram-unit-empty">
        {{ t('telegram.empty') }}
      </div>
      <button
        v-if="creating"
        class="telegram-unit-row is-selected"
        type="button"
        aria-current="true"
      >
        <span class="telegram-unit-row__status" />
        <span>
          <strong>{{ displayName.trim() || t('telegram.unnamed') }}</strong>
          <small>{{ t('telegram.unsaved') }}</small>
        </span>
      </button>
      <button
        v-for="unit in telegramResource.data"
        :key="unit.id"
        class="telegram-unit-row"
        :class="{ 'is-selected': !creating && unit.id === selectedID }"
        type="button"
        @click="selectUnit(unit.id)"
      >
        <span class="telegram-unit-row__status" :class="{ 'is-enabled': unit.enabled }" />
        <span>
          <strong>{{ unit.display_name }}</strong>
          <small>{{ unit.bot_username ? `@${unit.bot_username}` : unit.chat_id }}</small>
        </span>
      </button>
    </aside>

    <section class="telegram-unit-editor">
      <StatePanel
        v-if="!creating && !selectedUnit"
        state="empty"
        :title="t('telegram.selectOrCreate')"
      />
      <form v-else class="settings-form" @submit.prevent="submit">
        <div class="telegram-form-heading">
          <h3>{{ creating ? t('telegram.newBot') : t('telegram.botSettings') }}</h3>
          <label class="compact-switch">
            <span>{{ t('telegram.enabled') }}</span>
            <input v-model="enabled" type="checkbox" role="switch" />
          </label>
        </div>

        <div class="settings-form-grid">
          <label class="field">
            <span>{{ t('telegram.name') }}</span>
            <input v-model="displayName" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>Chat ID</span>
            <input v-model="chatID" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>{{ t('telegram.administratorID') }}</span>
            <input v-model="adminID" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>Bot token</span>
            <input
              v-model="botToken"
              type="password"
              autocomplete="new-password"
              :placeholder="tokenConfigured ? t('telegram.configured') : ''"
              :disabled="saving || deleting"
            />
            <small class="field-status">
              {{ tokenConfigured ? t('telegram.configured') : t('telegram.notConfigured') }}
            </small>
          </label>
        </div>

        <fieldset class="telegram-options">
          <legend>{{ t('telegram.notifications') }}</legend>
          <label><input v-model="incomingSMS" type="checkbox" />{{ t('telegram.incomingSMS') }}</label>
          <label><input v-model="missedCalls" type="checkbox" />{{ t('telegram.missedCalls') }}</label>
        </fieldset>

        <fieldset class="telegram-options telegram-line-scopes">
          <legend>{{ t('telegram.lineScope') }}</legend>
          <label>
            <input
              :checked="allLines"
              type="checkbox"
              :disabled="saving || deleting"
              @click.prevent="selectAllLines"
            />
            {{ t('telegram.allLines') }}
          </label>
          <label v-for="line in scopeOptions" :key="line.id">
            <input
              :checked="lineScopes.includes(line.id)"
              type="checkbox"
              :disabled="saving || deleting"
              @change="toggleLineScope(line.id, $event)"
            />
            {{ line.label }}
          </label>
        </fieldset>

        <footer class="settings-form-actions">
          <button
            v-if="creating"
            class="secondary-button"
            type="button"
            :disabled="saving || deleting"
            @click="cancelCreate"
          >
            <X :size="16" />
            <span>{{ t('common.cancel') }}</span>
          </button>
          <button
            v-else-if="selectedUnit"
            class="danger-button"
            type="button"
            :disabled="saving || deleting"
            @click="remove"
          >
            <LoaderCircle v-if="deleting" class="spin" :size="16" />
            <Trash2 v-else :size="16" />
            <span>{{ deleteConfirm ? t('telegram.confirmDelete') : t('common.delete') }}</span>
          </button>
          <span v-else />

          <div class="settings-form-feedback">
            <p v-if="validationError" class="field-error" role="alert">{{ validationError }}</p>
            <p v-else-if="saveError" class="field-error" role="alert">{{ saveError }}</p>
            <span v-else-if="saved" class="save-status">
              <Check :size="15" />{{ t('telegram.saved') }}
            </span>
          </div>

          <button
            class="primary-button"
            type="submit"
            :disabled="saving || deleting || Boolean(validationError)"
          >
            <LoaderCircle v-if="saving" class="spin" :size="17" />
            <Save v-else :size="17" />
            <span>{{ t('common.save') }}</span>
          </button>
        </footer>
      </form>
    </section>
  </div>
</template>
