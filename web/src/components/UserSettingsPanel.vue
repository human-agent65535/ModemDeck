<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  KeyRound,
  LoaderCircle,
  Plus,
  Save,
  Search,
  ShieldCheck,
  Trash2,
  UserRound
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { gateway } from '../api/client'
import type { LineSummary, UserAccount } from '../api/types'
import { ApiError } from '../api/types'
import { useSettingsMutation } from '../composables/useSettingsMutation'
import { requestConfirmation } from '../state/confirmation'
import { showError, showSuccess } from '../state/feedback'
import { resetNetworkState } from '../state/network'
import { refreshSession, sessionState } from '../state/session'
import {
  bootstrapResource,
  lineKey,
  lineLabel,
  loadBootstrap
} from '../state/workspace'
import {
  minimumPasswordCharacters,
  passwordCharacterCount
} from '../utils/password'
import { formatDateTime } from '../utils/format'
import BaseAvatar from './BaseAvatar.vue'
import AccountProfileSetting from './AccountProfileSetting.vue'
import AccountSettingsPanel from './AccountSettingsPanel.vue'
import StatePanel from './StatePanel.vue'
import SystemSettingsForm from './SystemSettingsForm.vue'
import SettingsLineScopeList from './settings/SettingsLineScopeList.vue'
import SettingsMasterDetail from './settings/SettingsMasterDetail.vue'
import SettingsSaveStatus from './settings/SettingsSaveStatus.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const users = ref<UserAccount[]>([])
const status = ref<'loading' | 'ready' | 'error'>('loading')
const loadError = ref('')
const selectedID = ref('')
const creating = ref(false)
const username = ref('')
const password = ref('')
const enabled = ref(true)
const iosPairingEnabled = ref(false)
const lineIDs = ref<string[]>([])
const saving = ref(false)
const pairingRevoking = ref(false)
const saveError = ref('')
const newPassword = ref('')
const searchQuery = ref('')
const lineMutation = useSettingsMutation({
  errorMessage: cause =>
    cause instanceof Error ? cause.message : t('users.saveFailed')
})
const lineSaving = lineMutation.saving
const lineSaveError = lineMutation.error

const lines = computed(
  () => bootstrapResource.data?.line_catalog || bootstrapResource.data?.lines || []
)
const lineScopeOptions = computed(() =>
  lines.value.map(line => ({
    id: lineKey(line),
    label: lineLabel(line),
    details: line.phone_number || t('lines.cellularLine'),
    line
  }))
)
const normalizedSearch = computed(() => searchQuery.value.trim().toLocaleLowerCase())
const filteredUsers = computed(() => {
  if (!normalizedSearch.value) return users.value
  return users.value.filter(user => {
    const lineSummary = userLineSummary(user).toLocaleLowerCase()
    return (
      user.username.toLocaleLowerCase().includes(normalizedSearch.value) ||
      (user.profile_name || '').toLocaleLowerCase().includes(normalizedSearch.value) ||
      lineSummary.includes(normalizedSearch.value)
    )
  })
})
const mobileDetailOpen = computed(
  () => typeof route.query.user === 'string' || route.query.newUser === '1'
)
const selectedUser = computed(() =>
  users.value.find(user => user.id === selectedID.value)
)
const editableUser = computed(() => selectedUser.value)
const validationError = computed(() => {
  if (!username.value.trim()) return t('users.enterUsername')
  if (
    creating.value &&
    passwordCharacterCount(password.value) < minimumPasswordCharacters
  ) {
    return t('users.passwordTooShort', { count: minimumPasswordCharacters })
  }
  if (
    !creating.value &&
    newPassword.value &&
    passwordCharacterCount(newPassword.value) < minimumPasswordCharacters
  ) {
    return t('users.passwordTooShort', { count: minimumPasswordCharacters })
  }
  return ''
})

