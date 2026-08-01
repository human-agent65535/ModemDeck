import assert from 'node:assert/strict'
import test from 'node:test'

import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { ApiError } from '../src/api/types.ts'
import {
  deviceConfigurationResource,
  loadDeviceConfiguration,
  restartModem,
  setRadioEnabled
} from '../src/state/deviceConfiguration.ts'
import { waitForUnavailableThenReadable } from '../src/state/deviceRecovery.ts'

function deterministicClock() {
  let current = 0
  return {
    now: () => current,
    wait: async milliseconds => {
      current += milliseconds
    }
  }
}

function deferred() {
  let resolve
  const promise = new Promise(done => {
    resolve = done
  })
  return { promise, resolve }
}

test('device recovery ignores the old readable object until an outage is observed', async () => {
  const unavailable = new Error('modem disappeared')
  const reads = [
    { revision: 'old-1' },
    { revision: 'old-2' },
    unavailable,
    { revision: 'new' }
  ]
  const clock = deterministicClock()

  const result = await waitForUnavailableThenReadable({
    read: async () => {
      const next = reads.shift()
      if (next instanceof Error) throw next
      return next
    },
    isCurrent: () => true,
    isUnavailable: error => error === unavailable,
    timeoutMs: 10_000,
    intervalMs: 1_000,
    ...clock
  })

  assert.deepEqual(result, { status: 'recovered', value: { revision: 'new' } })
  assert.equal(reads.length, 0)
})

test('device recovery does not report success when the old object stays readable', async () => {
  const clock = deterministicClock()
  let reads = 0

  const result = await waitForUnavailableThenReadable({
    read: async () => ({ revision: `old-${++reads}` }),
    isCurrent: () => true,
    isUnavailable: () => false,
    timeoutMs: 3_000,
    intervalMs: 1_000,
    ...clock
  })

  assert.deepEqual(result, { status: 'timeout', observedUnavailable: false })
  assert.equal(reads, 3)
})

test('device recovery reports whether a timed-out probe actually observed the outage', async () => {
  const unavailable = new Error('modem disappeared')
  const clock = deterministicClock()

  const result = await waitForUnavailableThenReadable({
    read: async () => {
      throw unavailable
    },
    isCurrent: () => true,
    isUnavailable: error => error === unavailable,
    timeoutMs: 3_000,
    intervalMs: 1_000,
    ...clock
  })

  assert.deepEqual(result, { status: 'timeout', observedUnavailable: true })
})

test('device recovery stops without writing when a newer operation supersedes it', async () => {
  const unavailable = new Error('modem disappeared')
  const clock = deterministicClock()
  let current = true

  const result = await waitForUnavailableThenReadable({
    read: async () => {
      current = false
      throw unavailable
    },
    isCurrent: () => current,
    isUnavailable: error => error === unavailable,
    timeoutMs: 3_000,
    intervalMs: 1_000,
    ...clock
  })

  assert.deepEqual(result, { status: 'superseded' })
})

test('device recovery surfaces errors that are not evidence of an outage', async () => {
  const invalid = new Error('invalid response')
  const clock = deterministicClock()

  await assert.rejects(
    waitForUnavailableThenReadable({
      read: async () => {
        throw invalid
      },
      isCurrent: () => true,
      isUnavailable: () => false,
      timeoutMs: 3_000,
      intervalMs: 1_000,
      ...clock
    }),
    invalid
  )
})

test('modem restart ignores accepted and pre-restart snapshots until the modem disappears', async () => {
  const lineID = 'line-restart-integration'
  const fixture = createFixtureGateway()
  const initial = await fixture.getDeviceConfiguration('line-fixture-main')
  const recovered = structuredClone(initial)
  recovered.hardware.revision = 'revision-after-restart'
  recovered.hardware.volte.restart_required = false

  const target = deviceConfigurationResource(lineID)
  target.status = 'ready'
  target.data = structuredClone(initial)
  target.error = ''
  target.savingOperation = ''
  target.recovering = false

  const originalGet = gateway.getDeviceConfiguration
  const originalUpdate = gateway.updateDeviceConfiguration
  const originalSetTimeout = globalThis.setTimeout
  let reads = 0
  try {
    gateway.getDeviceConfiguration = async requestedLineID => {
      assert.equal(requestedLineID, lineID)
      reads += 1
      if (reads <= 2) return structuredClone(initial)
      if (reads === 3) {
        throw new ApiError('modem unavailable', 503, 'communications_unavailable')
      }
      return structuredClone(recovered)
    }
    gateway.updateDeviceConfiguration = async (requestedLineID, input) => {
      assert.equal(requestedLineID, lineID)
      assert.equal(input.operation, 'restart_modem')
      return structuredClone(initial)
    }
    globalThis.setTimeout = callback => {
      queueMicrotask(callback)
      return 1
    }

    assert.equal(await restartModem(lineID), true)
    assert.equal(reads, 4)
    assert.equal(target.data.hardware.revision, 'revision-after-restart')
    assert.equal(target.status, 'ready')
    assert.equal(target.error, '')
    assert.equal(target.recovering, false)
  } finally {
    gateway.getDeviceConfiguration = originalGet
    gateway.updateDeviceConfiguration = originalUpdate
    globalThis.setTimeout = originalSetTimeout
  }
})

test('a stale configuration load cannot replay over a completed hardware write', async () => {
  const lineID = 'line-generation-integration'
  const fixture = createFixtureGateway()
  const initial = await fixture.getDeviceConfiguration('line-fixture-main')
  const stale = structuredClone(initial)
  stale.hardware.revision = 'stale-load'
  const latest = structuredClone(initial)
  latest.hardware.revision = 'latest-before-write'
  const updated = structuredClone(initial)
  updated.hardware.revision = 'completed-write'
  updated.hardware.radio.enabled = false

  const target = deviceConfigurationResource(lineID)
  target.status = 'ready'
  target.data = structuredClone(initial)
  target.error = ''
  target.savingOperation = ''
  target.recovering = false

  const staleLoad = deferred()
  const originalGet = gateway.getDeviceConfiguration
  const originalUpdate = gateway.updateDeviceConfiguration
  let reads = 0
  try {
    gateway.getDeviceConfiguration = async () => {
      reads += 1
      if (reads === 1) return staleLoad.promise
      return structuredClone(latest)
    }
    gateway.updateDeviceConfiguration = async (_requestedLineID, input) => {
      assert.equal(input.expected_device_revision, 'latest-before-write')
      return { hardware: structuredClone(updated.hardware) }
    }

    const loading = loadDeviceConfiguration(lineID, true)
    const writing = setRadioEnabled(lineID, false)
    assert.equal(await writing, true)
    staleLoad.resolve(stale)
    assert.equal(await loading, false)

    assert.equal(target.data.hardware.revision, 'completed-write')
    assert.equal(target.data.hardware.radio.enabled, false)
  } finally {
    gateway.getDeviceConfiguration = originalGet
    gateway.updateDeviceConfiguration = originalUpdate
  }
})
