import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
import {
  acceptRuntimeActiveCalls,
  callState,
  initializeCallRuntime,
  shutdownCallRuntime
} from '../src/state/call.ts'
import {
  acceptNetworkSnapshot,
  loadNetwork,
  networkState,
  resetNetworkState
} from '../src/state/network.ts'
import {
  acceptRuntimeState,
  shutdownRuntimeEvents
} from '../src/state/runtimeEvents.ts'

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

test('runtime SSE parses current state and reconnects without replay cursors', () => {
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
    globalThis.setTimeout = (callback, delay) => {
      nextTimerID += 1
      timers.set(nextTimerID, { callback, delay })
      return nextTimerID
    }
    globalThis.clearTimeout = timerID => {
      timers.delete(timerID)
    }
    let errors = 0
    let heartbeatAt = ''
    let state
    const close = gateway.subscribeRuntimeEvents({
      onOpen() {},
      onHeartbeat(observedAt) {
        heartbeatAt = observedAt
      },
      onState(value) {
        state = value
      },
      onUpdateOperation() {},
      onError() {
        errors += 1
      }
    })

    assert.equal(FakeEventSource.instances[0].url, '/api/v1/runtime/events')
    assert.equal(FakeEventSource.instances[0].withCredentials, true)
    assert.equal(FakeEventSource.instances[0].listeners.has('ready'), false)
    assert.equal(FakeEventSource.instances[0].listeners.has('reset'), false)
    assert.equal(FakeEventSource.instances[0].listeners.has('runtime'), false)
    FakeEventSource.instances[0].emit(
      'state',
      JSON.stringify({
        epoch: 'process-a',
        revision: 7,
        data_revision: 3,
        observed_at: '2026-08-02T07:29:57Z',
        communication: {
          capabilities: {
            agent_connected: false,
            dial: false,
            message: false,
            webrtc_audio: false,
            device_control: false,
            volte_control: false,
            vowifi_control: false,
            unavailable_reasons: {}
          },
          lines: [],
          line_catalog: []
        }
      })
    )
    FakeEventSource.instances[0].emit(
      'state',
      JSON.stringify({
        epoch: 'process-a',
        revision: 8,
        data_revision: 3,
        observed_at: '2026-08-02T07:29:58Z',
        calls: { calls: [], reservations: [] },
        recordings: []
      })
    )
    FakeEventSource.instances[0].emit(
      'state',
      JSON.stringify({
        epoch: 'process-a',
        revision: 9,
        data_revision: 4,
        observed_at: '2026-08-02T07:29:59Z'
      })
    )
    FakeEventSource.instances[0].emit(
      'heartbeat',
      '{"at":"2026-08-02T07:30:00Z"}'
    )
    assert.equal(state, undefined)
    assert.equal(heartbeatAt, '2026-08-02T07:30:00Z')
    assert.equal(timers.size, 2)

    const [stateTimerID, stateTimer] = [...timers.entries()].find(
      ([, timer]) => timer.delay === 50
    )
    timers.delete(stateTimerID)
    stateTimer.callback()
    assert.deepEqual(state, {
      epoch: 'process-a',
      revision: 9,
      data_revision: 4,
      observed_at: '2026-08-02T07:29:59Z',
      communication: {
        capabilities: {
          agent_connected: false,
          dial: false,
          message: false,
          webrtc_audio: false,
          device_control: false,
          volte_control: false,
          vowifi_control: false,
          unavailable_reasons: { dial: undefined, message: undefined }
        },
        lines: [],
        line_catalog: []
      },
      calls: { calls: [], reservations: [] },
      recordings: []
    })
    assert.equal(timers.size, 1)

    const [timerID, expire] = timers.entries().next().value
    timers.delete(timerID)
    expire.callback()
    assert.equal(errors, 1)
    assert.equal(FakeEventSource.instances.length, 2)
    assert.equal(FakeEventSource.instances[0].closed, true)
    assert.equal(FakeEventSource.instances[1].url, '/api/v1/runtime/events')

    FakeEventSource.instances[1].emit('state', '{')
    assert.equal(errors, 2)
    assert.equal(FakeEventSource.instances.length, 3)
    assert.equal(FakeEventSource.instances[1].closed, true)
    assert.equal(FakeEventSource.instances[2].url, '/api/v1/runtime/events')

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

test('live signal and network changes apply directly without REST request storms', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async input => {
    requests.push(String(input))
    return new Response('{"version":"test"}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }
  const capabilities = {
    agent_connected: true,
    dial: true,
    message: true,
    webrtc_audio: true,
    device_control: true,
    volte_control: true,
    vowifi_control: false,
    unavailable_reasons: {}
  }
  const networkStatus = sample => ({
    available: true,
    state: 'available',
    boot_epoch: 'boot-a',
    observed_at: `2026-08-02T07:30:${String(sample).padStart(2, '0')}Z`,
    lines: [],
    proxies: [],
    today_total: { rx_bytes: sample, tx_bytes: sample },
    today_usage: [],
    month_total: { rx_bytes: sample, tx_bytes: sample },
    month_usage: [],
    stale: false,
    apply_pending: false,
    apply_status: 'applied',
    apply_attempts: 0,
    apply_exhausted: false
  })

  try {
    shutdownRuntimeEvents()
    for (let revision = 0; revision < 40; revision += 1) {
      acceptRuntimeState({
          epoch: 'process-a',
          revision,
          data_revision: 0,
          observed_at: `2026-08-02T07:30:${String(revision).padStart(2, '0')}Z`,
          communication: {
            capabilities,
            lines: [{ id: 'line-1', signal_quality: revision }],
            line_catalog: [{ id: 'line-1', signal_quality: revision }],
            devices: []
          },
          network: {
            status: networkStatus(revision),
            proxies: [{ id: 'proxy-1', revision: 1 }]
          }
        })
    }
    await Promise.resolve()
    await Promise.resolve()

    assert.equal(networkState.snapshot?.today_total.rx_bytes, 39)
    assert.equal(networkState.proxies[0]?.id, 'proxy-1')
    assert.deepEqual(
      requests.filter(url =>
        /\/api\/v1\/(bootstrap|devices|network|proxies|calls\/active)/.test(url)
      ),
      []
    )
  } finally {
    shutdownRuntimeEvents()
    globalThis.fetch = originalFetch
  }
})

test('runtime proxy truth supersedes an older in-flight REST read', async () => {
  const originalStatus = gateway.getNetworkStatus
  const originalProxies = gateway.listProxies
  let resolveStatus
  let resolveProxies
  const pendingStatus = new Promise(resolve => { resolveStatus = resolve })
  const pendingProxies = new Promise(resolve => { resolveProxies = resolve })
  const observedAt = '2026-08-02T07:30:00Z'
  const status = {
    available: true,
    state: 'available',
    boot_epoch: 'boot-a',
    observed_at: observedAt,
    lines: [],
    proxies: [],
    today_total: { rx_bytes: 0, tx_bytes: 0 },
    today_usage: [],
    month_total: { rx_bytes: 0, tx_bytes: 0 },
    month_usage: [],
    stale: false,
    apply_pending: false,
    apply_status: 'applied',
    apply_attempts: 0,
    apply_exhausted: false
  }

  try {
    resetNetworkState()
    gateway.getNetworkStatus = () => pendingStatus
    gateway.listProxies = () => pendingProxies
    const oldRead = loadNetwork(true)
    acceptNetworkSnapshot(status, [{ id: 'proxy-1', revision: 2 }], true)
    resolveStatus(status)
    resolveProxies([{ id: 'proxy-1', revision: 1 }])
    await oldRead

    assert.equal(networkState.proxies[0]?.revision, 2)
  } finally {
    gateway.getNetworkStatus = originalStatus
    gateway.listProxies = originalProxies
    resetNetworkState()
  }
})

test('runtime call truth supersedes an older in-flight REST read', async () => {
  const originalSnapshot = gateway.getActiveCallSnapshot
  let resolveSnapshot
  const pendingSnapshot = new Promise(resolve => { resolveSnapshot = resolve })

  try {
    shutdownCallRuntime()
    gateway.getActiveCallSnapshot = () => pendingSnapshot
    initializeCallRuntime()
    acceptRuntimeActiveCalls({ calls: [], reservations: [] })
    resolveSnapshot({
      calls: [{
        id: 'stale-call',
        line_id: 'line-1',
        direction: 'incoming',
        remote_number: '+818012345678',
        phase: 'ringing',
        control_state: 'available',
        media_available: false,
        created_at: '2026-08-02T07:30:00Z'
      }],
      reservations: []
    })
    await pendingSnapshot
    await Promise.resolve()
    await Promise.resolve()

    assert.deepEqual(callState.sessions, [])
  } finally {
    gateway.getActiveCallSnapshot = originalSnapshot
    shutdownCallRuntime()
  }
})

test('runtime state is global to the authenticated application shell', async () => {
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
    /subscribeEventSource\(\s*`\$\{API_ROOT\}\/runtime\/events\$\{updateQuery\}`/
  )
  assert.match(client, /source\.addEventListener\('state'/)
  assert.match(client, /source\.addEventListener\('heartbeat'/)
  assert.match(client, /source\.addEventListener\('update'/)
  const runtimeSubscriptionStart = client.indexOf('subscribeRuntimeEvents(')
  const diagnosticSubscriptionStart = client.indexOf(
    'subscribeDiagnosticLogs(',
    runtimeSubscriptionStart
  )
  const runtimeSubscription = client.slice(
    runtimeSubscriptionStart,
    diagnosticSubscriptionStart
  )
  assert.doesNotMatch(runtimeSubscription, /addEventListener\('runtime'/)
  assert.doesNotMatch(runtimeSubscription, /addEventListener\('reset'/)
  assert.doesNotMatch(client, /RUNTIME_RESOURCES/)
  assert.match(client, /RUNTIME_EVENT_INACTIVITY_TIMEOUT_MS = 12_000/)
  assert.match(
    shell,
    /async function initializeWorkspaceRuntime[\s\S]*?await bootstrap\(\)[\s\S]*?initializeRuntimeEvents\(\)/
  )
  assert.match(shell, /shutdownRuntimeEvents\(\)/)
  assert.match(runtime, /acceptRuntimeCommunicationState\(runtime\.communication\)/)
  assert.match(
    runtime,
    /acceptNetworkSnapshot\([\s\S]*?runtime\.network\.status,[\s\S]*?runtime\.network\.proxies,[\s\S]*?true/
  )
  assert.match(runtime, /acceptRuntimeActiveCalls\(runtime\.calls\)/)
  assert.match(runtime, /state\.dataWatermark = `\$\{runtime\.epoch\}:\$\{runtime\.data_revision\}`/)
  assert.match(
    runtime,
    /setApplicationUpdateNoticeSuppressed\(Boolean\(normalizedOperationID\)\)/
  )
  assert.match(
    runtime,
    /operation\.state === 'succeeded' \|\| operation\.state === 'failed'/
  )
  assert.doesNotMatch(runtime, /requestDurableRefresh|Promise\.allSettled\(\[/)
  assert.doesNotMatch(runtime, /ALL_RESOURCES|RuntimeResource|FALLBACK_REFRESH_MS/)
  assert.doesNotMatch(runtime, /loadNetwork|requestActiveCallRefresh/)
})