const formChanged = computed(() => {
  if (creating.value) return true
  const user = editableUser.value
  if (!user) return false
  const currentLines = [...lineIDs.value].sort()
  const savedLines = [...user.line_ids].sort()
  return (
    username.value.trim() !== user.username ||
    enabled.value !== user.enabled ||
    iosPairingEnabled.value !== user.ios_pairing_enabled ||
    currentLines.length !== savedLines.length ||
    currentLines.some((id, index) => id !== savedLines[index]) ||
    Boolean(newPassword.value)
  )
})

function lineForID(id: string): LineSummary | undefined {
  return lines.value.find(line => lineKey(line) === id)
}

function userLineSummary(user: UserAccount): string {
  if (user.line_ids.length === 0) return t('users.noLines')
  return user.line_ids
    .map(id => {
      const line = lineForID(id)
      return line ? lineLabel(line) : t('users.unknownLine')
    })
    .join(t('common.listSeparator'))
}

function applyUser(user?: UserAccount): void {
  username.value = user?.username || ''
  password.value = ''
  enabled.value = user?.enabled ?? true
  iosPairingEnabled.value = user?.ios_pairing_enabled ?? false
  lineIDs.value = [...(user?.line_ids || [])]
  newPassword.value = ''
  saveError.value = ''
  lineMutation.reset()
}

function selectUser(id: string): void {
  if (saving.value || lineSaving.value || pairingRevoking.value) return
  creating.value = false
  selectedID.value = id
  applyUser(users.value.find(user => user.id === id))
  void router.push({
    name: 'settings',
    params: { section: 'account' },
    query: { ...route.query, user: id, newUser: undefined }
  })
}

function startCreate(): void {
  if (saving.value || lineSaving.value || pairingRevoking.value) return
  creating.value = true
  selectedID.value = '__new_member__'
  applyUser()
  void router.push({
    name: 'settings',
    params: { section: 'account' },
    query: { ...route.query, user: undefined, newUser: '1' }
  })
}

async function toggleLine(id: string, checked: boolean): Promise<void> {
  if (saving.value || lineSaving.value) return

  const previous = [...lineIDs.value]
  const next = checked
    ? [...new Set([...previous, id])]
    : previous.filter(value => value !== id)
  lineIDs.value = next
  lineMutation.reset()
  if (creating.value) return

  const user = editableUser.value
  if (!user) return

  const result = await lineMutation.run(async () => {
    const updated = await gateway.updateMember(user.id, {
      username: user.username,
      enabled: user.enabled,
      ios_pairing_enabled: user.ios_pairing_enabled,
      line_ids: next,
      revision: user.revision
    })
    users.value = users.value.map(current =>
      current.id === updated.id ? updated : current
    )
    lineIDs.value = [...updated.line_ids]
    if (updated.id === sessionState.userID) {
      await refreshSession()
      resetNetworkState()
      await loadBootstrap(true)
    }
    return updated
  })
  if (!result.ok) {
    lineIDs.value = previous
  }
}

async function load(): Promise<void> {
  status.value = 'loading'
  loadError.value = ''
  try {
    const [loaded] = await Promise.all([gateway.listUsers(), loadBootstrap()])
    users.value = loaded
    status.value = 'ready'
    if (route.query.newUser === '1') {
      creating.value = true
      selectedID.value = '__new_member__'
      applyUser()
      return
    }
    const requestedID =
      typeof route.query.user === 'string' ? route.query.user : ''
    const current =
      loaded.find(user => user.id === requestedID) ||
      loaded.find(user => user.id === selectedID.value) ||
      loaded.find(user => user.id === sessionState.userID) ||
      loaded[0]
    if (current) {
      selectedID.value = current.id
      applyUser(current)
    }
  } catch (cause) {
    status.value = 'error'
    loadError.value = cause instanceof Error ? cause.message : t('users.loadFailed')
  }
}

async function refreshUserList(): Promise<void> {
  try {
    users.value = await gateway.listUsers()
  } catch {
    // The profile save already succeeded; keep the current editor stable if
    // the non-critical list refresh fails.
  }
}

