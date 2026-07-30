<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  Check,
  KeyRound,
  LoaderCircle,
  Plus,
  Save,
  Search,
  ShieldCheck,
  UserRound
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { gateway } from '../api/client'
import type { LineSummary, UserAccount } from '../api/types'
import { ApiError } from '../api/types'
import { resetNetworkState } from '../state/network'
import { refreshSession, sessionState } from '../state/session'
import {
  bootstrapResource,
  lineKey,
  lineLabel,
  loadBootstrap
} from '../state/workspace'
import BaseAvatar from './BaseAvatar.vue'
import AccountSettingsPanel from './AccountSettingsPanel.vue'
import LineTag from './LineTag.vue'
import StatePanel from './StatePanel.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const users = ref<UserAccount[]>([])
const status = ref<'loading' | 'ready' | 'error'>('loading')
const loadError = ref('')
const selectedID = ref('')
const creating = ref(false)
const username = ref('')
const temporaryPassword = ref('')
const enabled = ref(true)
const lineIDs = ref<string[]>([])
const saving = ref(false)
const saved = ref(false)
const saveError = ref('')
const newPassword = ref('')
const settingPassword = ref(false)
const passwordSet = ref(false)
const searchQuery = ref('')

const lines = computed(
  () => bootstrapResource.data?.line_catalog || bootstrapResource.data?.lines || []
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
const passwordBytes = computed(() => new TextEncoder().encode(temporaryPassword.value).length)
const validationError = computed(() => {
  if (!username.value.trim()) return t('users.enterUsername')
  if (creating.value && passwordBytes.value < 12) {
    return t('users.passwordTooShort', { count: 12 })
  }
  return ''
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
  temporaryPassword.value = ''
  enabled.value = user?.enabled ?? true
  lineIDs.value = [...(user?.line_ids || [])]
  newPassword.value = ''
  saved.value = false
  saveError.value = ''
  passwordSet.value = false
}

function selectUser(id: string): void {
  if (saving.value || settingPassword.value) return
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
  if (saving.value || settingPassword.value) return
  creating.value = true
  selectedID.value = '__new_member__'
  applyUser()
  void router.push({
    name: 'settings',
    params: { section: 'account' },
    query: { ...route.query, user: undefined, newUser: '1' }
  })
}

function toggleLine(id: string, event: Event): void {
  const checked = (event.currentTarget as HTMLInputElement).checked
  lineIDs.value = checked
    ? [...new Set([...lineIDs.value, id])]
    : lineIDs.value.filter(value => value !== id)
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

async function submit(): Promise<void> {
  if (saving.value || settingPassword.value || validationError.value) return
  saving.value = true
  saved.value = false
  saveError.value = ''
  try {
    const user = creating.value
      ? await gateway.createMember({
          username: username.value,
          password: temporaryPassword.value,
          line_ids: lineIDs.value
        })
      : editableUser.value
        ? await gateway.updateMember(editableUser.value.id, {
            username: username.value,
            enabled: enabled.value,
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
    saved.value = true
  } catch (cause) {
    if (cause instanceof ApiError && cause.code === 'username_conflict') {
      saveError.value = t('users.usernameConflict')
    } else {
      saveError.value = cause instanceof Error ? cause.message : t('users.saveFailed')
    }
  } finally {
    saving.value = false
  }
}

async function setMemberPassword(): Promise<void> {
  const user = editableUser.value
  if (!user || settingPassword.value) return
  if (new TextEncoder().encode(newPassword.value).length < 12) {
    saveError.value = t('users.passwordTooShort', { count: 12 })
    return
  }
  settingPassword.value = true
  saved.value = false
  saveError.value = ''
  passwordSet.value = false
  try {
    await gateway.setMemberPassword(user.id, newPassword.value)
    newPassword.value = ''
    passwordSet.value = true
    const loaded = await gateway.listUsers()
    users.value = loaded
    const refreshed = loaded.find(current => current.id === user.id)
    if (refreshed) applyUser(refreshed)
    passwordSet.value = true
  } catch (cause) {
    saveError.value = cause instanceof Error ? cause.message : t('users.setPasswordFailed')
  } finally {
    settingPassword.value = false
  }
}

function syncSelectionFromRoute(): void {
  if (status.value !== 'ready' || saving.value || settingPassword.value) return
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
  [username, enabled, lineIDs, temporaryPassword, newPassword],
  () => {
    if (saving.value || settingPassword.value) return
    saved.value = false
    passwordSet.value = false
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
  <div
    v-else
    class="user-settings"
    :class="{ 'show-mobile-editor': mobileDetailOpen }"
  >
    <aside class="user-list">
      <header>
        <span>
          <strong>{{ t('users.title') }}</strong>
          <small>{{ t('users.count', { count: users.length }) }}</small>
        </span>
        <button
          class="icon-button"
          type="button"
          :title="t('users.newMember')"
          :aria-label="t('users.newMember')"
          @click="startCreate"
        >
          <Plus :size="18" />
        </button>
      </header>
      <label class="user-search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t('users.searchUsers') }}</span>
        <input
          v-model="searchQuery"
          type="search"
          :placeholder="t('users.searchUsers')"
        />
      </label>
      <button
        v-if="creating"
        class="user-row is-selected"
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
        class="user-row"
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
    </aside>

    <section class="user-editor">
      <StatePanel
        v-if="!creating && !selectedUser"
        state="empty"
        :title="t('users.selectUser')"
      />
      <form v-else class="settings-form user-form" @submit.prevent="submit">
        <header class="user-editor__heading">
          <span>
            <ShieldCheck v-if="selectedUser?.role === 'admin'" :size="19" />
            <UserRound v-else :size="19" />
          </span>
          <div>
            <h3>{{ creating ? t('users.newMember') : username }}</h3>
            <small>
              {{
                selectedUser?.role === 'admin'
                  ? t('users.initialAdminDescription')
                  : selectedUser?.profile_name
                    ? `${selectedUser.profile_name} · ${t('users.memberRole')}`
                    : t('users.memberRole')
              }}
            </small>
          </div>
        </header>

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
            <span>{{ t('users.temporaryPassword') }}</span>
            <input
              v-model="temporaryPassword"
              type="password"
              autocomplete="new-password"
              :disabled="saving"
            />
            <small>{{ t('users.passwordHint', { count: 12 }) }}</small>
          </label>
        </div>

        <label
          v-if="!creating && selectedUser?.role === 'member'"
          class="user-account-access"
        >
          <span>
            <strong>{{ t('users.accountAccess') }}</strong>
            <small>{{ t('users.accountAccessDescription') }}</small>
          </span>
          <span class="user-account-access__status">
            {{ enabled ? t('users.enabled') : t('users.disabled') }}
          </span>
          <input v-model="enabled" type="checkbox" role="switch" :disabled="saving" />
        </label>

        <fieldset class="user-lines">
          <legend>{{ t('users.assignedLines') }}</legend>
          <p>
            {{
              selectedUser?.role === 'admin'
                ? t('users.initialAdminDescription')
                : t('users.assignedLinesDescription')
            }}
          </p>
          <div class="user-line-options">
            <label
              v-for="line in lines"
              :key="lineKey(line)"
              :class="{ 'is-selected': lineIDs.includes(lineKey(line)) }"
            >
              <input
                :checked="lineIDs.includes(lineKey(line))"
                type="checkbox"
                :disabled="saving"
                @change="toggleLine(lineKey(line), $event)"
              />
              <LineTag :line="line" :fallback="lineLabel(line)" />
              <small>{{ line.phone_number || t('lines.cellularLine') }}</small>
              <Check v-if="lineIDs.includes(lineKey(line))" :size="16" />
            </label>
          </div>
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
                :disabled="settingPassword || saving"
              />
            </label>
            <button
              class="secondary-button"
              type="button"
              :disabled="settingPassword || saving || !newPassword"
              @click="setMemberPassword"
            >
              <LoaderCircle v-if="settingPassword" class="spin" :size="16" />
              <KeyRound v-else :size="16" />
              {{ t('users.setPassword') }}
            </button>
          </div>
        </section>

        <footer class="settings-form-actions">
          <span class="user-feedback">
            <span v-if="validationError" class="field-error">{{ validationError }}</span>
            <span v-else-if="saveError" class="field-error">{{ saveError }}</span>
            <span v-else-if="saved" class="save-status">
              <Check :size="15" /> {{ t('users.saved') }}
            </span>
            <span v-else-if="passwordSet" class="save-status">
              <Check :size="15" /> {{ t('users.passwordSet') }}
            </span>
          </span>
          <button
            class="primary-button"
            type="submit"
            :disabled="saving || Boolean(validationError)"
          >
            <LoaderCircle v-if="saving" class="spin" :size="17" />
            <Save v-else :size="17" />
            {{ creating ? t('users.createMember') : t('common.save') }}
          </button>
        </footer>
      </form>

      <AccountSettingsPanel
        v-if="!creating && selectedUser?.id === sessionState.userID"
        @profile-saved="load"
      />
    </section>
  </div>
</template>

<style scoped>
.user-settings {
  display: grid;
  min-height: 560px;
  grid-template-columns: clamp(200px, 25%, 240px) minmax(0, 1fr);
  border-top: 1px solid var(--border);
}

.user-list {
  min-width: 0;
  border-right: 1px solid var(--border);
}

.user-list > header {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
}

.user-list > header > span,
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

.user-list small,
.user-editor small {
  overflow: hidden;
  color: var(--muted);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-row {
  display: grid;
  width: 100%;
  min-height: 64px;
  align-items: center;
  gap: 9px;
  padding: 9px 10px;
  text-align: left;
  grid-template-columns: auto minmax(0, 1fr) auto;
  border-bottom: 1px solid var(--border);
}

.user-row:hover,
.user-row.is-selected {
  background: var(--surface-hover);
}

.user-row.is-selected {
  box-shadow: inset 3px 0 0 var(--accent);
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
  padding-left: 24px;
}

.user-editor__heading {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--border);
}

.user-editor__heading > span,
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

.user-editor__heading > div {
  min-width: 0;
  flex: 1;
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

.user-fields {
  display: grid;
  gap: 16px;
  padding: 18px 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.user-fields .field small {
  margin-top: 4px;
}

.user-account-access {
  display: grid;
  min-height: 64px;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
  grid-template-columns: minmax(0, 1fr) auto auto;
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

.user-account-access__status {
  color: var(--muted);
  font-size: 11px;
  font-weight: 650;
}

.user-account-access input {
  position: relative;
  width: 42px;
  height: 24px;
  appearance: none;
  background: #d8dde2;
  border-radius: 12px;
  cursor: pointer;
}

.user-account-access input::before {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  content: "";
  background: #ffffff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(16 24 40 / 20%);
  transition: transform 150ms ease;
}

.user-account-access input:checked {
  background: var(--accent);
}

.user-account-access input:checked::before {
  transform: translateX(18px);
}

.user-account-access input:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.user-lines {
  padding: 16px 0;
  border: 0;
  border-top: 1px solid var(--border);
}

.user-lines legend {
  color: var(--text);
  font-size: 12px;
  font-weight: 700;
}

.user-lines > p,
.user-admin-summary p {
  margin: 3px 0 12px;
  color: var(--muted);
  font-size: 11px;
}

.user-line-options {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.user-line-options label {
  display: grid;
  min-height: 54px;
  align-items: center;
  gap: 7px;
  padding: 8px 10px;
  grid-template-columns: auto minmax(0, 1fr) auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  cursor: pointer;
}

.user-line-options label.is-selected {
  background: var(--surface-selected);
  border-color: #aed8cf;
}

.user-line-options input {
  position: absolute;
  opacity: 0;
}

.user-line-options small {
  grid-row: 2;
  grid-column: 2 / -1;
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
  align-items: end;
  gap: 10px;
  grid-template-columns: minmax(0, 1fr) auto;
}

.user-password-set .secondary-button {
  min-height: 40px;
}

.user-feedback {
  min-width: 0;
}

.user-editor :deep(.account-settings-panel) {
  max-width: none;
  margin-top: 26px;
  padding-top: 26px;
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
    padding-left: 18px;
  }

  .user-fields,
  .user-line-options {
    grid-template-columns: 1fr;
  }

  .user-password-set > div {
    align-items: stretch;
    grid-template-columns: 1fr;
  }

  .user-account-access {
    align-items: start;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .user-account-access input {
    grid-row: 1 / span 2;
    grid-column: 2;
  }

  .user-account-access__status {
    grid-row: 2;
    grid-column: 1;
  }
}

@media (max-width: 760px) {
  .user-settings {
    min-height: 0;
    grid-template-columns: 1fr;
  }

  .user-list {
    max-height: none;
    border-right: 0;
    border-bottom: 0;
  }

  .user-settings:not(.show-mobile-editor) .user-editor {
    display: none;
  }

  .user-settings.show-mobile-editor .user-list {
    display: none;
  }

  .user-editor {
    padding: 12px 0 0;
  }

  .user-fields,
  .user-line-options {
    grid-template-columns: 1fr;
  }

  .user-password-set > div {
    align-items: stretch;
    grid-template-columns: 1fr;
  }

  .user-account-access {
    align-items: start;
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .user-account-access input {
    grid-row: 1 / span 2;
    grid-column: 2;
  }

  .user-account-access__status {
    grid-row: 2;
    grid-column: 1;
  }
}
</style>
