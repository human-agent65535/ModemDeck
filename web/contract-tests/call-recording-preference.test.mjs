import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway } from '../src/api/client.ts'
import { callState, dial, shutdownCallRuntime } from '../src/state/call.ts'
import { bootstrapResource } from '../src/state/workspace.ts'

function callResponse(id, lineKey, number) {
  return {
    call: {
      id,
      line_key: lineKey,
      direction: 'outgoing',
      remote_number: number,
      phase: 'dialing',
      media_available: false,
      created_at: '2026-07-23T12:00:00Z'
    }
  }
}

test('real call requests keep recording preferences local to each request', async () => {
  const originalFetch = globalThis.fetch
  const bodies = []
  let failNext = false

  globalThis.fetch = async (_input, init) => {
    bodies.push(JSON.parse(String(init?.body)))
    if (failNext) {
      failNext = false
      return new Response(
        JSON.stringify({ code: 'forced_failure', message: 'forced failure' }),
        {
          status: 500,
          headers: { 'Content-Type': 'application/json' }
        }
      )
    }
    const body = bodies.at(-1)
    return new Response(
      JSON.stringify(callResponse(`call-${bodies.length}`, body.line_id, body.number)),
      {
        status: 201,
        headers: { 'Content-Type': 'application/json' }
      }
    )
  }

  try {
    await gateway.startCall('line-main', '+818000000001', true)
    await gateway.startCall('line-main', '+818000000002', false)
    failNext = true
    await assert.rejects(
      () => gateway.startCall('line-main', '+818000000003', true),
      /forced failure/
    )
    await gateway.startCall('line-main', '+818000000004')
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(
    bodies.map(body => body.recording_enabled),
    [true, false, true, undefined]
  )
  assert.equal('recording_enabled' in bodies[3], false)
})

test('dial forwards the current recording preference and keeps one call in flight', async () => {
  const originalStartCall = gateway.startCall
  const originalBootstrap = bootstrapResource.data
  const originalBootstrapStatus = bootstrapResource.status
  const originalBootstrapError = bootstrapResource.error
  const received = []

  bootstrapResource.data = {
    capabilities: {
      agent_connected: true,
      dial: true,
      message: true,
      webrtc_audio: false,
      device_control: false,
      volte_control: false,
      vowifi_control: false
    },
    lines: []
  }
  bootstrapResource.status = 'ready'
  bootstrapResource.error = ''
  gateway.startCall = async (lineKey, number, recordingEnabled) => {
    received.push({ lineKey, number, recordingEnabled })
    return callResponse(`state-call-${received.length}`, lineKey, number).call
  }

  try {
    assert.equal(await dial('+818000000011', 'line-main', true), true)
    shutdownCallRuntime()
    assert.equal(await dial('+818000000012', 'line-main', false), true)
    shutdownCallRuntime()

    let releasePending
    gateway.startCall = (lineKey, number, recordingEnabled) => {
      received.push({ lineKey, number, recordingEnabled })
      return new Promise(resolve => {
        releasePending = () =>
          resolve(callResponse(`state-call-${received.length}`, lineKey, number).call)
      })
    }
    const pending = dial('+818000000013', 'line-main', true)
    assert.equal(callState.busy, true)
    assert.equal(await dial('+818000000014', 'line-main', false), false)
    assert.equal(callState.errorStatus, 409)
    assert.equal(typeof releasePending, 'function')
    releasePending()
    assert.equal(await pending, true)
  } finally {
    shutdownCallRuntime()
    gateway.startCall = originalStartCall
    bootstrapResource.data = originalBootstrap
    bootstrapResource.status = originalBootstrapStatus
    bootstrapResource.error = originalBootstrapError
    callState.session = null
  }

  assert.deepEqual(
    received.map(request => request.recordingEnabled),
    [true, false, true]
  )
})