async function submit(): Promise<void> {
  if (
    saving.value ||
    lineSaving.value ||
    pairingRevoking.value ||
    validationError.value
  ) {
    return
  }
  saving.value = true
  saveError.value = ''
  try {
    const user = creating.value
      ? await gateway.createMember({
          username: username.value,
          password: password.value,
          ios_pairing_enabled: iosPairingEnabled.value,
          line_ids: lineIDs.value
        })
      : editableUser.value
        ? await gateway.updateMember(editableUser.value.id, {
            username: username.value,
            ...(newPassword.value ? { password: newPassword.value } : {}),
            enabled: enabled.value,
            ios_pairing_enabled: iosPairingEnabled.value,
            line_ids: lineIDs.value,
            revision: editableUser.value.revision
          })
        : undefined
    if (!user) return
    users.value = users.value
      .filter(current => current.id !== user.id)
      .concat(user)
      .sort((left, right) => {
        if (left.role !== right.role) return left.role === 'admin' ? -1 : 1
        return left.username.localeCompare(right.username)
      })
    creating.value = false
    selectedID.value = user.id
    applyUser(user)
    await router.replace({
      name: 'settings',
      params: { section: 'account' },
      query: { ...route.query, user: user.id, newUser: undefined }
    })
    if (user.id === sessionState.userID) {
      await refreshSession()
      resetNetworkState()
      await loadBootstrap(true)
    }
    showSuccess(t('users.saved'))
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'username_conflict') {
      saveError.value = t('users.usernameConflict')
    } else if (cause instanceof ApiError && cause.code === 'password_too_short') {
      saveError.value = t('users.passwordTooShort', {
        count: minimumPasswordCharacters
      })
    } else {
      saveError.value = cause instanceof Error ? cause.message : t('users.saveFailed')
    }
    showError(saveError.value)
  } finally {
    saving.value = false
  }
}

async function revokeSelectedPairing(): Promise<void> {
  const user = selectedUser.value
  if (!user?.ios_pairing_has_credential || pairingRevoking.value) return
  const confirmed = await requestConfirmation({
    title: t('iosPairing.revokeTitle'),
    message: t('iosPairing.revokeMessage'),
    confirmLabel: t('iosPairing.revokeConfirm'),
    tone: 'danger'
  })
  if (!confirmed) return

  pairingRevoking.value = true
  try {
    await gateway.revokeUserIOSPairing(user.id)
    users.value = users.value.map(current =>
      current.id === user.id
        ? {
            ...current,
            ios_pairing_has_credential: false,
            ios_pairing_credential_created_at: undefined,
            ios_pairing_paired: false,
            ios_pairing_paired_at: undefined
          }
        : current
    )
    showSuccess(
      `${t('users.iosPairingAccess')}: ${t('iosPairing.notPaired')}`
    )
  } catch (cause) {
    showError(
      cause instanceof Error ? cause.message : t('iosPairing.revokeFailed')
    )
  } finally {
    pairingRevoking.value = false
  }
}

function syncSelectionFromRoute(): void {
  if (
    status.value !== 'ready' ||
    saving.value ||
    lineSaving.value ||
    pairingRevoking.value
  ) {
    return
  }
  if (route.query.newUser === '1') {
    if (!creating.value) {
      creating.value = true
      selectedID.value = '__new_member__'
      applyUser()
    }
    return
  }
  if (typeof route.query.user !== 'string') return
  const user = users.value.find(candidate => candidate.id === route.query.user)
  if (!user || (!creating.value && selectedID.value === user.id)) return
  creating.value = false
  selectedID.value = user.id
  applyUser(user)
}

watch(
  [username, enabled, iosPairingEnabled, lineIDs, password, newPassword],
  () => {
    if (saving.value) return
    saveError.value = ''
  },
  { deep: true, flush: 'sync' }
)

watch(
  [() => route.query.user, () => route.query.newUser],
  syncSelectionFromRoute
)

