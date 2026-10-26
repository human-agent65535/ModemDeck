import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

async function styleSources() {
  const root = new URL('../src/', import.meta.url)
  const entries = await readdir(root, { recursive: true })
  const files = entries.filter(entry => entry.endsWith('.vue') || entry.endsWith('.css'))
  return Promise.all(files.map(async file => [file, await readFile(new URL(file, root), 'utf8')]))
}

test('type, weight and corner radius come only from design tokens', async () => {
  const offenders = []
  for (const [file, text] of await styleSources()) {
    text.split('\n').forEach((line, index) => {
      if (/^\s*--/.test(line)) return
      if (
        /font-size:\s*\d+(\.\d+)?px/.test(line) ||
        /font-weight:\s*\d{3}\b/.test(line) ||
        /border-radius:[^;]*\b([3-9]|\d{2,})px/.test(line)
      ) {
        offenders.push(`${file}:${index + 1}: ${line.trim()}`)
      }
    })
  }
  assert.deepEqual(offenders, [])

  const styles = await source('../src/style.css')
  assert.match(styles, /--text-title: var\(--text-xl\);\s*--text-heading: var\(--text-lg\);\s*--text-subheading: var\(--text-md\);/)
  assert.match(styles, /\.list-item__content small \{[\s\S]*?font-size: var\(--text-sm\);/)
})

test('appearance follows the system or a saved choice without a light flash', async () => {
  const [html, script, styles, theme, row] = await Promise.all([
    source('../index.html'),
    source('../public/theme.js'),
    source('../src/style.css'),
    source('../src/state/theme.ts'),
    source('../src/components/ThemeSettingRow.vue')
  ])

  assert.match(html, /<script src="\/theme\.js"><\/script>/)
  assert.match(script, /localStorage\.getItem\('modemdeck\.theme'\)/)
  assert.match(script, /setAttribute\('data-theme', dark \? 'dark' : 'light'\)/)
  assert.doesNotMatch(styles, /@media \(prefers-color-scheme: dark\)/)
  assert.match(styles, /:root\[data-theme="dark"\] \{\s*color-scheme: dark;/)
  assert.match(styles, /:root\[data-theme="dark"\] \.avatar--palette-0/)
  assert.match(theme, /systemDark\?\.addEventListener\('change', applyTheme\)/)
  assert.match(row, /@change="setThemePreference\(\$event as ThemePreference\)"/)
})

test('explicit-save settings keep Save in reach and ask before discarding edits', async () => {
  const [guard, users, telegram, styles] = await Promise.all([
    source('../src/composables/useUnsavedChangesGuard.ts'),
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/style.css')
  ])

  assert.match(guard, /onBeforeRouteLeave\(confirmDiscard\)/)
  assert.match(guard, /onBeforeRouteUpdate\(confirmDiscard\)/)
  assert.match(guard, /window\.addEventListener\('beforeunload', onBeforeUnload\)/)
  for (const form of [users, telegram]) {
    assert.match(form, /useUnsavedChangesGuard\(\(\) => !creating\.value && !saving\.value && formChanged\.value\)/)
    assert.match(form, /t\('common\.unsavedChanges'\)/)
  }
  assert.match(styles, /\.settings-form-actions \{[\s\S]*?position: sticky;[\s\S]*?bottom: 0;/)
})

test('data pages keep their layout stable and controls where they act', async () => {
  const [traffic, skeleton, dashboard, player, about, login] = await Promise.all([
    source('../src/views/TrafficView.vue'),
    source('../src/components/skeletons/TrafficSkeleton.vue'),
    source('../src/views/DashboardView.vue'),
    source('../src/components/RecordingPlayer.vue'),
    source('../src/components/AboutSettingsPanel.vue'),
    source('../src/views/LoginView.vue')
  ])

  assert.match(traffic, /<header class="traffic-page__header">\s*<h1>[\s\S]*?<nav v-if="lines\.length > 1" class="traffic-line-filter"/)
  assert.match(traffic, /<TrafficShareChart v-if="lines\.length > 1" :segments="monthShare" \/>/)
  assert.match(traffic, /v-if="visibleProxies\.length === 0"\s*class="proxy-add-card"/)
  assert.match(skeleton, /class="traffic-skeleton__share"/)
  assert.doesNotMatch(dashboard, /dashboard-summary-card is-traffic/)
  assert.match(dashboard, /t\('dashboard\.callsToday'\)/)
  assert.match(player, /@click="skip\(-10\)"[\s\S]*@click="skip\(10\)"[\s\S]*@click="cycleRate"/)
  assert.match(about, /--about-status-color: var\(--warning-strong\);\s*--about-status-background: var\(--surface-subtle\);/)
  assert.match(login, /:type="showPassword \? 'text' : 'password'"/)
  assert.match(login, /return '\/'\n\}/)
})
