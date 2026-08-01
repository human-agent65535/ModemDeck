import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  createMemberPayload,
  createMemberUpdatePayload,
  createTelegramUnitPayload,
  parseUserResponse,
  parseUsersResponse
} from '../src/api/contract.ts'

const source = path => readFile(new URL(path, import.meta.url), 'utf8')

const member = {
  id: 'user_member',
  username: 'member',
  role: 'member',
  enabled: true,
  ios_pairing_enabled: true,
  ios_pairing_has_credential: true,
  ios_pairing_credential_created_at: '2026-07-29T00:30:00Z',
  ios_pairing_paired: true,
  ios_pairing_paired_at: '2026-07-29T00:31:00Z',
  revision: 2,
  profile_name: 'Member Name',
  profile_avatar: 'data:image/png;base64,avatar',
  line_ids: ['line_alpha', 'line_beta'],
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T01:00:00Z'
}

test('user contracts retain role, personal profile, and assigned-line state', () => {
  assert.deepEqual(parseUserResponse({ user: member }), member)
  assert.deepEqual(parseUsersResponse({ users: [member] }), [member])
  assert.deepEqual(
    createMemberPayload({
      username: '  new-member  ',
      password: 'initial password',
      ios_pairing_enabled: true,
      line_ids: [' line_alpha ', 'line_beta']
    }),
    {
      username: 'new-member',
      password: 'initial password',
      ios_pairing_enabled: true,
      line_ids: ['line_alpha', 'line_beta']
    }
  )
  assert.deepEqual(
    createMemberUpdatePayload({
      username: '  renamed  ',
      password: 'new password',
      enabled: false,
      ios_pairing_enabled: false,
      line_ids: ['line_alpha', ' line_beta '],
      revision: 3
    }),
    {
      username: 'renamed',
      password: 'new password',
      enabled: false,
      ios_pairing_enabled: false,
      line_ids: ['line_alpha', 'line_beta'],
      revision: 3
    }
  )
})

test('Telegram bots always belong to a user and select all or a line subset', () => {
  const allAssignedLines = createTelegramUnitPayload({
    display_name: ' Member bot ',
    enabled: true,
    chat_id: ' -100123 ',
    admin_id: ' 42 ',
    assigned_user_id: ' user_member ',
    line_scopes: [],
    incoming_sms: true,
    missed_calls: true
  })
  assert.deepEqual(allAssignedLines, {
    display_name: 'Member bot',
    enabled: true,
    chat_id: '-100123',
    admin_id: '42',
    assigned_user_id: 'user_member',
    line_scopes: [],
    incoming_sms: true,
    missed_calls: true
  })

  const selectedLines = createTelegramUnitPayload({
    display_name: 'Selected bot',
    enabled: true,
    chat_id: '-100124',
    admin_id: '43',
    assigned_user_id: 'user_member',
    line_scopes: [' line_beta '],
    incoming_sms: true,
    missed_calls: false
  })
  assert.deepEqual(selectedLines, {
    display_name: 'Selected bot',
    enabled: true,
    chat_id: '-100124',
    admin_id: '43',
    assigned_user_id: 'user_member',
    line_scopes: ['line_beta'],
    incoming_sms: true,
    missed_calls: false
  })
})

