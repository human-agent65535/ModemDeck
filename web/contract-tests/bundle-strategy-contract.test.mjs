import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('production build splits route CSS and keeps intentional chunk groups', async () => {
  const [config, packageJSON, budget] = await Promise.all([
    source('../vite.config.ts'),
    source('../package.json'),
    source('../scripts/check-bundle-budget.mjs')
  ])

  assert.match(config, /cssCodeSplit: true/)
  assert.match(config, /strictExecutionOrder: true/)
  for (const group of ['vue-vendor', 'phone-vendor', 'icons', 'app-shell']) {
    assert.match(config, new RegExp(`name: '${group}'`))
  }
  assert.match(config, /tags: \['\$initial'\]/)
  assert.match(packageJSON, /vite build && npm run check:bundle/)
  assert.match(budget, /initialRequests: 12/)
  assert.match(budget, /javascriptGzip: 100 \* kibibyte/)
  assert.match(budget, /stylesheetGzip: 20 \* kibibyte/)
})

test('local Vite development stays on plain HTTP', async () => {
  const config = await source('../vite.config.ts')

  assert.match(config, /server: \{[\s\S]*?https: false,[\s\S]*?port: 5173/)
})

test('settings panels and their CSS load through one shared async boundary', async () => {
  const [settings, boundary] = await Promise.all([
    source('../src/views/SettingsView.vue'),
    source('../src/components/settings/SettingsAsyncBoundary.vue')
  ])

  assert.match(settings, /defineAsyncComponent/)
  assert.doesNotMatch(
    settings,
    /import (?:AccountSettingsPanel|UserSettingsPanel|DeviceConfigurationPanel) from/
  )
  for (const panel of [
    'AccountPreferencesPanel',
    'SecuritySettingsPanel',
    'UserSettingsPanel',
    'AudioSettingsForm',
    'ContactSyncSettings',
    'DeviceConfigurationPanel',
    'DiagnosticsPanel',
    'PairingSettingsPanel',
    'ConnectivitySettingsPanel',
    'TelegramSettingsForm',
  ]) {
    assert.match(settings, new RegExp(`import\\('../components/${panel}\\.vue'\\)`))
  }
  assert.match(settings, /@focus="preloadSection\(section\.id\)"/)
  assert.match(settings, /@pointerenter="preloadSection\(section\.id\)"/)
  assert.match(settings, /<SettingsAsyncBoundary/)
  assert.match(boundary, /<Suspense>/)
  assert.match(boundary, /<SettingsSkeleton/)
  assert.match(boundary, /:shape="loadingShape"/)
})

test('locale catalogs and fixture data stay out of the production shell', async () => {
  const [i18n, client, main, session] = await Promise.all([
    source('../src/i18n/index.ts'),
    source('../src/api/client.ts'),
    source('../src/main.ts'),
    source('../src/state/session.ts')
  ])

  assert.match(i18n, /import enUS from '\.\/locales\/en-US'/)
  assert.doesNotMatch(i18n, /import zhCN from|import jaJP from|import viVN from/)
  assert.match(i18n, /'zh-CN': \(\) => import\('\.\/locales\/zh-CN'\)/)
  assert.match(i18n, /const localeLoads = new Map/)
  assert.match(main, /await ensureSession\(\)/)
  assert.match(session, /await setSystemLanguage\(session\.language\)/)
  assert.doesNotMatch(client, /import \{ createFixtureGateway \} from '\.\/fixture'/)
  assert.match(client, /await import\('\.\/fixture'\)/)
})
