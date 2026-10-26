import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  feedbackState,
  pauseFeedback,
  resetFeedback,
  resumeFeedback,
  showFeedback
} from '../src/state/feedback.ts'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('the shell offers a skip link, labelled landmarks, page titles and search shortcuts', async () => {
  const [shell, search, field] = await Promise.all([
    source('../src/components/AppShell.vue'),
    source('../src/components/GlobalSearch.vue'),
    source('../src/components/SearchField.vue')
  ])

  assert.match(shell, /<a class="skip-link" href="#main-content" @click\.prevent="skipToContent">/)
  assert.match(shell, /id="main-content"[\s\S]*tabindex="-1"/)
  assert.match(shell, /<nav class="rail__primary" :aria-label="t\('shell\.primaryNavigation'\)">/)
  assert.match(shell, /<nav class="rail__secondary" :aria-label="t\('shell\.secondaryNavigation'\)">/)
  // Sub-routes such as /settings/security still mark their rail destination as current.
  assert.match(shell, /:aria-current="route\.name === 'settings' \? 'page' : undefined"/)
  assert.match(shell, /return page \? `\$\{page\} · ModemDeck` : 'ModemDeck'/)
  assert.match(shell, /watch\(documentTitle, title => \{ document\.title = title \}, \{ immediate: true \}\)/)

  assert.match(search, /event\.key\.toLowerCase\(\) === 'k' &&\s*\(event\.metaKey \|\| event\.ctrlKey\)/)
  assert.match(search, /event\.key === '\/' &&[\s\S]*!editableTarget\(event\.target\)/)
  assert.match(search, /keyshortcuts="Meta\+K Control\+K \/"/)
  assert.match(field, /<kbd v-if="shortcut && !modelValue" class="search-field__shortcut" aria-hidden="true">/)
})

test('a minimized call stays visible in the header and live calls are not shown as errors', async () => {
  const [shell, pill, dialer, styles] = await Promise.all([
    source('../src/components/AppShell.vue'),
    source('../src/components/ActiveCallPill.vue'),
    source('../src/components/DialerPanel.vue'),
    source('../src/style.css')
  ])

  assert.match(shell, /<ActiveCallPill\s*v-if="activeCallPresent && uiState\.callMinimized"\s*:ringing="incomingCallRinging"/)
  assert.match(pill, /@click="showCallSurface\(\)"/)
  assert.match(pill, /color: var\(--success\);/)
  // The pill appears exactly where the dialer is not docked beside the content.
  assert.match(styles, /\.shell-header \.shell-dialer-toggle,\s*\.shell-header \.active-call-pill \{\s*display: none;/)
  assert.match(styles, /@media \(max-width: 1479px\) \{[\s\S]*?\.shell-header \.active-call-pill \{\s*display: inline-flex;/)
  assert.match(dialer, /\.dialer-active-calls--busy \{\s*color: var\(--success\);\s*background: var\(--success-soft\);/)
})

test('call controls keep one layout and audio problems are a banner with the fix', async () => {
  const surface = await source('../src/components/CallSurface.vue')
  const controls = surface.slice(surface.indexOf('class="call-surface__controls"'), surface.indexOf('class="call-surface__devices"'))

  assert.match(surface, /const showMediaControls = computed\(\s*\(\) =>\s*!terminal\.value &&\s*owned\.value &&\s*!incoming\.value &&\s*!occupied\.value/)
  assert.doesNotMatch(controls, /v-if="mediaControllable"|v-if="keypadAvailable"|v-if="recordingAdjustable"/)
  assert.equal((controls.match(/:disabled="!mediaControllable"/g) || []).length, 2)
  assert.doesNotMatch(controls, /playbackBlocked|retryActiveCallMedia/)
  assert.match(surface, /<div v-if="audioNotice" class="call-surface__audio-notice" role="alert">/)
  assert.match(surface, /if \(callMediaState\.playbackBlocked\) return \{ message: t\('calls\.audioBlockedNotice'\), action: 'resume' \}/)
  assert.match(surface, /if \(callMediaState\.status === 'error'\) return \{ message: t\('calls\.audioFailedNotice'\), action: 'retry' \}/)
  assert.match(surface, /grid-template-columns: repeat\(auto-fit, minmax\(80px, 1fr\)\);/)
})

test('destructive confirmations name their target and announce as alert dialogs', async () => {
  const [confirmation, access, users] = await Promise.all([
    source('../src/components/ConfirmationDialog.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/UserSettingsPanel.vue')
  ])

  assert.match(confirmation, /layer="critical"\s*role="alertdialog"/)
  assert.match(access, /t\('iosPairing\.revokeDeviceTitle', \{ device: deviceName \}\)/)
  assert.match(access, /@click="revokePairing\(device\.id, pairingDeviceTitle\(device\)\)"/)
  assert.match(users, /t\('iosPairing\.revokeUserTitle', \{ user: user\.username \}\)/)
})

test('errors stay until dismissed and hovering holds every toast', t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  resetFeedback()
  try {
    showFeedback('Saved', 'success')
    showFeedback('Could not save', 'error')
    t.mock.timers.tick(60_000)
    assert.deepEqual(feedbackState.items.map(item => item.tone), ['error'])

    resetFeedback()
    showFeedback('Saved', 'success')
    t.mock.timers.tick(3000)
    pauseFeedback()
    t.mock.timers.tick(10_000)
    assert.equal(feedbackState.items.length, 1)
    resumeFeedback()
    t.mock.timers.tick(1300)
    assert.equal(feedbackState.items.length, 0)
  } finally {
    resetFeedback()
  }
})
