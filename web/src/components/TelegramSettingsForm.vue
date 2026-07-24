<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Check, LoaderCircle, Plus, Save, Trash2 } from '@lucide/vue'
import type { TelegramUnit } from '../api/types'
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

const selectedID = ref('')
const creating = ref(false)
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
const scopeOptions = computed(() => {
  const options = lines.value.map(line => ({
    id: lineKey(line),
    label: lineLabel(line)
  }))
  for (const scope of lineScopes.value) {
    if (!options.some(option => option.id === scope)) {
      options.push({ id: scope, label: `未知线路 · ${scope}` })
    }
  }
  return options
})
const tokenConfigured = computed(() => selectedUnit.value?.token_configured === true)
const validationError = computed(() => {
  if (!displayName.value.trim()) return '请输入名称'
  if (!chatID.value.trim()) return '请输入 Chat ID'
  if (!adminID.value.trim()) return '请输入管理员 ID'
  if (creating.value && !botToken.value.trim()) return '新建 Bot 需要 token'
  if (!allLines.value && lineScopes.value.length === 0) return '请选择至少一条线路'
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
  creating.value = false
  selectedID.value = id
}

function startCreate(): void {
  if (saving.value || deleting.value) return
  creating.value = true
  selectedID.value = ''
  applyUnit()
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
    selectedID.value = unit.id
    applyUnit(unit)
    await nextTick()
    saved.value = true
  } catch (error) {
    saveError.value =
      error instanceof ApiError && error.status === 403
        ? '当前账户无权修改 Telegram Bot'
        : error instanceof Error
          ? error.message
          : '无法保存 Telegram Bot'
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
        ? '当前账户无权删除 Telegram Bot'
        : error instanceof Error
          ? error.message
          : '无法删除 Telegram Bot'
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
    title="正在载入 Telegram Bot"
  />
  <StatePanel
    v-else-if="telegramResource.status === 'forbidden'"
    state="forbidden"
    title="无权查看 Telegram 设置"
    :detail="telegramResource.error"
  />
  <StatePanel
    v-else-if="telegramResource.status === 'error'"
    state="error"
    title="无法载入 Telegram 设置"
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
          title="新建 Bot"
          aria-label="新建 Telegram Bot"
          @click="startCreate"
        >
          <Plus :size="18" />
        </button>
      </header>
      <div v-if="telegramResource.data.length === 0" class="telegram-unit-empty">
        还没有 Telegram Bot
      </div>
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
        title="选择或新建 Telegram Bot"
      />
      <form v-else class="settings-form" @submit.prevent="submit">
        <div class="telegram-form-heading">
          <h3>{{ creating ? '新建 Bot' : 'Bot 设置' }}</h3>
          <label class="compact-switch">
            <span>启用</span>
            <input v-model="enabled" type="checkbox" role="switch" />
          </label>
        </div>

        <div class="settings-form-grid">
          <label class="field">
            <span>名称</span>
            <input v-model="displayName" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>Chat ID</span>
            <input v-model="chatID" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>管理员 ID</span>
            <input v-model="adminID" type="text" autocomplete="off" :disabled="saving || deleting" />
          </label>
          <label class="field">
            <span>Bot token</span>
            <input
              v-model="botToken"
              type="password"
              autocomplete="new-password"
              :placeholder="tokenConfigured ? '已配置' : ''"
              :disabled="saving || deleting"
            />
            <small class="field-status">{{ tokenConfigured ? '已配置' : '未配置' }}</small>
          </label>
        </div>

        <fieldset class="telegram-options">
          <legend>通知</legend>
          <label><input v-model="incomingSMS" type="checkbox" />收到短信</label>
          <label><input v-model="missedCalls" type="checkbox" />未接来电</label>
        </fieldset>

        <fieldset class="telegram-options telegram-line-scopes">
          <legend>线路范围</legend>
          <label>
            <input
              :checked="allLines"
              type="checkbox"
              :disabled="saving || deleting"
              @click.prevent="selectAllLines"
            />
            全部线路
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
            v-if="selectedUnit"
            class="danger-button"
            type="button"
            :disabled="saving || deleting"
            @click="remove"
          >
            <LoaderCircle v-if="deleting" class="spin" :size="16" />
            <Trash2 v-else :size="16" />
            <span>{{ deleteConfirm ? '确认删除' : '删除' }}</span>
          </button>
          <span v-else />

          <div class="settings-form-feedback">
            <p v-if="validationError" class="field-error" role="alert">{{ validationError }}</p>
            <p v-else-if="saveError" class="field-error" role="alert">{{ saveError }}</p>
            <span v-else-if="saved" class="save-status"><Check :size="15" />已保存</span>
          </div>

          <button
            class="primary-button"
            type="submit"
            :disabled="saving || deleting || Boolean(validationError)"
          >
            <LoaderCircle v-if="saving" class="spin" :size="17" />
            <Save v-else :size="17" />
            <span>保存</span>
          </button>
        </footer>
      </form>
    </section>
  </div>
</template>
