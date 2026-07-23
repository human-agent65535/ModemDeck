import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('sent messages belong to the line confirmed by the server', async () => {
  const workspace = await readFile(
    new URL('../src/state/workspace.ts', import.meta.url),
    'utf8'
  )
  const fixture = await readFile(new URL('../src/api/fixture.ts', import.meta.url), 'utf8')

  assert.match(workspace, /const key = `\$\{sent\.iccid\}\|\$\{sent\.peer\}`/)
  assert.doesNotMatch(workspace, /input\.thread_key \|\| `\$\{sent\.iccid\}/)
  assert.match(fixture, /const key = threadKey\(iccid, input\.to\)/)
  assert.doesNotMatch(fixture, /input\.thread_key \|\| threadKey/)
})
