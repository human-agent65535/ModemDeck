import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('anchored menus and listboxes share one motion primitive', async () => {
  const [transition, audio, incoming, line, country, contactSuggest] = await Promise.all([
    source('../src/components/PopoverTransition.vue'),
    source('../src/components/AudioSettingsMenu.vue'),
    source('../src/components/IncomingCallModeControl.vue'),
    source('../src/components/LineSelector.vue'),
    source('../src/components/CountryRegionSelector.vue'),
    source('../src/components/ContactSuggestInput.vue')
  ])

  assert.match(transition, /<Transition name="popover-motion">/)
  assert.match(transition, /opacity var\(--motion-fast\)/)
  assert.match(transition, /transform var\(--motion-base\)/)
  assert.match(transition, /prefers-reduced-motion: reduce/)

  for (const consumer of [audio, incoming, line, country, contactSuggest]) {
    assert.match(consumer, /import PopoverTransition from/)
    assert.match(consumer, /<PopoverTransition>/)
  }
})

test('audio popover closes when keyboard focus leaves it', async () => {
  const audio = await source('../src/components/AudioSettingsMenu.vue')

  assert.match(audio, /function onDocumentFocusIn/)
  assert.match(audio, /document\.addEventListener\('focusin', onDocumentFocusIn\)/)
  assert.match(audio, /document\.removeEventListener\('focusin', onDocumentFocusIn\)/)
})

test('workspace action overflow has synchronized visibility and focus dismissal', async () => {
  const actions = await source('../src/components/workspace/WorkspaceDetailActions.vue')

  assert.match(actions, /opacity var\(--motion-fast\)/)
  assert.match(actions, /visibility 0s linear var\(--motion-base\)/)
  assert.match(actions, /document\.addEventListener\('focusin', onDocumentFocusIn\)/)
  assert.doesNotMatch(
    actions,
    /\.workspace-detail-actions__secondary\s*\{[^}]*display:\s*none/s
  )
})
