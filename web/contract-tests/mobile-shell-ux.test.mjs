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
  assert.match(shell, /:aria-pressed="uiState\.dialerOpen \|\| activeCallPresent"/)
  assert.match(shell, /const occupiedLineCount = computed\(\(\) => occupiedLineIDs\(\)\.size\)/)
  assert.match(shell, /callState\.sessions\.some\(isLiveCallSession\)/)
  assert.match(shell, /v-if="occupiedLineCount > 0"[\s\S]*mobile-nav__call-count/)
  assert.match(
    shell,
    /function openMobileCall\(\)[\s\S]*if \(activeCallPresent\.value\) showCallSurface\(\)[\s\S]*else openDialer\(\)/
  )
  assert.match(shell, /@click="openDialer\(\)"/)
  assert.match(
    shell,
    /<span class="mobile-nav__label">\{\{ t\('shell\.mobileCall'\) \}\}<\/span>/
  )
  assert.match(shell, /const mobileSecondaryNav = computed/)
  assert.match(
    shell,
    /const mobileSettingsNavItem = computed\(\(\) => \(\{[\s\S]*name: 'settings',[\s\S]*to: \{ name: 'settings' \}/
  )
  assert.match(shell, /ref="mobileMoreTrigger"[\s\S]*:aria-expanded="mobileMoreOpen"/)
})

test('mobile navigation exposes routes that fit and falls back to More on narrow phones', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const styles = await source('../src/style.css')
  const english = await source('../src/i18n/locales/en-US.ts')

  assert.match(shell, /class="mobile-nav__label"/)
  assert.match(shell, /:aria-label="item\.label"/)
  assert.match(
    shell,
    /\['dashboard', 'contacts', 'messages'\]\.includes\(item\.name\)/
  )
  assert.match(shell, /\['calls', 'recordings'\]\.includes\(item\.name\)/)
  assert.match(shell, /const mobileSettingsNavItem = computed/)
  assert.match(shell, /v-for="item in mobileSecondaryNav"/)
  assert.match(
    styles,
    /\.mobile-nav :is\(a, button\) \{[\s\S]*?grid-template-rows: minmax\(0, 1fr\) 14px;/
  )
  assert.match(
    styles,
    /\.mobile-nav \{[\s\S]*?grid-template-columns: repeat\(3, minmax\(0, 1fr\)\) 62px repeat\(3, minmax\(0, 1fr\)\);[\s\S]*?grid-template-rows: 100%;/
  )
  assert.match(
    styles,
    /@media \(max-width: 379px\) \{[\s\S]*?\.mobile-nav \{[\s\S]*?grid-template-columns: repeat\(2, minmax\(0, 1fr\)\) 62px repeat\(2, minmax\(0, 1fr\)\);/
  )
  assert.match(styles, /\.mobile-nav \.mobile-nav__overflow \{\s*display: none;/)
  assert.match(styles, /\.mobile-nav \.mobile-nav__more \{\s*display: grid;/)
  assert.match(
    styles,
    /@media \(max-width: 700px\) \{[\s\S]*?\.mobile-nav__label \{\s*display: block;/
  )
  assert.match(english, /mobileCall: 'Dial'/)
  assert.match(english, /more: 'More'/)
})

test('ringing calls animate the central call action and remain restorable when minimized', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const dialer = await source('../src/components/DialerPanel.vue')
  const callSurface = await source('../src/components/CallSurface.vue')
  const ui = await source('../src/state/ui.ts')
  const styles = await source('../src/style.css')

  assert.match(shell, /const incomingCallRinging = computed/)
  assert.match(shell, /const minimizedIncomingCall = computed/)
  assert.match(shell, /'is-ringing': minimizedIncomingCall/)
  assert.match(shell, /mobile-nav__ring-wave--inner/)
  assert.match(shell, /mobile-nav__ring-wave--outer/)
  assert.match(styles, /@keyframes mobile-call-ring/)
  assert.match(styles, /@keyframes mobile-call-wave/)
  assert.match(ui, /export function minimizeCallSurface\(\): void/)
  assert.match(ui, /uiState\.callMinimized = true/)
  assert.match(dialer, /showingCall \? minimizeCallSurface\(\) : closeDialer\(\)/)
  assert.match(dialer, /<Minus v-if="showingCall"/)
  assert.match(
    callSurface,
    /v-if="\(incoming \|\| active\) && !occupied"[\s\S]*call-footer-action--recording/
  )
})

test('a minimized connected call animates the existing phone audio waves', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const styles = await source('../src/style.css')

  assert.match(shell, /const minimizedActiveCall = computed/)
  assert.match(shell, /'is-active-call': minimizedActiveCall/)
  assert.match(
    styles,
    /\.mobile-nav__dial\.is-active-call \.mobile-nav__dial-icon > svg path:nth-child\(1\)/
  )
  assert.match(styles, /@keyframes mobile-call-audio-wave/)
  assert.doesNotMatch(shell, /mobile-nav__audio-bars/)
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
  assert.doesNotMatch(settings, /attentionCount|dashboard\.attention/)
  assert.match(settings, /class="list-item settings-overview-link"/)
  assert.match(settings, /\{\{ t\('dashboard\.mobileOverview'\) \}\}/)
  assert.match(settings, /\{\{ overviewSummary \}\}/)
  assert.match(settings, /query: \{ item: 'overview', from: 'settings' \}/)
  assert.match(settings, /class="list-item settings-overview-link settings-traffic-link"/)
  assert.match(settings, /name: 'traffic',[\s\S]*query: \{ from: 'settings' \}/)
  assert.match(dashboard, /if \(route\.query\.from === 'settings'\)/)
  assert.match(dashboard, /route\.query\.from === 'settings' \? t\('settings\.back'\)/)
  assert.match(dashboard, /router\.push\(\{ name: 'settings' \}\)/)
  assert.doesNotMatch(settings, /<ChevronRight/)
  assert.match(shell, /const mobileOverviewFromSettings = computed/)
  assert.match(shell, /const mobileTrafficFromSettings = computed/)
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
