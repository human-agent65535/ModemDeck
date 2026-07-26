import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  createSystemSettingsPayload,
  parseSystemSettingsResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { resolveSystemLanguage } from '../src/i18n/index.ts'
import enUS from '../src/i18n/locales/en-US.ts'
import zhCN from '../src/i18n/locales/zh-CN.ts'

const main = await readFile(new URL('../src/main.ts', import.meta.url), 'utf8')
const settingsView = await readFile(
  new URL('../src/views/SettingsView.vue', import.meta.url),
  'utf8'
)
const systemForm = await readFile(
  new URL('../src/components/SystemSettingsForm.vue', import.meta.url),
  'utf8'
)

function leafKeys(value, prefix = '') {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return child && typeof child === 'object' ? leafKeys(child, path) : [path]
  })
}

test('Chinese and English catalogs contain the same message keys', () => {
  assert.deepEqual(leafKeys(zhCN).sort(), leafKeys(enUS).sort())
})

test('automatic language follows Chinese browser preferences and otherwise uses English', () => {
  assert.equal(resolveSystemLanguage('auto', ['zh-Hans-CN', 'en-US']), 'zh-CN')
  assert.equal(resolveSystemLanguage('auto', ['ja-JP', 'en-US']), 'en-US')
  assert.equal(resolveSystemLanguage('zh-CN', ['en-US']), 'zh-CN')
  assert.equal(resolveSystemLanguage('en-US', ['zh-CN']), 'en-US')
})

test('system settings contract supports only auto, Simplified Chinese, and English', () => {
  assert.deepEqual(
    createSystemSettingsPayload({
      language: 'auto',
      expected_revision: 4
    }),
    {
      language: 'auto',
      expected_revision: 4
    }
  )
  assert.deepEqual(
    parseSystemSettingsResponse({
      settings: {
        language: 'en-US',
        revision: 5
      }
    }),
    {
      language: 'en-US',
      revision: 5
    }
  )
  assert.throws(
    () =>
      createSystemSettingsPayload({
        language: 'ja-JP',
        expected_revision: 4
      }),
    /language is invalid/
  )
})

test('fixture gateway persists language with optimistic revision control', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.getSystemSettings()
  assert.deepEqual(initial, { language: 'auto', revision: 1 })

  const updated = await gateway.updateSystemSettings({
    language: 'zh-CN',
    expected_revision: initial.revision
  })
  assert.deepEqual(updated, { language: 'zh-CN', revision: 2 })
  await assert.rejects(
    gateway.updateSystemSettings({
      language: 'en-US',
      expected_revision: initial.revision
    }),
    /another session/
  )
})

test('app loads the public session before mounting the i18n-enabled UI', () => {
  assert.match(main, /await ensureSession\(\)/)
  assert.match(main, /createApp\(App\)\.use\(i18n\)\.use\(router\)\.mount/)
})

test('settings makes system language a first-class section', () => {
  assert.match(settingsView, /type SettingsSection = 'system'/)
  assert.match(settingsView, /<SystemSettingsForm \/>/)
  assert.match(systemForm, /value: 'auto'/)
  assert.match(systemForm, /value: 'zh-CN'/)
  assert.match(systemForm, /value: 'en-US'/)
  assert.match(systemForm, /setSystemLanguage\(language\)/)
  assert.match(systemForm, /expected_revision: settings\.value\.revision/)
})