onMounted(() => {
  void load()
})

</script>

<template>
  <StatePanel
    v-if="status === 'loading'"
    state="loading"
    :title="t('users.loading')"
  />
  <StatePanel
    v-else-if="status === 'error'"
    state="error"
    :title="t('users.loadFailed')"
    :detail="loadError"
    retryable
    @retry="load"
  />
  <SettingsMasterDetail
    v-else
    :label="t('users.title')"
    :sidebar-title="t('users.title')"
    :sidebar-description="t('users.count', { count: users.length })"
    :detail-open="mobileDetailOpen"
    :detail-key="creating ? '__new_member__' : selectedID"
  >
    <template #sidebar-action>
      <button
        class="icon-button"
        type="button"
        :title="t('users.newMember')"
        :aria-label="t('users.newMember')"
        @click="startCreate"
      >
        <Plus :size="18" />
      </button>
    </template>
    <template #sidebar-toolbar>
      <label class="user-search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t('users.searchUsers') }}</span>
        <input
          v-model="searchQuery"
          type="search"
          :placeholder="t('users.searchUsers')"
        />
      </label>
    </template>
    <template #sidebar>
      <button
        v-if="creating"
        class="settings-resource-row user-row is-selected"
        type="button"
      >
        <BaseAvatar
          :name="username || t('users.newMember')"
          size="small"
          fallback="person"
        />
        <span>
          <strong>{{ username || t('users.newMember') }}</strong>
          <small>{{ t('users.unsaved') }}</small>
        </span>
      </button>
      <button
        v-for="user in filteredUsers"
        :key="user.id"
        class="settings-resource-row user-row"
        :class="{ 'is-selected': !creating && selectedID === user.id }"
        type="button"
        @click="selectUser(user.id)"
      >
        <BaseAvatar
          :name="user.profile_name || user.username"
          :src="user.profile_avatar"
          size="small"
          :fallback="user.profile_name ? 'initials' : 'person'"
          :palette-key="user.id"
        />
        <span>
          <span class="user-row__name">
            <strong>{{ user.username }}</strong>
            <ShieldCheck v-if="user.role === 'admin'" :size="13" />
          </span>
          <small>{{ userLineSummary(user) }}</small>
        </span>
        <span
          class="user-status"
          :class="{ 'is-disabled': !user.enabled }"
          :title="user.enabled ? t('users.enabled') : t('users.disabled')"
        />
      </button>
      <StatePanel
        v-if="!creating && filteredUsers.length === 0"
        state="empty"
        :title="t('users.noMatchingUsers')"
      />
    </template>

    <section class="user-editor">
      <StatePanel
        v-if="!creating && !selectedUser"
        state="empty"
        :title="t('users.selectUser')"
      />
      <form v-else class="settings-form user-form" @submit.prevent="submit">
        <section class="user-management-card">
          <header class="user-editor__heading">
            <BaseAvatar
              v-if="!creating && selectedUser"
              class="user-editor__avatar"
              :name="selectedUser.profile_name || selectedUser.username"
              :src="selectedUser.profile_avatar"
              size="small"
              :fallback="selectedUser.profile_name ? 'initials' : 'person'"
              :palette-key="selectedUser.id"
            />
            <span v-else class="user-editor__icon">
              <UserRound :size="19" />
            </span>
            <div>
              <h3>
                {{
                  creating
                    ? t('users.newMember')
                    : selectedUser?.profile_name || username
                }}
              </h3>
              <small>
                {{
                  selectedUser?.role === 'admin'
                    ? selectedUser.profile_name
                      ? `@${username} · ${t('users.initialAdminDescription')}`
                      : t('users.initialAdminDescription')
                    : selectedUser?.profile_name
                      ? `@${username} · ${t('users.memberRole')}`
                      : t('users.memberRole')
                }}
              </small>
            </div>
            <span v-if="!creating" class="user-editor__role">
              {{
                selectedUser?.role === 'admin'
                  ? t('account.administrator')
                  : t('account.member')
              }}
            </span>
          </header>

          <div class="user-management-card__body">
            <AccountProfileSetting
              v-if="!creating && selectedUser?.id === sessionState.userID"
              @saved="refreshUserList"
            />

            <div class="user-fields">
              <label class="field">
                <span>{{ t('common.username') }}</span>
                <input
                  v-model="username"
                  autocomplete="off"
                  :disabled="saving || selectedUser?.role === 'admin'"
                />
                <small v-if="selectedUser?.role === 'admin'">
                  {{ t('users.adminUsernameLocked') }}
                </small>
              </label>
              <label v-if="creating" class="field">
                <span>{{ t('auth.password') }}</span>
                <input
                  v-model="password"
                  type="password"
                  autocomplete="new-password"
                  :disabled="saving"
                />
                <small>
                  {{
                    t('users.passwordHint', {
                      count: minimumPasswordCharacters
                    })
                  }}
                </small>
              </label>
            </div>

            <SystemSettingsForm
              v-if="!creating && selectedUser?.id === sessionState.userID"
              class="user-system-language"
            />

            <section
              v-if="!creating && selectedUser"
              class="user-account-access"
              aria-labelledby="user-account-access-title"
            >
              <span>
                <strong id="user-account-access-title">
                  {{ t('users.accountAccess') }}
                </strong>
                <small>{{ t('users.accountAccessDescription') }}</small>
              </span>
              <label class="user-account-access__control">
                <input
                  class="ui-switch"
                  v-model="enabled"
                  type="checkbox"
                  role="switch"
                  :aria-label="t('users.accountAccess')"
                  :disabled="saving || selectedUser?.role === 'admin'"
                />
              </label>
            </section>

            <section
              v-if="creating || selectedUser"
              class="user-account-access"
              aria-labelledby="user-ios-pairing-access-title"
            >
              <span>
                <span class="user-account-access__title">
                  <strong id="user-ios-pairing-access-title">
                    {{ t('users.iosPairingAccess') }}
                  </strong>
                  <span
                    v-if="!creating && selectedUser"
                    class="user-pairing-state"
                    :class="{
                      'is-paired': selectedUser.ios_pairing_paired
                    }"
                  >
                    {{
                      selectedUser.ios_pairing_paired
                        ? t('iosPairing.paired')
                        : selectedUser.ios_pairing_has_credential
                          ? t('iosPairing.waiting')
                        : t('iosPairing.notPaired')
                    }}
                    <template
                      v-if="selectedUser.ios_pairing_credential_created_at"
                    >
                      · {{ t('iosPairing.createdAt') }}
                      {{
                        formatDateTime(
                          selectedUser.ios_pairing_credential_created_at
                        )
                      }}
                    </template>
                  </span>
                  <button
                    v-if="selectedUser?.ios_pairing_has_credential"
                    class="danger-button user-pairing-revoke"
                    type="button"
                    :disabled="saving || pairingRevoking"
                    @click="revokeSelectedPairing"
                  >
                    <LoaderCircle
                      v-if="pairingRevoking"
                      class="spin"
                      :size="14"
                    />
                    <Trash2 v-else :size="14" />
                    {{ t('iosPairing.revoke') }}
                  </button>
                </span>
                <small>{{ t('users.iosPairingAccessDescription') }}</small>
              </span>
              <label class="user-account-access__control">
                <input
                  class="ui-switch"
                  v-model="iosPairingEnabled"
                  type="checkbox"
                  role="switch"
                  :aria-label="t('users.iosPairingAccess')"
                  :disabled="saving || selectedUser?.role === 'admin'"
                />
              </label>
            </section>

            <fieldset class="user-lines">
              <legend class="sr-only">{{ t('users.assignedLines') }}</legend>
              <div class="user-lines__heading">
                <strong>{{ t('users.assignedLines') }}</strong>
                <SettingsSaveStatus
                  :status="lineMutation.status.value"
                  :error="lineMutation.error.value"
                />
              </div>
              <p>
                {{
                  selectedUser?.role === 'admin'
                    ? t('account.administratorRole')
                    : t('users.assignedLinesDescription')
                }}
              </p>
              <SettingsLineScopeList
                :options="lineScopeOptions"
                :selected-ids="lineIDs"
                :disabled="saving || lineSaving"
                @toggle-line="toggleLine"
              />
              <p v-if="lineSaveError" class="field-error" role="alert">
                {{ lineSaveError }}
              </p>
            </fieldset>

            <section
              v-if="!creating && selectedUser?.role === 'member'"
              class="user-password-set"
            >
              <header>
                <KeyRound :size="17" />
                <div>
                  <h4>{{ t('users.setPassword') }}</h4>
                </div>
              </header>
              <div>
                <label class="field">
                  <span>{{ t('users.newPassword') }}</span>
                  <input
                    v-model="newPassword"
                    type="password"
                    autocomplete="new-password"
                    :disabled="saving"
                  />
                  <small>
                    {{
                      t('users.optionalPasswordHint', {
                        count: minimumPasswordCharacters
                      })
                    }}
                  </small>
                </label>
              </div>
            </section>

            <footer
              v-if="creating || selectedUser?.role === 'member'"
              class="settings-form-actions"
            >
              <span class="user-feedback">
                <span v-if="validationError" class="field-error">{{ validationError }}</span>
                <span v-else-if="saveError" class="field-error">{{ saveError }}</span>
              </span>
              <button
                class="primary-button"
                type="submit"
                :disabled="
                  saving ||
                  lineSaving ||
                  pairingRevoking ||
                  Boolean(validationError) ||
                  (!creating && !formChanged)
                "
              >
                <LoaderCircle v-if="saving" class="spin" :size="17" />
                <Save v-else :size="17" />
                {{ creating ? t('users.createMember') : t('common.save') }}
              </button>
            </footer>
          </div>
        </section>
      </form>

      <AccountSettingsPanel
        v-if="!creating && selectedUser?.id === sessionState.userID"
        :show-identity="false"
        :show-profile="false"
        :show-language="false"
        @profile-saved="refreshUserList"
      />
    </section>
  </SettingsMasterDetail>
