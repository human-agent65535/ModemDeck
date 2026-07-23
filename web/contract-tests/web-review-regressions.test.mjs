import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('dialing and messages resolve context, contact, and global default lines', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const messages = await source('../src/views/MessagesView.vue')
  const workspace = await source('../src/state/workspace.ts')

  assert.match(workspace, /export function resolveLine\(/)
  assert.match(workspace, /const contextLine = byKey\(options\.contextKey\)/)
  assert.match(workspace, /contact\?\.preferred_device_imei/)
  assert.match(workspace, /line_settings\.default_device_imei/)

  assert.match(dialer, /resolveLine\('dial'/)
  assert.match(dialer, /contextKey: draftContextLineKey\.value/)
  assert.match(dialer, /lineSelectionOverridden/)
  assert.match(dialer, /\(!selectedLineId\.value \? '选择线路' : ''\)/)
  assert.match(dialer, /<option value="" disabled>选择线路<\/option>/)
  assert.doesNotMatch(dialer, /lines\.length === 1/)

  assert.match(messages, /resolveLine\('message'/)
  assert.match(messages, /contextKey: composeContextLineKey\.value/)
  assert.match(messages, /threadUsesLine\(thread, line\)/)
  assert.match(messages, /if \(!activeLineID\.value && !activeICCID\.value\) return '请选择线路'/)
  assert.match(messages, /<option value="" disabled>选择线路<\/option>/)
  assert.doesNotMatch(messages, /lines\.length === 1/)
})

test('every dialer request has a monotonic event revision even for the same number', async () => {
  const { closeDialer, openDialer, uiState } = await import('../src/state/ui.ts')
  const before = uiState.dialRequestRevision

  openDialer('+81 3 1234 5678', 'Test contact')
  const first = uiState.dialRequestRevision
  closeDialer()
  openDialer('+81 3 1234 5678', 'Test contact')

  assert.equal(first, before + 1)
  assert.equal(uiState.dialRequestRevision, before + 2)

  const dialer = await source('../src/components/DialerPanel.vue')
  assert.match(dialer, /\(\) => uiState\.dialRequestRevision/)
  assert.match(
    dialer,
    /beginDraft\(uiState\.dialTarget, uiState\.dialLabel, true, uiState\.dialLineKey\)/
  )
  assert.doesNotMatch(dialer, /\(\) => \[uiState\.dialTarget, uiState\.dialLabel\]/)
  closeDialer()
})

test('header menu and audio dialog implement bounded keyboard focus behavior', async () => {
  const incoming = await source('../src/components/IncomingCallModeControl.vue')
  const audio = await source('../src/components/AudioSettingsMenu.vue')

  for (const key of ['Escape', 'ArrowDown', 'ArrowUp', 'Home', 'End', 'Tab']) {
    assert.match(incoming, new RegExp(`event\\.key === '${key}'`))
  }
  assert.match(incoming, /role="menu"/)
  assert.match(incoming, /role="menuitemradio"/)
  assert.match(incoming, /close\(true\)/)
  assert.match(incoming, /selected\.focus\(\)/)
  assert.match(incoming, /leaveMenu\(event\.shiftKey\)/)

  assert.match(audio, /role="dialog"/)
  assert.match(audio, /aria-haspopup="dialog"/)
  assert.match(audio, /event\.key !== 'Escape'/)
  assert.match(audio, /close\(true\)/)
  assert.match(audio, /querySelector<HTMLElement>/)
  assert.doesNotMatch(audio, /focus-trap|trapFocus/)
})

test('contact suggestions expose a complete combobox relationship', async () => {
  const suggest = await source('../src/components/ContactSuggestInput.vue')

  assert.match(suggest, /role="combobox"/)
  assert.match(suggest, /aria-autocomplete="list"/)
  assert.match(suggest, /:aria-expanded="showSuggestions"/)
  assert.match(suggest, /:aria-controls="listboxId"/)
  assert.match(suggest, /:aria-activedescendant="activeDescendant"/)
  assert.match(suggest, /:id="listboxId"[^>]*role="listbox"/)
  assert.match(suggest, /:id="`\$\{listboxId\}-option-\$\{index\}`"/)
  assert.match(suggest, /role="option"/)
  assert.match(suggest, /:aria-selected="index === activeIndex"/)
})

test('call rows use sibling buttons and callback cannot trigger row selection', async () => {
  const calls = await source('../src/views/CallsView.vue')

  assert.doesNotMatch(calls, /role="button"/)
  assert.match(calls, /class="call-list-item__select"[\s\S]*?@click="selectCall\(call\)"/)
  assert.match(
    calls,
    /class="icon-button icon-button--quiet call-list-item__call"[\s\S]*?@click="callBack\(call\)"[\s\S]*?@keydown\.enter\.prevent="callBack\(call\)"/
  )
  assert.doesNotMatch(calls, /@keydown\.enter="selectCall\(call\)"/)
  assert.doesNotMatch(calls, /@click\.stop="callBack\(call\)"/)
})

test('call timer is excluded from live announcements', async () => {
  const surface = await source('../src/components/CallSurface.vue')

  assert.match(surface, /<section v-if="session" class="call-surface">/)
  assert.match(surface, /<span role="status" aria-live="polite" aria-atomic="true">/)
  assert.match(surface, /<strong v-if="duration" aria-live="off">\{\{ duration \}\}<\/strong>/)
  assert.doesNotMatch(surface, /class="call-surface" aria-live=/)
})
