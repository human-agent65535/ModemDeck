import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
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
  const replayBurst = Array.from({ length: 512 }, () =>
    queue.enqueue(['lines', 'network', 'network'])
  )
  releaseFirst()
  await Promise.all([initial, ...replayBurst])

  assert.deepEqual(calls, ['lines', 'lines', 'network'])
  assert.equal(maximum.get('lines'), 1)
  assert.equal(maximum.get('network'), 1)
  queue.stop()
})

test('runtime refresh queue continues after a failed resource refresh', async () => {
  const calls = []
  const queue = createRuntimeRefreshQueue(async resource => {
    calls.push(resource)
    if (resource === 'lines') throw new Error('request timed out')
  })

  await queue.enqueue(['lines'])
  await queue.enqueue(['calls'])

  assert.deepEqual(calls, ['lines', 'calls'])
  queue.stop()
})

test('API requests abort at the shared deadline', async () => {
  const originalFetch = globalThis.fetch
  const originalTimeout = AbortSignal.timeout

  try {
    AbortSignal.timeout = () => {
      const controller = new AbortController()
      queueMicrotask(() => {
        controller.abort(new DOMException('request timed out', 'TimeoutError'))
      })
      return controller.signal
    }
    globalThis.fetch = (_input, init) =>
      new Promise((_resolve, reject) => {
        const signal = init?.signal
        assert.ok(signal)
        const rejectAborted = () => reject(signal.reason)
        if (signal.aborted) {
          rejectAborted()
        } else {
          signal.addEventListener('abort', rejectAborted, { once: true })
        }
      })

    await assert.rejects(gateway.getBootstrap(), error => {
      assert.equal(error?.code, 'request_timeout')
      return true
    })
  } finally {
    globalThis.fetch = originalFetch
    AbortSignal.timeout = originalTimeout
  }
})

test('runtime SSE reports heartbeats and recreates stale or malformed streams', () => {
  const originalEventSource = globalThis.EventSource
  const originalSetTimeout = globalThis.setTimeout
  const originalClearTimeout = globalThis.clearTimeout
  const timers = new Map()
  let nextTimerID = 0

  class FakeEventSource {
    static instances = []

    constructor(url, options) {
      this.closed = false
      this.listeners = new Map()
      this.url = url
      this.withCredentials = options?.withCredentials
      FakeEventSource.instances.push(this)
    }

    addEventListener(type, handler) {
      this.listeners.set(type, handler)
    }

    close() {
      this.closed = true
    }

    emit(type, data) {
      this.listeners.get(type)?.({ data })
    }
  }

  try {
    globalThis.EventSource = FakeEventSource
    globalThis.setTimeout = callback => {
      nextTimerID += 1
      timers.set(nextTimerID, callback)
      return nextTimerID
    }
    globalThis.clearTimeout = timerID => {
      timers.delete(timerID)
    }
    let errors = 0
    let heartbeatAt = ''
    const close = gateway.subscribeRuntimeEvents({
      onOpen() {},
      onHeartbeat(observedAt) {
        heartbeatAt = observedAt
      },
      onReady() {},
      onEvent() {},
      onReset() {},
      onError() {
        errors += 1
      }
    })

    assert.equal(FakeEventSource.instances[0].url, '/api/v1/runtime/events')
    assert.equal(FakeEventSource.instances[0].withCredentials, true)
    FakeEventSource.instances[0].emit('ready', '{"newest_id":7}')
    FakeEventSource.instances[0].emit(
      'heartbeat',
      '{"at":"2026-07-28T07:30:00Z"}'
    )
    assert.equal(heartbeatAt, '2026-07-28T07:30:00Z')
    assert.equal(timers.size, 1)

    const [timerID, expire] = timers.entries().next().value
    timers.delete(timerID)
    expire()
    assert.equal(errors, 1)
    assert.equal(FakeEventSource.instances.length, 2)
    assert.equal(FakeEventSource.instances[0].closed, true)
    assert.equal(
      FakeEventSource.instances[1].url,
      '/api/v1/runtime/events?after=7'
    )

    FakeEventSource.instances[1].emit('runtime', '{')
    assert.equal(errors, 2)
    assert.equal(FakeEventSource.instances.length, 3)
    assert.equal(FakeEventSource.instances[1].closed, true)
    assert.equal(
      FakeEventSource.instances[2].url,
      '/api/v1/runtime/events?after=7'
    )

    close()
    assert.equal(FakeEventSource.instances[2].closed, true)
    assert.equal(timers.size, 0)
  } finally {
    globalThis.setTimeout = originalSetTimeout
    globalThis.clearTimeout = originalClearTimeout
    if (originalEventSource === undefined) {
      delete globalThis.EventSource
    } else {
      globalThis.EventSource = originalEventSource
    }
  }
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

  assert.match(client, /function subscribeEventSource\(/)
  assert.match(
    client,
    /subscribeEventSource\(\s*`\$\{API_ROOT\}\/runtime\/events`/
  )
  assert.match(client, /source\.addEventListener\('runtime'/)
  assert.match(client, /source\.addEventListener\('heartbeat'/)
  assert.match(client, /source\.addEventListener\('reset'/)
  assert.match(client, /RUNTIME_EVENT_INACTIVITY_TIMEOUT_MS = 12_000/)
  assert.match(client, /RUNTIME_RESOURCES[\s\S]*?'messages'/)
  assert.match(
    shell,
    /async function initializeWorkspaceRuntime[\s\S]*?await bootstrap\(\)[\s\S]*?initializeRuntimeEvents\(router\)/
  )
  assert.match(shell, /shutdownRuntimeEvents\(\)/)
  assert.match(runtime, /refreshDeviceWorkspace\(\)/)
  assert.match(runtime, /loadNetwork\(true, true\)/)
  assert.match(runtime, /onOpen:[\s\S]*?renewActiveCallLease\(\)/)
  assert.match(runtime, /onHeartbeat:[\s\S]*?renewActiveCallLease\(\)/)
  assert.match(runtime, /case 'calls':[\s\S]*?await requestActiveCallRefresh\(\)/)
  assert.match(runtime, /refreshCalls\(\)/)
  assert.match(
    runtime,
    /case 'messages':[\s\S]*?refreshMessageWorkspace\([\s\S]*?visibleMessageThreadKey\(router\.currentRoute\.value\)/
  )
  assert.match(
    runtime,
    /onReady:[\s\S]*?if \(!initialized\)[\s\S]*?refreshQueue\?\.enqueue\(ALL_RESOURCES\)/
  )
  assert.doesNotMatch(runtime, /let lastEventID/)
  assert.doesNotMatch(
    runtime,
    /onError:[\s\S]*?if \(wasConnected\)[\s\S]*?refreshQueue\?\.enqueue\(ALL_RESOURCES\)/
  )
})
