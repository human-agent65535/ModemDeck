import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('modal surfaces share one focus, layering, and motion primitive', async () => {
  const [overlay, state, ...consumers] = await Promise.all([
    source('../src/components/OverlayDialog.vue'),
    source('../src/state/overlay.ts'),
    source('../src/components/ConfirmationDialog.vue'),
    source('../src/components/ContactEditor.vue'),
    source('../src/components/ContactNumberActions.vue'),
    source('../src/components/ExternalAccessSettingsPanel.vue'),
    source('../src/components/ProxyEditorModal.vue'),
    source('../src/components/AppShell.vue')
  ])

  assert.match(overlay, /<Teleport to="body">/)
  assert.match(overlay, /<Transition name="overlay">/)
  assert.match(overlay, /role="dialog"/)
  assert.match(overlay, /aria-modal="true"/)
  assert.match(overlay, /event\.key === 'Escape'/)
  assert.match(overlay, /event\.key !== 'Tab'/)
  assert.match(overlay, /previousFocus\?\.isConnected/)
  assert.match(overlay, /overlayIsTopmost\(overlayID\)/)
  assert.match(state, /application\.inert = overlayStack\.length > 0/)

  for (const consumer of consumers) {
    assert.match(consumer, /import OverlayDialog from/)
    assert.match(consumer, /<OverlayDialog/)
    assert.doesNotMatch(consumer, /<Teleport/)
  }
})
