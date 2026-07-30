<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Bell,
  CardSim,
  Check,
  CircleCheck,
  CircleOff,
  KeyRound,
  ListFilter,
  LoaderCircle,
  MessageSquareText,
  PhoneMissed,
  Plus,
  Save,
  Send,
  Trash2,
  UsersRound,
  X
} from '@lucide/vue'
import type { LineSummary, TelegramUnit, UserAccount } from '../api/types'
import { ApiError } from '../api/types'
import { gateway } from '../api/client'
import { sessionState } from '../state/session'
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
import { lineTone } from '../utils/lineTone'
import LineTag from './LineTag.vue'
import StatePanel from './StatePanel.vue'

type TelegramScopeOption = {
  id: string
  label: string
  phoneNumber: string
  line?: LineSummary
}

const { t } = useI18n()
const selectedID = ref('')
const creating = ref(false)
const draftReturnID = ref('')
const displayName = ref('')
const enabled = ref(true)
const chatID = ref('')
const adminID = ref('')
const assignedUserID = ref('')
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
const users = ref<UserAccount[]>([])
const isAdmin = computed(() => sessionState.role === 'admin')
const currentSessionUser = computed<UserAccount>(() => ({
  id: sessionState.userID,
  username: sessionState.username,
  role: sessionState.role === 'admin' ? 'admin' : 'member',
  enabled: true,
  must_change_password: sessionState.mustChangePassword,
  revision: 0,
  line_ids: [...sessionState.allowedLineIDs],
  created_at: '',
  updated_at: ''
}))
const availableUsers = computed(() =>
  isAdmin.value ? users.value : [currentSessionUser.value]
)

const selectedUnit = computed(() =>
  telegramResource.data.find(unit => unit.id === selectedID.value)
)
const lines = computed(
  () => bootstrapResource.data?.line_catalog || bootstrapResource.data?.lines || []
)
const assignedUser = computed(() =>
  availableUsers.value.find(user => user.id === assignedUserID.value)
)
const inheritedLineIDs = computed(() => {
  if (!assignedUser.value) return []
  return assignedUser.value.line_ids
})

const scopeOptions = computed<TelegramScopeOption[]>(() => {
  const inherited = new Set(inheritedLineIDs.value)
  const options: TelegramScopeOption[] = lines.value.flatMap(line => {
    const id = lineKey(line)
    return id && inherited.has(id)
      ? [{
          id,
          label: lineLabel(line),
          phoneNumber: line.phone_number.trim(),
          line
        }]
      : []
  })
  for (const scope of lineScopes.value) {
    if (!options.some(option => option.id === scope)) {
      options.push({
        id: scope,
        label: t('telegram.unknownLine'),
        phoneNumber: ''
      })
    }
  }
  return options
})

function scopedLine(scopeID: string): LineSummary | undefined {
  return lines.value.find(line => lineKey(line) === scopeID)
}

function unitScopeSummary(unit: TelegramUnit): string {
  const summary = unit.all_assigned_lines
    ? t('telegram.allAssignedLines')
    : unit.line_scopes
    .map(scopeID => {
      const line = scopedLine(scopeID)
      if (!line) return t('telegram.unknownLine')
      const label = lineLabel(line)
      const phoneNumber = line.phone_number.trim()
      return phoneNumber ? `${label} · ${phoneNumber}` : label
    })
    .join(t('common.listSeparator'))
  return t('telegram.userScopeSummary', {
    user: unit.assigned_username || t('telegram.unknownUser'),
    lines: summary
  })
}

function scopeToneStyle(line?: LineSummary): Record<string, string> | undefined {
  if (!line) return undefined
  const tone = lineTone(line)
  return {
    color: tone.foreground,
    backgroundColor: tone.background,
    borderColor: tone.border
  }
}

const tokenConfigured = computed(() => selectedUnit.value?.token_configured === true)
const validationError = computed(() => {
  if (!displayName.value.trim()) return t('telegram.enterName')
  if (!chatID.value.trim()) return t('telegram.enterChatID')
  if (!adminID.value.trim()) return t('telegram.enterAdminID')
  if (creating.value && !botToken.value.trim()) return t('telegram.tokenRequired')
  if (!assignedUserID.value) {
    return t('telegram.selectUser')
  }
  if (
    !allLines.value &&
    lineScopes.value.length === 0
  ) {
    return t('telegram.selectLine')
  }
  return ''
})