</template>

<style scoped>
.user-row > span:nth-child(2) {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}

.user-search {
  display: flex;
  height: 44px;
  align-items: center;
  gap: 7px;
  padding: 6px 10px;
  color: var(--muted);
  border-bottom: 1px solid var(--border);
}

.user-search input {
  width: 100%;
  min-width: 0;
  height: 32px;
  padding: 0;
  color: var(--text);
  font-size: 12px;
  background: transparent;
  border: 0;
  outline: 0;
}

.user-search:focus-within {
  color: var(--accent-strong);
  box-shadow: inset 3px 0 0 var(--accent);
}

.user-row small,
.user-editor small {
  overflow: hidden;
  color: var(--muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-row {
  display: grid;
  align-items: center;
  gap: 9px;
  padding: 9px 10px;
  grid-template-columns: auto minmax(0, 1fr) auto;
}

.user-row__name {
  display: flex;
  align-items: center;
  gap: 5px;
}

.user-row__name svg {
  color: var(--accent-strong);
}

.user-status {
  width: 8px;
  height: 8px;
  background: var(--success);
  border-radius: 50%;
}

.user-status.is-disabled {
  background: var(--faint);
}

.user-editor {
  min-width: 0;
}

.user-form {
  width: 100%;
  max-width: 760px;
}

.user-management-card {
  background: transparent;
}

.user-editor__heading {
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--border);
}

.user-editor__icon,
.user-admin-summary > span {
  display: grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  color: var(--accent-strong);
  background: var(--accent-soft);
  border-radius: 50%;
}

.user-editor__avatar {
  flex: 0 0 auto;
}

.user-editor__heading > div {
  min-width: 0;
  flex: 1;
}

.user-editor__role {
  flex: 0 0 auto;
  padding: 5px 9px;
  color: var(--accent-strong);
  font-size: 10px;
  font-weight: 750;
  background: var(--accent-soft);
  border-radius: 999px;
}

.user-editor__heading small {
  display: block;
  overflow: visible;
  text-overflow: clip;
  white-space: normal;
}

.user-editor h3,
.user-editor h4 {
  margin: 0;
  color: var(--text);
  font-size: 13px;
  text-transform: none;
}

.user-editor__heading h3 {
  font-size: 16px;
}

.user-management-card__body {
  padding-bottom: 4px;
}

.user-fields {
  display: grid;
  gap: 16px;
  padding: 18px 0;
  grid-template-columns: minmax(0, 520px);
}

.user-fields .field small {
  margin-top: 4px;
}

.user-system-language {
  max-width: none;
  padding-bottom: 12px;
  border-bottom: 0;
}

.user-account-access {
  display: grid;
  min-height: 64px;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
  grid-template-columns: minmax(0, 1fr) auto;
  border-top: 1px solid var(--border);
}

.user-account-access > span:first-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.user-account-access small {
  white-space: normal;
}

.user-account-access__title {
  display: flex;
  min-width: 0;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px 7px;
}

.user-account-access__control {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}

.user-account-access__control:has(input:disabled) {
  cursor: not-allowed;
}

.user-pairing-state {
  display: inline-flex;
  align-items: center;
  padding: 2px 7px;
  color: var(--muted-strong);
  font-size: 10px;
  font-weight: 650;
  line-height: 1.4;
  background: var(--surface-hover);
  border-radius: 999px;
}

.user-pairing-state.is-paired {
  color: var(--success);
  background: var(--success-soft);
}

.user-pairing-revoke {
  min-height: 26px;
  padding: 0 7px;
  font-size: 10px;
}

.user-lines {
  padding: 16px 0;
  border: 0;
  border-top: 1px solid var(--border);
}

.user-lines__heading {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--text);
  font-size: 12px;
  font-weight: 700;
}