test('multi-user UI exposes only authorized settings and communication areas', async () => {
  const [
    settings,
    users,
    telegram,
    shell,
    router,
    client,
    session,
    runtime,
    english,
    chinese,
    style
  ] =
    await Promise.all([
      source('../src/views/SettingsView.vue'),
      source('../src/components/UserSettingsPanel.vue'),
      source('../src/components/TelegramSettingsForm.vue'),
      source('../src/components/AppShell.vue'),
      source('../src/router/index.ts'),
      source('../src/api/client.ts'),
      source('../src/state/session.ts'),
      source('../src/state/runtimeEvents.ts'),
      source('../src/i18n/locales/en-US.ts'),
      source('../src/i18n/locales/zh-CN.ts'),
      source('../src/style.css')
    ])

  assert.doesNotMatch(settings, /mustChangePassword/)
  assert.match(settings, /id: 'users'/)
  assert.match(settings, /if \(sessionState\.role === 'admin'\)/)
  assert.doesNotMatch(settings, /id: 'system'/)
  assert.doesNotMatch(settings, /id: 'recording'/)
  assert.match(settings, /sessionState\.role === 'admin'/)
  assert.match(settings, /if \(canManageExternalAccess\.value\)/)
  assert.match(settings, /return result/)
  assert.match(
    settings,
    /canPairIOS = computed\(\(\) => sessionState\.iosPairingEnabled\)/
  )
  assert.match(settings, /destination = canManageExternalAccess\.value/)
  assert.doesNotMatch(settings, /externalAccessEnabled|loadExternalAccessVisibility/)
  assert.match(settings, /<UserSettingsPanel/)
  assert.match(users, /gateway\.listUsers\(\)/)
  assert.match(users, /gateway\.createMember/)
  assert.match(users, /gateway\.updateMember/)
  assert.match(users, /gateway\.setMemberPassword/)
  assert.doesNotMatch(users, /password: newPassword\.value/)
  assert.match(users, /@click="openPasswordDialog"/)
  assert.match(users, /minimumPasswordCharacters/)
  assert.match(users, /passwordCharacterCount\(password\.value\)/)
  assert.match(users, /passwordCharacterCount\(resetPassword\.value\)/)
  assert.doesNotMatch(users, /temporaryPassword/)
  assert.doesNotMatch(users, /recordingDefaultEnabled|languageOptions|preferences: \{/)
  assert.doesNotMatch(users, /import AccountProfileSetting from/)
  assert.doesNotMatch(users, /<AccountSettingsPanel/)
  assert.doesNotMatch(users, /class="user-personal-settings"/)
  assert.match(users, /selectedUser\.profile_name \|\| selectedUser\.username/)
  assert.match(users, /selectedUser\?\.profile_name \|\| username/)
  assert.match(users, /const filteredUsers = computed/)
  assert.match(users, /<SettingsMasterDetail/)
  assert.match(users, /class="settings-resource-row user-row/)
  assert.match(
    users,
    /<SettingsResourceStatus[\s\S]*:disabled="!user\.enabled"[\s\S]*:label="t\('users\.disabled'\)"/
  )
  assert.doesNotMatch(users, /class="user-status"/)
  assert.match(users, /:detail-open="mobileDetailOpen"/)
  assert.match(users, /class="user-account-access"/)
  assert.match(
    users,
    /v-if="!creating && selectedUser"[\s\S]*?users\.accountAccess/
  )
  assert.match(
    users,
    /v-model="enabled"[\s\S]*?:disabled="saving \|\| selectedUser\?\.role === 'admin'"/
  )
  assert.match(
    users,
    /v-model="iosPairingEnabled"[\s\S]*?:disabled="saving \|\| selectedUser\?\.role === 'admin'"/
  )
  assert.match(
    users,
    /v-if="creating \|\| selectedUser\?\.role === 'member'"[\s\S]*?:selected-ids="lineIDs"[\s\S]*?:disabled="saving"/
  )
  assert.match(users, /class="user-pairing-state"/)
  assert.match(users, /class="ui-switch"/)
  assert.doesNotMatch(users, /class="user-pairing-status"/)
  assert.doesNotMatch(users, /class="user-account-access__status"/)
  assert.match(
    style,
    /\.ui-switch:checked:disabled\s*\{\s*background: var\(--accent-border-strong\)/
  )
  assert.doesNotMatch(users, /users\.adminUsernameLocked/)
  assert.match(shell, /const settingsUserDetailOpen = computed/)
  assert.match(shell, /query\.newUser === '1'/)
  assert.doesNotMatch(telegram, /scopeSource|manualScope/)
  assert.match(telegram, /const isAdmin = computed/)
  assert.match(telegram, /currentSessionUser/)
  assert.match(telegram, /v-if="isAdmin" class="telegram-editor-section telegram-access-source"/)
  assert.match(telegram, /assigned_user_id: assignedUserID\.value/)
  assert.match(shell, /sessionState\.role === 'admin'/)
  assert.match(shell, /\{ name: 'traffic', label: t\('shell\.traffic'\)/)
  assert.match(shell, /<DialerPanel :permanent="permanentDialer"/)
  assert.doesNotMatch(shell, /accountRestricted|mustChangePassword/)
  assert.doesNotMatch(router, /mustChangePassword/)
  assert.doesNotMatch(router, /to\.name === 'traffic'/)
  assert.match(settings, /id: 'devices'/)
  assert.match(users, /bootstrapResource\.data\?\.line_catalog/)
  assert.doesNotMatch(users, /if \(user\.role === 'admin'\) return t\('users\.allLines'\)/)
  assert.doesNotMatch(client, /must_change_password/)
  assert.match(client, /source\.allowed_line_ids/)
  assert.match(session, /state\.allowedLineIDs = \[\.\.\.\(session\.allowed_line_ids \|\| \[\]\)\]/)
  assert.match(session, /resetRecordingState\(\)/)
  assert.match(runtime, /case 'session':[\s\S]*?await refreshSession\(\)/)
  assert.match(english, /The bot uses this user’s contacts/)
  assert.match(chinese, /Bot 使用该用户的通讯录/)
  assert.doesNotMatch(english, /temporary password|first login/i)
  assert.doesNotMatch(chinese, /临时密码|首次登录/)
})
