import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  createSystemSettingsPayload,
  parseSystemSettingsResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { resolveSystemLanguage } from '../src/i18n/index.ts'
import deDE from '../src/i18n/locales/de-DE.ts'
import enUS from '../src/i18n/locales/en-US.ts'
import esES from '../src/i18n/locales/es-ES.ts'
import frFR from '../src/i18n/locales/fr-FR.ts'
import jaJP from '../src/i18n/locales/ja-JP.ts'
import ptBR from '../src/i18n/locales/pt-BR.ts'
import viVN from '../src/i18n/locales/vi-VN.ts'
import zhCN from '../src/i18n/locales/zh-CN.ts'
import zhTW from '../src/i18n/locales/zh-TW.ts'

const main = await readFile(new URL('../src/main.ts', import.meta.url), 'utf8')
const settingsView = await readFile(
  new URL('../src/views/SettingsView.vue', import.meta.url),
  'utf8'
)
const systemForm = await readFile(
  new URL('../src/components/SystemSettingsForm.vue', import.meta.url),
  'utf8'
)
const accountPanel = await readFile(
  new URL('../src/components/AccountSettingsPanel.vue', import.meta.url),
  'utf8'
)

function leafKeys(value, prefix = '') {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return child && typeof child === 'object' ? leafKeys(child, path) : [path]
  })
}

async function vueFiles(root) {
  const entries = await readdir(root, { withFileTypes: true })
  const files = await Promise.all(
    entries.map(entry => {
      const url = new URL(`${entry.name}${entry.isDirectory() ? '/' : ''}`, root)
      if (entry.isDirectory()) return vueFiles(url)
      return entry.name.endsWith('.vue') ? [url] : []
    })
  )
  return files.flat()
}

test('all locale catalogs contain the same message keys', () => {
  assert.deepEqual(leafKeys(zhCN).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(zhTW).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(jaJP).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(viVN).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(esES).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(deDE).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(frFR).sort(), leafKeys(enUS).sort())
  assert.deepEqual(leafKeys(ptBR).sort(), leafKeys(enUS).sort())
})

test('English-capable Vue UI has no hard-coded Chinese interface copy', async () => {
  const roots = [
    new URL('../src/components/', import.meta.url),
    new URL('../src/views/', import.meta.url)
  ]
  const files = (await Promise.all(roots.map(vueFiles))).flat()

  for (const file of files) {
    const source = await readFile(file, 'utf8')
    assert.doesNotMatch(source, /[\u3400-\u9fff]/u, file.pathname)
  }
})

test('automatic language follows supported browser preferences and otherwise uses English', () => {
  assert.equal(resolveSystemLanguage('auto', ['zh-Hans-CN', 'en-US']), 'zh-CN')
  assert.equal(resolveSystemLanguage('auto', ['zh-Hant-TW', 'en-US']), 'zh-TW')
  assert.equal(resolveSystemLanguage('auto', ['zh-HK', 'en-US']), 'zh-TW')
  assert.equal(resolveSystemLanguage('auto', ['ja-JP', 'en-US']), 'ja-JP')
  assert.equal(resolveSystemLanguage('auto', ['vi-VN', 'en-US']), 'vi-VN')
  assert.equal(resolveSystemLanguage('auto', ['es-MX', 'en-US']), 'es-ES')
  assert.equal(resolveSystemLanguage('auto', ['de-AT', 'en-US']), 'de-DE')
  assert.equal(resolveSystemLanguage('auto', ['fr-CA', 'en-US']), 'fr-FR')
  assert.equal(resolveSystemLanguage('auto', ['pt-PT', 'en-US']), 'pt-BR')
  assert.equal(resolveSystemLanguage('auto', ['en-US', 'ja-JP']), 'en-US')
  assert.equal(resolveSystemLanguage('auto', ['it-IT']), 'en-US')
  assert.equal(resolveSystemLanguage('zh-CN', ['en-US']), 'zh-CN')
  assert.equal(resolveSystemLanguage('en-US', ['zh-CN']), 'en-US')
  assert.equal(resolveSystemLanguage('ja-JP', ['zh-CN']), 'ja-JP')
  assert.equal(resolveSystemLanguage('vi-VN', ['zh-CN']), 'vi-VN')
  assert.equal(resolveSystemLanguage('zh-TW', ['en-US']), 'zh-TW')
  assert.equal(resolveSystemLanguage('es-ES', ['en-US']), 'es-ES')
  assert.equal(resolveSystemLanguage('de-DE', ['en-US']), 'de-DE')
  assert.equal(resolveSystemLanguage('fr-FR', ['en-US']), 'fr-FR')
  assert.equal(resolveSystemLanguage('pt-BR', ['en-US']), 'pt-BR')
})

test('system settings contract supports every available locale', () => {
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
    createSystemSettingsPayload({
      language: 'ja-JP',
      expected_revision: 6
    }),
    {
      language: 'ja-JP',
      expected_revision: 6
    }
  )
  assert.deepEqual(
    createSystemSettingsPayload({
      language: 'pt-BR',
      expected_revision: 8
    }),
    {
      language: 'pt-BR',
      expected_revision: 8
    }
  )
  assert.deepEqual(
    parseSystemSettingsResponse({
      settings: {
        language: 'vi-VN',
        revision: 7
      }
    }),
    {
      language: 'vi-VN',
      revision: 7
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
        language: 'it-IT',
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

test('account settings include the current user language preference', () => {
  assert.match(settingsView, /id: 'preferences'/)
  assert.match(settingsView, /selectedSection === 'preferences'/)
  assert.match(accountPanel, /<SystemSettingsForm(?:\s+v-if="[^"]+")?\s*\/>/)
  assert.match(systemForm, /value: 'auto'/)
  assert.match(systemForm, /value: 'zh-CN'/)
  assert.match(systemForm, /value: 'zh-TW'/)
  assert.match(systemForm, /value: 'en-US'/)
  assert.match(systemForm, /value: 'ja-JP'/)
  assert.match(systemForm, /value: 'vi-VN'/)
  assert.match(systemForm, /value: 'es-ES'/)
  assert.match(systemForm, /value: 'de-DE'/)
  assert.match(systemForm, /value: 'fr-FR'/)
  assert.match(systemForm, /value: 'pt-BR'/)
  assert.match(systemForm, /setSystemLanguage\(language\)/)
  assert.match(systemForm, /expected_revision: settings\.value\.revision/)
})