.user-lines__state {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--accent-strong);
  font-size: 10px;
  font-weight: 700;
}

.user-lines > p,
.user-admin-summary p {
  margin: 3px 0 12px;
  color: var(--muted);
  font-size: 11px;
}

.user-lines > .field-error {
  margin-top: 10px;
  color: var(--danger);
}

.user-default-line {
  display: block;
  max-width: 420px;
  padding: 16px 0;
  border-top: 1px solid var(--border);
}

.user-password-set {
  padding: 16px 0;
  border-top: 1px solid var(--border);
}

.user-password-set > header {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.user-password-set > header svg {
  color: var(--accent-strong);
}

.user-password-set > div {
  display: grid;
  max-width: 560px;
  grid-template-columns: minmax(0, 1fr);
}

.user-feedback {
  min-width: 0;
}

.user-editor :deep(.account-settings-panel) {
  max-width: 760px;
  margin-top: 28px;
  padding-top: 28px;
  border-top: 1px solid var(--border);
}

.user-admin-summary {
  display: flex;
  max-width: 620px;
  align-items: flex-start;
  gap: 12px;
  padding: 28px 0;
}

.user-admin-summary strong {
  color: var(--accent-strong);
  font-size: 12px;
}

@container (max-width: 720px) {
  .user-editor {
    padding-inline: 18px;
  }

  .user-password-set > div {
    align-items: stretch;
    grid-template-columns: 1fr;
  }

  .user-account-access {
    align-items: start;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .user-account-access__control {
    padding-top: 1px;
  }
}

@media (max-width: 860px) {
  .user-password-set > div {
    align-items: stretch;
    grid-template-columns: 1fr;
  }

  .user-account-access {
    align-items: start;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .user-account-access__control {
    padding-top: 1px;
  }
}
</style>
