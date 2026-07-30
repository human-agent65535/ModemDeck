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
  must_change_password: true,
  revision: 2,
  profile_contact_id: 'contact-member',
  profile_name: 'Member Name',
  profile_avatar: 'data:image/png;base64,avatar',
  default_line_id: 'line_beta',
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
      password: 'temporary password',
      line_ids: [' line_alpha ', 'line_beta']
    }),
    {
      username: 'new-member',
      password: 'temporary password',
      line_ids: ['line_alpha', 'line_beta']
    }
  )
  assert.deepEqual(
    createMemberUpdatePayload({
      username: '  renamed  ',
      enabled: false,
      line_ids: ['line_alpha', ' line_beta '],
      revision: 3
    }),
    {
      username: 'renamed',
      enabled: false,
      line_ids: ['line_alpha', 'line_beta'],
      revision: 3
    }
  )
})

test('Telegram user mode inherits a user while manual mode carries only line access', () => {
  const userMode = createTelegramUnitPayload({
    display_name: ' Member bot ',
    enabled: true,
    chat_id: ' -100123 ',
    admin_id: ' 42 ',
    scope_source: 'user',
    assigned_user_id: ' user_member ',
    manual_all_lines: true,
    line_scopes: ['stale', 'stale'],
    incoming_sms: true,
    missed_calls: true
  })
  assert.deepEqual(userMode, {
    display_name: 'Member bot',
    enabled: true,
    chat_id: '-100123',
    admin_id: '42',
    scope_source: 'user',
    assigned_user_id: 'user_member',
    manual_all_lines: false,
    line_scopes: [],
    incoming_sms: true,
    missed_calls: true
  })

  const manualMode = createTelegramUnitPayload({
    display_name: 'Manual bot',
    enabled: true,
    chat_id: '-100124',
    admin_id: '43',
    scope_source: 'manual',
    assigned_user_id: 'stale-user',
    manual_all_lines: false,
    line_scopes: [' line_beta '],
    incoming_sms: true,
    missed_calls: false
  })
  assert.deepEqual(manualMode, {
    display_name: 'Manual bot',
    enabled: true,
    chat_id: '-100124',
    admin_id: '43',
    scope_source: 'manual',
    manual_all_lines: false,
    line_scopes: ['line_beta'],
    incoming_sms: true,
    missed_calls: false
  })
  assert.equal('assigned_user_id' in manualMode, false)
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
    chinese
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
      source('../src/i18n/locales/zh-CN.ts')
    ])

  assert.match(settings, /sessionState\.role !== 'admin'/)
  assert.match(settings, /sessionState\.mustChangePassword\) return personal\.slice\(0, 1\)/)
  assert.match(settings, /id: 'system' as const/)
  assert.match(settings, /id: 'recording' as const/)
  assert.match(users, /gateway\.listUsers\(\)/)
  assert.match(users, /gateway\.createMember/)
  assert.match(users, /gateway\.updateMember/)
  assert.match(users, /gateway\.resetMemberPassword/)
  assert.match(telegram, /scopeSource = ref<'manual' \| 'user'>\('user'\)/)
  assert.match(telegram, /scopeSource\.value === 'user'/)
  assert.match(telegram, /assigned_user_id: assignedUserID\.value/)
  assert.match(shell, /sessionState\.role === 'admin'/)
  assert.match(shell, /\{ name: 'traffic', label: t\('shell\.traffic'\)/)
  assert.match(shell, /<DialerPanel v-if="!accountRestricted"/)
  assert.doesNotMatch(router, /to\.name === 'traffic'/)
  assert.match(settings, /id: 'devices' as const/)
  assert.match(users, /bootstrapResource\.data\?\.line_catalog/)
  assert.doesNotMatch(users, /if \(user\.role === 'admin'\) return t\('users\.allLines'\)/)
  assert.match(client, /source\.must_change_password/)
  assert.match(client, /source\.allowed_line_ids/)
  assert.match(session, /state\.allowedLineIDs = \[\.\.\.\(session\.allowed_line_ids \|\| \[\]\)\]/)
  assert.match(session, /resetRecordingState\(\)/)
  assert.match(runtime, /case 'session':[\s\S]*?await refreshSession\(\)/)
  assert.match(
    english,
    /contacts are not resolved and raw numbers are shown/
  )
  assert.match(chinese, /不解析联系人，只显示号码/)
})
