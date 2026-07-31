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

  assert.match(
    shell,
    /<DialerPanel[\s\S]*:permanent="permanentDialer"[\s\S]*:non-modal="nonModalDialer"/
  )
  assert.match(dialer, /<CallSurface v-if="showingCall" \/>/)
  assert.match(dialer, /<div v-else class="dialer-panel__body">/)
  assert.match(styles, /--dialer-width: clamp\(360px, 28vw, 420px\);/)
  assert.match(
    dialer,
    /@media \(max-width: 860px\)[\s\S]*\.drawer-backdrop \.dialer-panel \{[\s\S]*height: min\(720px, calc\(100dvh - var\(--mobile-nav-height\) - 8px\)\);/
  )
})

test('desktop dialer is a non-blocking side panel below the permanent breakpoint', async () => {
  const shell = await source('../src/components/AppShell.vue')
  const dialer = await source('../src/components/DialerPanel.vue')

  assert.match(shell, /window\.matchMedia\('\(min-width: 861px\)'\)/)
  assert.match(dialer, /nonModal\?: boolean/)
  assert.match(
    dialer,
    /:role="permanent \? undefined : nonModal \? 'complementary' : 'dialog'"/
  )
  assert.match(
    dialer,
    /:aria-modal="!permanent && !nonModal \? true : undefined"/
  )
  assert.match(
    dialer,
    /@media \(min-width: 861px\) and \(max-width: 1479px\)[\s\S]*\.drawer-backdrop--nonmodal \{[\s\S]*pointer-events: none;[\s\S]*background: transparent;/
  )
  assert.match(
    dialer,
    /\.drawer-backdrop--nonmodal \.dialer-panel \{\s*pointer-events: auto;/
  )
})
