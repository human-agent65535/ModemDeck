import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'
import { h, ref } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { useUnsavedChangesGuard } from '../src/composables/useUnsavedChangesGuard.ts'
import { answerConfirmation, confirmationState } from '../src/state/confirmation.ts'
import { mountComponent } from './helpers/component-harness.mjs'

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
    assert.match(form, /useUnsavedChangesGuard\(\s*\(\) => !creating\.value && !saving\.value && formChanged\.value\s*\)/)
    assert.match(form, /t\('common\.unsavedChanges'\)/)
    // Choosing another entry changes the form before the route guard runs, so
    // selection asks first through the guard's returned check.
    assert.match(form, /if \(!\(await confirmDiscardChanges\(\)\)\) return/)
  }
  assert.match(styles, /\.settings-form-actions \{[\s\S]*?position: sticky;[\s\S]*?bottom: 0;/)
})

test('every explicit-save settings panel with drafts is guarded by the shared check', async () => {
  const [devices, contactSync] = await Promise.all([
    source('../src/components/DeviceConfigurationPanel.vue'),
    source('../src/components/ContactSyncSettings.vue')
  ])
  // Device workbench: module name, line identity and the connection profile.
  assert.match(
    devices,
    /const deviceDraftsDirty = computed\(\s*\(\) => moduleNameDirty\.value \|\| lineIdentityDirty\.value \|\| profileDirty\.value\s*\)/
  )
  assert.match(devices, /useUnsavedChangesGuard\(\s*\(\) => !discardAlreadyConfirmed && deviceDraftsDirty\.value\s*\)/)
  assert.match(
    devices,
    /async function chooseLine[\s\S]*?if \(!\(await confirmDiscardDeviceDrafts\(\)\)\) return[\s\S]*?selectLine\(line\)/
  )
  // Contact sync: the Google Client ID is saved explicitly.
  assert.match(contactSync, /const clientIDDirty = computed\(\(\) => clientID\.value\.trim\(\) !== savedClientID\.value\)/)
  assert.match(contactSync, /useUnsavedChangesGuard\(clientIDDirty\)/)
})

test('the unsaved-changes guard asks before leaving, switching entries or closing the tab', async t => {
  const previousWindow = globalThis.window
  const browserWindow = new EventTarget()
  globalThis.window = browserWindow
  t.after(() => {
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
    while (confirmationState.request) answerConfirmation(false)
  })

  const dirty = ref(false)
  let confirmDiscard
  const Guarded = {
    setup() {
      confirmDiscard = useUnsavedChangesGuard(() => dirty.value)
      return () => null
    }
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/settings', component: Guarded },
      { path: '/elsewhere', component: { render: () => null } }
    ]
  })
  await router.push('/settings?entry=a')
  const { unmount } = mountComponent({ render: () => h(RouterView) }, { plugins: [router] })
  t.after(unmount)
  const settle = () => new Promise(resolve => setImmediate(resolve))
  const closeTab = () => {
    const event = new Event('beforeunload', { cancelable: true })
    browserWindow.dispatchEvent(event)
    return event.defaultPrevented
  }

  // Clean forms never ask.
  await router.push('/settings?entry=b')
  assert.equal(confirmationState.request, null)
  assert.equal(router.currentRoute.value.fullPath, '/settings?entry=b')
  assert.equal(closeTab(), false)
  assert.equal(await confirmDiscard(), true)

  // A dirty form asks on route update (another entry) and on leaving; Cancel stays.
  dirty.value = true
  assert.equal(closeTab(), true)
  for (const target of ['/settings?entry=c', '/elsewhere']) {
    const navigation = router.push(target)
    await settle()
    assert.equal(confirmationState.request?.tone, 'danger')
    answerConfirmation(false)
    await navigation
    assert.equal(router.currentRoute.value.fullPath, '/settings?entry=b')
  }

  // The returned check asks the same question for in-page selection changes.
  const asked = confirmDiscard()
  assert.ok(confirmationState.request)
  answerConfirmation(true)
  assert.equal(await asked, true)

  // Discard lets navigation through, and an unmounted form stops guarding the tab.
  const leaving = router.push('/elsewhere')
  await settle()
  answerConfirmation(true)
  await leaving
  assert.equal(router.currentRoute.value.fullPath, '/elsewhere')
  assert.equal(closeTab(), false)
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

test('review fixes: entry switches ask first, phone Traffic keeps its filter, narrow details wrap', async () => {
  const [users, telegram, traffic, header, toolbar] = await Promise.all([
    source('../src/components/UserSettingsPanel.vue'),
    source('../src/components/TelegramSettingsForm.vue'),
    source('../src/views/TrafficView.vue'),
    source('../src/components/workspace/WorkspaceDetailHeader.vue'),
    source('../src/components/workspace/CommunicationListToolbar.vue')
  ])

  // The edit is replaced before the route guard runs, so switching entries confirms first.
  assert.match(users, /async function selectUser\(id: string\): Promise<void> \{[\s\S]*?if \(!\(await confirmDiscardChanges\(\)\)\) return[\s\S]*?selectedID\.value = id/)
  assert.match(users, /async function startCreate\(\): Promise<void> \{[\s\S]*?if \(!\(await confirmDiscardChanges\(\)\)\) return[\s\S]*?creating\.value = true/)
  assert.match(telegram, /async function selectUnit\(id: string\): Promise<void> \{[\s\S]*?if \(!\(await confirmDiscardChanges\(\)\)\) return[\s\S]*?discardDraft\(id\)/)
  assert.match(telegram, /async function startCreate\(\): Promise<void> \{[\s\S]*?if \(!\(await confirmDiscardChanges\(\)\)\) return/)
  assert.match(traffic, /@media \(max-width: 560px\) \{[\s\S]*?\.traffic-page__header h1 \{\s*display: none;/)
  assert.doesNotMatch(traffic, /\.traffic-page__header \{\s*display: none;/)
  assert.match(header, /flex-wrap: wrap;/)
  assert.match(toolbar, /--filters-4 :deep\(\.segmented-control\) \{\s*grid-template-columns: repeat\(4, auto\);/)
})

test('review fixes: dialer keeps names intact, edits at the caret, and Clear filters navigates once', async () => {
  const [dialer, messages] = await Promise.all([
    source('../src/components/DialerPanel.vue'),
    source('../src/views/MessagesView.vue')
  ])

  assert.match(dialer, /if \(!isDialEntry\(value\)\) \{\s*number\.value = value\s*return/)
  assert.match(dialer, /next = stripped\.slice\(0, digitsBeforeCaret - 1\) \+ stripped\.slice\(digitsBeforeCaret\)/)
  assert.match(dialer, /input\.setSelectionRange\(position, position\)/)
  assert.match(messages, /function clearFilters\(\): void \{[\s\S]*?delete query\.filter\s*delete query\.favorite\s*void router\.replace/)
})
