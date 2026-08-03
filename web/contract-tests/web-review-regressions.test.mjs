import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('text-entry focus keeps the resting field border and geometry', async () => {
  const styles = await source('../src/style.css')
  const textarea = await source('../src/components/TextareaControl.vue')
  const resourceEditor = await source('../src/components/settings/SettingsMasterDetail.vue')
  const proxyEditor = await source('../src/components/ProxyEditorModal.vue')

  assert.match(styles, /button:focus-visible,\s*a:focus-visible\s*\{/)
  assert.doesNotMatch(styles, /a:focus-visible,\s*input:focus-visible/)
  assert.doesNotMatch(styles, /\.search-field:focus-within\s*\{[^}]*border-color:/)
  assert.doesNotMatch(styles, /\.suggest-input__field:focus-within\s*\{[^}]*box-shadow:/)
  assert.doesNotMatch(textarea, /\.textarea-control:focus-within/)
  assert.doesNotMatch(resourceEditor, /\.settings-master-detail__search:focus-within\s*\{[^}]*box-shadow:/)
  assert.doesNotMatch(proxyEditor, /\.proxy-field (?:input|select):focus/)
})

test('dialing and messages resolve context, contact, and global default lines', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const messages = await source('../src/views/MessagesView.vue')
  const lineSelector = await source('../src/components/LineSelector.vue')
  const workspace = await source('../src/state/workspace.ts')

  assert.match(workspace, /export function resolveLine\(/)
  assert.match(workspace, /const contextLine = byID\(options\.contextKey\)/)
  assert.match(workspace, /contact\?\.preferred_line_id/)
  assert.match(workspace, /line_settings\.default_line_id/)
  assert.match(workspace, /return line\.id\.trim\(\)/)
  assert.doesNotMatch(workspace, /return line\.id \|\| line\.iccid/)

  assert.match(dialer, /resolveLine\('dial'/)
  assert.match(dialer, /contextKey: draftContextLineKey\.value/)
  assert.match(dialer, /preferredLineID: selectedContactPreferredLineID\.value/)
  assert.match(dialer, /lineSelectionOverridden/)
  assert.match(dialer, /\(!selectedLineId\.value \? t\('dialer\.selectLine'\) : ''\)/)
  assert.match(dialer, /<LineSelector/)
  assert.match(dialer, /:lines="lines"/)
  assert.match(dialer, /:disabled-values="unavailableDialLineIDs"/)
  assert.match(dialer, /lines\.value\.filter\(lineCanPlaceVoiceCall\)/)
  assert.doesNotMatch(dialer, /lines\.length === 1/)

  assert.match(messages, /resolveLine\('message'/)
  assert.match(messages, /contextKey: composeContextLineKey\.value/)
  assert.match(messages, /preferredLineID: selectedRecipientPreferredLineID\.value/)
  assert.match(messages, /threadUsesLine\(thread, line\)/)
  assert.match(
    messages,
    /if \(!activeLineID\.value\) return t\('messages\.selectLine'\)/
  )
  assert.doesNotMatch(messages, /activeICCID/)
  assert.match(messages, /<LineSelector/)
  assert.doesNotMatch(messages, /lines\.length === 1/)
  assert.doesNotMatch(lineSelector, /<select|<option/)
  assert.match(lineSelector, /role="listbox"/)
  assert.match(lineSelector, /v-for="\(option, index\) in options"/)
  assert.match(lineSelector, /aria-haspopup="listbox"/)
  assert.match(lineSelector, /case 'ArrowDown'/)
  assert.match(lineSelector, /case 'Escape'/)
})

test('line switching uses one custom selector instead of native dropdowns', async () => {
  const lineSelector = await source('../src/components/LineSelector.vue')
  const contactEditor = await source('../src/components/ContactEditor.vue')
  const proxyEditor = await source('../src/components/ProxyEditorModal.vue')
  const messages = await source('../src/views/MessagesView.vue')

  assert.doesNotMatch(lineSelector, /<select|<option/)
  assert.match(lineSelector, /role="listbox"/)
  assert.match(lineSelector, /role="option"/)
  assert.doesNotMatch(lineSelector, /valueField|device_imei/)
  assert.match(contactEditor, /<LineSelector[\s\S]*v-model="draft\.preferredLineID"/)
  assert.doesNotMatch(contactEditor, /<select v-model="draft\.preferredLineID"/)
  assert.match(proxyEditor, /<LineSelector[\s\S]*v-model="form\.line_id"/)
  assert.doesNotMatch(proxyEditor, /<select v-model="form\.line_id"/)
  assert.match(
    messages,
    /<template v-if="composingNew">[\s\S]*:label="t\('messages\.sendingLine'\)"[\s\S]*compact/
  )
  assert.doesNotMatch(
    messages,
    /<footer class="message-composer">[\s\S]*<LineSelector/
  )
})

test('every dialer request has a monotonic event revision even for the same number', async () => {
  const {
    closeDialer,
    openDialer,
    openDialerAndCall,
    uiState
  } = await import('../src/state/ui.ts')
  const before = uiState.dialRequestRevision

  openDialer('+81 3 1234 5678', 'Test contact')
  const first = uiState.dialRequestRevision
  assert.equal(uiState.dialImmediately, false)
  closeDialer()
  openDialerAndCall('+81 3 1234 5678', 'Test contact')

  assert.equal(first, before + 1)
  assert.equal(uiState.dialRequestRevision, before + 2)
  assert.equal(uiState.dialImmediately, true)

  const dialer = await source('../src/components/DialerPanel.vue')
  assert.match(dialer, /\(\) => uiState\.dialRequestRevision/)
  assert.match(
    dialer,
    /beginDraft\(uiState\.dialTarget, uiState\.dialLabel, focus, uiState\.dialLineKey\)/
  )
  assert.match(dialer, /if \(dialImmediately\) void nextTick\(placeCall\)/)
  assert.doesNotMatch(dialer, /\(\) => \[uiState\.dialTarget, uiState\.dialLabel\]/)
  closeDialer()
})

test('call-again and callback dial immediately while Enter submits the number field', async () => {
  const calls = await source('../src/views/CallsView.vue')
  const dialer = await source('../src/components/DialerPanel.vue')
  const suggest = await source('../src/components/ContactSuggestInput.vue')

  assert.match(calls, /openDialerAndCall\(call\.remote_number, displayName\(call\), actionLineKey\(call\)\)/)
  assert.match(dialer, /<ContactSuggestInput[\s\S]*?@submit="placeCall"/)
  assert.match(
    suggest,
    /if \(event\.key === 'Enter'\)[\s\S]*?if \(suggestion\) \{[\s\S]*?choose\(suggestion\)[\s\S]*?return[\s\S]*?\}[\s\S]*?emit\('submit'\)/
  )
})

test('dialer separates the primary call action from backspace', async () => {
  const dialer = await source('../src/components/DialerPanel.vue')
  const styles = await source('../src/style.css')

  assert.match(
    dialer,
    /class="dialer-number-control"[\s\S]*?v-if="number"[\s\S]*?class="icon-button dialer-backspace-button"/
  )
  assert.match(dialer, /class="dialer-primary-actions"[\s\S]*?class="call-button"/)
  assert.match(dialer, /\.dialer-primary-actions\s*\{[\s\S]*?border-top: 1px solid var\(--border\)/)
  assert.match(
    dialer,
    /\.dialer-number-trailing\s*\{[\s\S]*?position: absolute[\s\S]*?right: 7px/
  )
  assert.match(dialer, /\.dialer-number-trailing \.dialer-backspace-button\s*\{/)
  assert.match(
    dialer,
    /\.dialer-number-entry :deep\(\.suggest-input__field\)\s*\{[\s\S]*?background: transparent[\s\S]*?border-bottom: 2px solid var\(--border\)[\s\S]*?border-radius: 0/
  )
  assert.match(
    dialer,
    /\.dialer-number-entry :deep\(\.suggest-input__field input:focus-visible\)\s*\{[\s\S]*?outline: none[\s\S]*?box-shadow: none/
  )
  assert.match(
    dialer,
    /class="dialer-contact-match"[\s\S]*?<BaseAvatar[\s\S]*?t\('dialer\.matchedContact'\)[\s\S]*?<strong>\{\{ contactLabel \}\}/
  )
  assert.match(dialer, /text-align: center/)
  assert.doesNotMatch(dialer, /class="dialer-actions"/)
  assert.match(styles, /grid-template-columns: repeat\(3, 62px\)/)
  assert.match(dialer, /<small v-if="key\.letters">\{\{ key\.letters \}\}<\/small>/)
  assert.match(styles, /\.keypad__key strong\s*\{[\s\S]*?font-size: 26px/)
  assert.match(styles, /\.keypad__key--zero small\s*\{[\s\S]*?font-size: 14px/)
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
  assert.match(
    suggest,
    /\.suggest-input__field input::placeholder\s*\{[\s\S]*font-size:\s*15px[\s\S]*font-weight:\s*500/
  )
})

test('call rows reuse one selectable surface without a trailing callback button', async () => {
  const [calls, row] = await Promise.all([
    source('../src/views/CallsView.vue'),
    source('../src/components/CallHistoryListItem.vue')
  ])

  assert.match(
    calls,
    /<CallHistoryListItem[\s\S]*?:call="call"[\s\S]*?@select="selectCall"/
  )
  assert.match(
    row,
    /<button[\s\S]*?class="list-item call-list-item"[\s\S]*?@click="emit\('select', props\.call\)"/
  )
  assert.doesNotMatch(calls, /call-list-item__call/)
  assert.doesNotMatch(row, /call-list-item__call/)
})

test('call timer is excluded from live announcements', async () => {
  const surface = await source('../src/components/CallSurface.vue')

  assert.match(surface, /<section v-if="session" class="call-surface">/)
  assert.match(surface, /<span role="status" aria-live="polite" aria-atomic="true">/)
  assert.match(surface, /<strong v-if="duration" aria-live="off">\{\{ duration \}\}<\/strong>/)
  assert.doesNotMatch(surface, /class="call-surface" aria-live=/)
})