function applyUnit(unit?: TelegramUnit): void {
  const scopes = [...new Set(unit?.line_scopes || [])]
  displayName.value = unit?.display_name || ''
  enabled.value = unit?.enabled ?? true
  chatID.value = unit?.chat_id || ''
  adminID.value = unit?.admin_id || ''
  assignedUserID.value =
    unit?.assigned_user_id ||
    (isAdmin.value
      ? users.value.find(
          user => user.id === sessionState.userID && user.enabled
        )?.id || users.value.find(user => user.enabled)?.id
      : sessionState.userID) ||
    ''
  allLines.value = unit ? unit.all_assigned_lines : true
  lineScopes.value = unit?.all_assigned_lines ? [] : scopes
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
  [
    displayName,
    enabled,
    chatID,
    adminID,
    assignedUserID,
    allLines,
    lineScopes,
    incomingSMS,
    missedCalls,
    botToken
  ],
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

function changeAssignedUser(event: Event): void {
  assignedUserID.value = (event.currentTarget as HTMLSelectElement).value
  selectAllLines()
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
        assigned_user_id: assignedUserID.value,
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
  const usersRequest = isAdmin.value
    ? gateway.listUsers().then(loaded => {
        users.value = loaded
      })
    : Promise.resolve()
  void Promise.all([loadTelegramUnits(), loadBootstrap(), usersRequest]).then(() => {
    applyUnit(selectedUnit.value)
  })
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
  <div v-else class="telegram-settings-container">
    <div class="telegram-settings">
      <aside class="telegram-unit-list" :aria-label="t('telegram.bots')">
      <header class="telegram-unit-list__heading">
        <span class="telegram-unit-list__title">
          <strong>{{ t('telegram.bots') }}</strong>
          <small>Telegram</small>
        </span>
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
        <span class="telegram-unit-row__icon is-enabled" aria-hidden="true">
          <Send :size="18" />
        </span>
        <span class="telegram-unit-row__copy">
          <span class="telegram-unit-row__topline">
            <strong>{{ displayName.trim() || t('telegram.unnamed') }}</strong>
            <span class="telegram-unit-row__state is-draft">
              {{ t('telegram.unsaved') }}
            </span>
          </span>
          <small>Telegram Bot</small>
          <span class="telegram-unit-row__scope">
            <UsersRound :size="13" aria-hidden="true" />
            {{ assignedUser?.username || t('telegram.selectUser') }}
          </span>
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
        <span
          class="telegram-unit-row__icon"
          :class="{ 'is-enabled': unit.effective_enabled }"
          aria-hidden="true"
        >
          <Send :size="18" />
        </span>
        <span class="telegram-unit-row__copy">
          <span class="telegram-unit-row__topline">
            <strong>{{ unit.display_name }}</strong>
            <span
              class="telegram-unit-row__state"
              :class="{ 'is-enabled': unit.effective_enabled }"
            >
              <CircleCheck v-if="unit.effective_enabled" :size="12" aria-hidden="true" />
              <CircleOff v-else :size="12" aria-hidden="true" />
              {{ unit.effective_enabled ? t('lines.enabled') : t('lines.disabled') }}
            </span>
          </span>
          <small>{{ unit.bot_username ? `@${unit.bot_username}` : unit.chat_id }}</small>
          <span class="telegram-unit-row__scope" :title="unitScopeSummary(unit)">
            <UsersRound :size="13" aria-hidden="true" />
            {{ unitScopeSummary(unit) }}
          </span>
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
          <span class="telegram-form-heading__icon" aria-hidden="true">
            <Send :size="19" />
          </span>
          <span class="telegram-form-heading__copy">
            <h3>
              {{ displayName.trim() || (creating ? t('telegram.newBot') : t('telegram.unnamed')) }}
            </h3>
            <small>Telegram Bot</small>
          </span>
          <label class="compact-switch">
            <span>{{ t('telegram.enabled') }}</span>
            <input
              v-model="enabled"
              type="checkbox"
              role="switch"
              :disabled="saving || deleting"
            />
          </label>
        </div>

        <section class="telegram-editor-section telegram-bot-identity">
          <header class="telegram-editor-section__heading">
            <span class="telegram-editor-section__icon" aria-hidden="true">
              <KeyRound :size="17" />
            </span>
            <h4>{{ t('telegram.botSettings') }}</h4>
          </header>
          <div class="settings-form-grid">
            <label class="field">
              <span>{{ t('telegram.name') }}</span>
              <input
                v-model="displayName"
                type="text"
                autocomplete="off"
                :disabled="saving || deleting"
              />
            </label>
            <label class="field">
              <span>Chat ID</span>
              <input
                v-model="chatID"
                type="text"
                autocomplete="off"
                :disabled="saving || deleting"
              />
            </label>
            <label class="field">
              <span>{{ t('telegram.allowedUserID') }}</span>
              <input
                v-model="adminID"
                type="text"
                autocomplete="off"
                :disabled="saving || deleting"
              />
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
        </section>

        <section v-if="isAdmin" class="telegram-editor-section telegram-access-source">
          <header class="telegram-editor-section__heading">
            <span class="telegram-editor-section__icon" aria-hidden="true">
              <UsersRound :size="17" />
            </span>
            <h4>{{ t('telegram.botOwner') }}</h4>
          </header>
          <label class="field telegram-user-select">
            <span>{{ t('telegram.assignedUser') }}</span>
            <select
              :value="assignedUserID"
              :disabled="saving || deleting"
              @change="changeAssignedUser"
            >
              <option value="" disabled>{{ t('telegram.selectUser') }}</option>
              <option v-for="user in availableUsers" :key="user.id" :value="user.id">
                {{ user.username }}
                {{ user.enabled ? '' : `· ${t('users.disabled')}` }}
              </option>
            </select>
            <small>{{ t('telegram.botOwnerDescription') }}</small>
          </label>
        </section>

        <fieldset class="telegram-options">
          <legend class="sr-only">{{ t('telegram.notifications') }}</legend>
          <header class="telegram-editor-section__heading">
            <span class="telegram-editor-section__icon" aria-hidden="true">
              <Bell :size="17" />
            </span>
            <h4>{{ t('telegram.notifications') }}</h4>
          </header>
          <div class="telegram-event-options">
            <label :class="{ 'is-selected': incomingSMS }">
              <span class="telegram-event-option__icon" aria-hidden="true">
                <MessageSquareText :size="18" />
              </span>
              <strong>{{ t('telegram.incomingSMS') }}</strong>
              <input
                v-model="incomingSMS"
                type="checkbox"
                :disabled="saving || deleting"
              />
            </label>
            <label :class="{ 'is-selected': missedCalls }">
              <span class="telegram-event-option__icon" aria-hidden="true">
                <PhoneMissed :size="18" />
              </span>
              <strong>{{ t('telegram.missedCalls') }}</strong>
              <input
                v-model="missedCalls"
                type="checkbox"
                :disabled="saving || deleting"
              />
            </label>
          </div>
        </fieldset>

        <fieldset class="telegram-options telegram-line-scopes">
          <legend class="sr-only">{{ t('telegram.lineScope') }}</legend>
          <header class="telegram-editor-section__heading">
            <span class="telegram-editor-section__icon" aria-hidden="true">
              <CardSim :size="17" />
            </span>
            <h4>
              {{ t('telegram.userLines') }}
            </h4>
          </header>
          <div class="telegram-scope-options">
            <label class="telegram-scope-option" :class="{ 'is-selected': allLines }">
              <input
                :checked="allLines"
                type="checkbox"
                :disabled="saving || deleting"
                @click.prevent="selectAllLines"
              />
              <span class="telegram-scope-option__icon is-all" aria-hidden="true">
                <ListFilter :size="18" />
              </span>
              <span class="telegram-scope-option__copy">
                <strong>{{ t('telegram.allAssignedLines') }}</strong>
                <small>{{ t('telegram.allAssignedLinesDescription') }}</small>
              </span>
              <Check
                v-if="allLines"
                class="telegram-scope-option__check"
                :size="17"
                aria-hidden="true"
              />
            </label>
            <label
              v-for="line in scopeOptions"
              :key="line.id"
              class="telegram-scope-option"
              :class="{ 'is-selected': lineScopes.includes(line.id) }"
            >
              <input
                :checked="lineScopes.includes(line.id)"
                type="checkbox"
                :disabled="saving || deleting"
                @change="toggleLineScope(line.id, $event)"
              />
              <span
                class="telegram-scope-option__icon"
                :style="scopeToneStyle(line.line)"
                aria-hidden="true"
              >
                <CardSim :size="18" />
              </span>
              <span class="telegram-scope-option__copy">
                <LineTag v-if="line.line" :line="line.line" :fallback="line.label" />
                <strong v-else>{{ line.label }}</strong>
                <small>{{ line.phoneNumber || t('lines.cellularLine') }}</small>
              </span>
              <Check
                v-if="lineScopes.includes(line.id)"
                class="telegram-scope-option__check"
                :size="17"
                aria-hidden="true"
              />
            </label>
          </div>
          <p v-if="assignedUser && inheritedLineIDs.length === 0" class="telegram-scope-empty">
            {{ t('telegram.userHasNoLines') }}
          </p>
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
  </div>
</template>

<style scoped>
.telegram-settings-container {
  min-width: 0;
  container-type: inline-size;
}

.telegram-settings {
  min-height: 520px;
  grid-template-columns: 280px minmax(0, 1fr);
}

.telegram-unit-list {
  min-width: 0;
  background: var(--surface-subtle);
}

.telegram-unit-list__heading {
  min-height: 64px;
  padding: 8px 12px;
  background: var(--surface);
}

.telegram-unit-list__title,
.telegram-form-heading__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.telegram-unit-list__title strong,
.telegram-form-heading__copy h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
  font-weight: 750;
}

.telegram-unit-list__title small,
.telegram-form-heading__copy small {
  color: var(--muted);
  font-size: 11px;
  font-weight: 550;
}

.telegram-unit-row {
  min-height: 86px;
  grid-template-columns: 38px minmax(0, 1fr);
  gap: 10px;
  padding: 10px 12px;
  background: var(--surface);
}

.telegram-unit-row.is-selected {
  background: var(--accent-soft);
}

.telegram-unit-row__icon {
  display: inline-flex;
  width: 36px;
  height: 36px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 50%;
}

.telegram-unit-row__icon.is-enabled {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: #b9ddd5;
}

.telegram-unit-row__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.telegram-unit-row__topline {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.telegram-unit-row__topline > strong {
  min-width: 0;
  overflow: hidden;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.telegram-unit-row__copy > small {
  overflow: hidden;
  color: var(--muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.telegram-unit-row__state {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 700;
}

.telegram-unit-row__state.is-enabled {
  color: var(--success);
}

.telegram-unit-row__state.is-draft {
  color: var(--accent-strong);
}

.telegram-unit-row__scope {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  overflow: hidden;
  color: var(--muted);
  font-size: 10px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.telegram-unit-row__scope svg {
  flex: 0 0 auto;
}

.telegram-unit-editor {
  min-width: 0;
  padding-left: 24px;
}

.telegram-unit-editor .settings-form {
  width: 100%;
  max-width: 920px;
}

.telegram-form-heading {
  display: grid;
  min-height: 64px;
  align-items: center;
  grid-template-columns: 40px minmax(0, 1fr) auto;
  gap: 10px;
}

.telegram-form-heading__icon,
.telegram-editor-section__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border: 1px solid #c8e5de;
  border-radius: 7px;
}

.telegram-form-heading__icon {
  width: 36px;
  height: 36px;
}

.telegram-editor-section {
  min-width: 0;
  padding: 18px 0;
  border-top: 1px solid var(--border);
}

.telegram-bot-identity {
  border-top: 0;
}

.telegram-editor-section__heading {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  margin-bottom: 14px;
}

.telegram-editor-section__icon {
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border-radius: 6px;
}

.telegram-editor-section__heading h4 {
  margin: 0;
  color: var(--text);
  font-size: 13px;
  font-weight: 750;
}

.telegram-bot-identity .settings-form-grid {
  gap: 16px;
  padding: 0;
}

.telegram-bot-identity .field {
  min-width: 0;
}

.telegram-bot-identity .field > input {
  width: 100%;
  min-width: 0;
}

.telegram-access-options {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.telegram-access-options > label {
  position: relative;
  display: grid;
  min-width: 0;
  min-height: 76px;
  align-items: center;
  gap: 10px;
  padding: 11px 12px;
  color: var(--muted);
  cursor: pointer;
  grid-template-columns: 22px minmax(0, 1fr) 18px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
}

.telegram-access-options > label:hover {
  border-color: var(--border-strong);
}

.telegram-access-options > label.is-selected {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: #9fcfc4;
}

.telegram-access-options input {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  overflow: hidden;
  opacity: 0;
}

.telegram-access-options label:has(input:focus-visible) {
  outline: 3px solid rgb(17 120 100 / 14%);
  outline-offset: 1px;
}

.telegram-access-options label > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.telegram-access-options strong {
  color: var(--text);
  font-size: 12px;
}

.telegram-access-options small {
  color: var(--muted);
  font-size: 10px;
  line-height: 1.35;
}

.telegram-user-select {
  display: grid;
  max-width: 520px;
  gap: 6px;
  margin-top: 12px;
}

.telegram-user-select > small,
.telegram-scope-empty {
  color: var(--muted);
  font-size: 10px;
}

.telegram-options {
  display: block;
  max-height: none;
  padding: 18px 0;
}

.telegram-options .telegram-editor-section__heading {
  margin-bottom: 12px;
}

.telegram-event-options,
.telegram-scope-options {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.telegram-event-options label {
  display: grid;
  min-width: 0;
  min-height: 52px;
  align-items: center;
  grid-template-columns: 32px minmax(0, 1fr) 18px;
  gap: 9px;
  padding: 8px 10px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
  cursor: pointer;
}

.telegram-event-options label:hover,
.telegram-event-options label.is-selected {
  border-color: #9fcfc4;
}

.telegram-event-options label.is-selected {
  background: var(--accent-soft);
}

.telegram-event-options label:has(input:focus-visible) {
  outline: 3px solid rgb(17 120 100 / 14%);
  outline-offset: 1px;
}

.telegram-event-option__icon {
  display: inline-flex;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.telegram-event-options strong {
  min-width: 0;
  overflow: hidden;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.telegram-event-options input {
  width: 16px;
  height: 16px;
  margin: 0;
  accent-color: var(--accent);
}

.telegram-line-scopes {
  max-height: none;
  overflow: visible;
}

.telegram-scope-option {
  position: relative;
  display: grid;
  min-width: 0;
  min-height: 64px;
  align-items: center;
  grid-template-columns: 38px minmax(0, 1fr) 20px;
  gap: 10px;
  padding: 9px 10px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 7px;
  cursor: pointer;
}

.telegram-scope-option:hover {
  border-color: var(--border-strong);
  background: var(--surface-subtle);
}

.telegram-scope-option.is-selected {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.telegram-scope-option.is-readonly {
  cursor: default;
}

.telegram-scope-option.is-readonly:hover {
  background: var(--surface-subtle);
  border-color: var(--accent);
}

.telegram-scope-empty {
  margin: 0;
  padding: 12px;
  background: var(--surface-subtle);
  border: 1px dashed var(--border);
  border-radius: 7px;
  grid-column: 1 / -1;
}

.telegram-scope-option:has(input:focus-visible) {
  outline: 3px solid rgb(17 120 100 / 14%);
  outline-offset: 1px;
}

.telegram-scope-option > input {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  overflow: hidden;
  opacity: 0;
  pointer-events: none;
}

.telegram-scope-option__icon {
  display: inline-flex;
  width: 36px;
  height: 36px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  background: var(--surface-subtle);
  border: 1px solid var(--border);
  border-radius: 50%;
}

.telegram-scope-option__icon.is-all {
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-color: #c8e5de;
}

.telegram-scope-option__copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.telegram-scope-option__copy > strong,
.telegram-scope-option__copy > small {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.telegram-scope-option__copy > strong {
  color: var(--text);
  font-size: 12px;
}

.telegram-scope-option__copy > small {
  color: var(--muted);
  font-size: 11px;
}

.telegram-scope-option__check {
  color: var(--accent-strong);
}

.settings-form-actions {
  margin-top: 0;
}

@media (max-width: 860px) {
  .telegram-settings {
    grid-template-columns: 250px minmax(0, 1fr);
  }

  .telegram-event-options,
  .telegram-access-options,
  .telegram-scope-options {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .telegram-settings {
    min-height: 0;
    grid-template-columns: minmax(0, 1fr);
  }

  .telegram-unit-list {
    max-height: 232px;
    overflow-y: auto;
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .telegram-unit-list__heading {
    position: sticky;
    z-index: 2;
    top: 0;
  }

  .telegram-unit-row {
    min-height: 78px;
  }

  .telegram-unit-editor {
    padding: 12px 0 0;
  }

  .telegram-form-heading {
    min-height: 60px;
  }

  .telegram-bot-identity .settings-form-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .telegram-event-options,
  .telegram-access-options,
  .telegram-scope-options {
    grid-template-columns: minmax(0, 1fr);
  }
}

@container (max-width: 700px) {
  .telegram-settings {
    min-height: 0;
    grid-template-columns: minmax(0, 1fr);
  }

  .telegram-unit-list {
    max-height: 232px;
    overflow-y: auto;
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .telegram-unit-list__heading {
    position: sticky;
    z-index: 2;
    top: 0;
  }

  .telegram-unit-editor {
    padding: 12px 0 0;
  }

  .telegram-bot-identity .settings-form-grid,
  .telegram-event-options,
  .telegram-scope-options {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (max-width: 420px) {
  .telegram-form-heading {
    grid-template-columns: 34px minmax(0, 1fr) auto;
    gap: 8px;
  }

  .telegram-form-heading__icon {
    width: 32px;
    height: 32px;
  }

  .compact-switch {
    gap: 6px;
  }

  .compact-switch > span {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  .settings-form-actions {
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 6px;
  }
}
</style>
