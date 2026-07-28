import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { createRuntimeRefreshQueue } from '../src/state/runtimeEvents.ts'

test('runtime refresh queue coalesces duplicate resources without overlapping refreshes', async () => {
  let releaseFirst
  const firstRefresh = new Promise(resolve => {
    releaseFirst = resolve
  })
  const calls = []
  const active = new Map()
  const maximum = new Map()
  let first = true
  const queue = createRuntimeRefreshQueue(async resource => {
    calls.push(resource)
    const count = (active.get(resource) || 0) + 1
    active.set(resource, count)
    maximum.set(resource, Math.max(maximum.get(resource) || 0, count))
    if (resource === 'lines' && first) {
      first = false
      await firstRefresh
    }
    active.set(resource, (active.get(resource) || 1) - 1)
  })

  const initial = queue.enqueue(['lines', 'lines'])
  await Promise.resolve()
  const followUp = queue.enqueue(['lines', 'network', 'network'])
  releaseFirst()
  await Promise.all([initial, followUp])

  assert.deepEqual(calls, ['lines', 'lines', 'network'])
  assert.equal(maximum.get('lines'), 1)
  assert.equal(maximum.get('network'), 1)
  queue.stop()
})

test('runtime SSE is global to the authenticated application shell', async () => {
  const shell = await readFile(
    new URL('../src/components/AppShell.vue', import.meta.url),
    'utf8'
  )
  const client = await readFile(new URL('../src/api/client.ts', import.meta.url), 'utf8')
  const runtime = await readFile(
    new URL('../src/state/runtimeEvents.ts', import.meta.url),
    'utf8'
  )

  assert.match(client, /new EventSource\(`\$\{API_ROOT\}\/runtime\/events`/)
  assert.match(client, /source\.addEventListener\('runtime'/)
  assert.match(client, /source\.addEventListener\('reset'/)
  assert.match(client, /RUNTIME_RESOURCES[\s\S]*?'messages'/)
  assert.match(shell, /initializeRuntimeEvents\(\)/)
  assert.match(shell, /shutdownRuntimeEvents\(\)/)
  assert.match(runtime, /refreshDeviceWorkspace\(\)/)
  assert.match(runtime, /loadNetwork\(true, true\)/)
  assert.match(runtime, /refreshCalls\(\)/)
  assert.match(runtime, /case 'messages':[\s\S]*?refreshMessageWorkspace\(\)/)
  assert.match(runtime, /if \(wasConnected\) void refreshQueue\?\.enqueue\(ALL_RESOURCES\)/)
})
