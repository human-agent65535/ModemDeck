import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('mobile shell uses page context and a dedicated central dial action', async () => {
  const shell = await source('../src/components/AppShell.vue')

  assert.match(shell, /const mobilePageTitle = computed/)
  assert.match(shell, /<div class="mobile-brand">\{\{ mobilePageTitle \}\}<\/div>/)
  assert.match(shell, /class="mobile-nav__dial"/)
  assert.match(shell, /:aria-pressed="uiState\.dialerOpen"/)
  assert.match(shell, /@click="openDialer\(\)"/)
  assert.match(shell, /<span>\{\{ t\('shell\.mobileCall'\) \}\}<\/span>/)
  assert.match(shell, /:to="\{ name: 'settings' \}"/)
})

test('mobile shell removes duplicate global search and compacts page toolbars', async () => {
  const styles = await source('../src/style.css')

  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.global-search \{\s*display: none;/
  )
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.list-pane > \.pane-header:not\(:has\(> button\)\) \{\s*display: none;/
  )
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.search-field input \{\s*font-size: 15px;/
  )
})

test('incoming call policy becomes a viewport-bound compact mobile sheet', async () => {
  const incomingCallMode = await source('../src/components/IncomingCallModeControl.vue')

  assert.match(
    incomingCallMode,
    /@media \(max-width: 560px\)[\s\S]*\.incoming-call-mode__menu \{[\s\S]*position: fixed;[\s\S]*right: 10px;[\s\S]*left: 10px;[\s\S]*width: auto;/
  )
  assert.match(
    incomingCallMode,
    /@media \(max-width: 560px\)[\s\S]*\.incoming-call-mode__menu > button \{[\s\S]*min-height: 54px;/
  )
  assert.match(incomingCallMode, /:aria-label="label"/)
  assert.match(incomingCallMode, /incoming-call-mode__option-icon--quiet/)
  assert.match(incomingCallMode, /<Moon v-else/)
  assert.doesNotMatch(incomingCallMode, /BellOff/)
})

test('mobile list creation actions share one bottom-right floating treatment', async () => {
  const contacts = await source('../src/views/ContactsView.vue')
  const messages = await source('../src/views/MessagesView.vue')
  const styles = await source('../src/style.css')

  assert.match(contacts, /class="pane-create-button mobile-list-fab"/)
  assert.match(contacts, /<span>\{\{ t\('contacts\.new'\) \}\}<\/span>/)
  assert.match(messages, /class="pane-create-button mobile-list-fab"/)
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.mobile-list-fab,[\s\S]*position: fixed;[\s\S]*right: 16px;[\s\S]*bottom: calc\(var\(--mobile-nav-height\) \+ 16px\);/
  )
  assert.match(styles, /\.mobile-list-fab > span \{\s*display: none;/)
})

test('mobile settings starts with a live communication overview entry', async () => {
  const settings = await source('../src/views/SettingsView.vue')
  const dashboard = await source('../src/views/DashboardView.vue')
  const shell = await source('../src/components/AppShell.vue')
  const styles = await source('../src/style.css')

  assert.match(settings, /const overviewSummary = computed/)
  assert.match(settings, /class="list-item settings-overview-link"/)
  assert.match(settings, /\{\{ t\('dashboard\.mobileOverview'\) \}\}/)
  assert.match(settings, /\{\{ overviewSummary \}\}/)
  assert.match(settings, /query: \{ item: 'overview', from: 'settings' \}/)
  assert.match(dashboard, /if \(route\.query\.from === 'settings'\)/)
  assert.match(dashboard, /route\.query\.from === 'settings' \? t\('settings\.back'\)/)
  assert.match(dashboard, /router\.push\(\{ name: 'settings' \}\)/)
  assert.doesNotMatch(settings, /<ChevronRight/)
  assert.match(shell, /const mobileOverviewFromSettings = computed/)
  assert.match(shell, /v-if="mobileShellBackVisible"/)
  assert.match(shell, /@click="backToSettingsMenu"/)
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.settings-overview-link \{\s*display: flex;/
  )
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.settings-detail-header \{\s*display: none;/
  )
  assert.match(
    styles,
    /@media \(max-width: 860px\)[\s\S]*\.dashboard-overview-back\.is-shell-managed \{\s*display: none;/
  )
})
