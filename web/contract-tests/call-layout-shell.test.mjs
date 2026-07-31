import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('calls replace the desktop dialer and share the mobile dialer height', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const dialer = await source('../src/components/DialerPanel.vue')
  const styles = await source('../src/style.css')

  assert.match(shell, /<DialerPanel :permanent="permanentDialer" \/>/)
  assert.match(dialer, /<CallSurface v-if="showingCall" \/>/)
  assert.match(dialer, /<div v-else class="dialer-panel__body">/)
  assert.match(styles, /--dialer-width: clamp\(360px, 28vw, 420px\);/)
  assert.match(
    dialer,
    /@media \(max-width: 860px\)[\s\S]*\.drawer-backdrop \.dialer-panel \{[\s\S]*height: min\(720px, calc\(100dvh - var\(--mobile-nav-height\) - 8px\)\);/
  )
})
