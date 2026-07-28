import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('browser call ownership follows the existing runtime SSE connection', async () => {
  const callState = await readFile(
    new URL('../src/state/call.ts', import.meta.url),
    'utf8'
  )
  const runtimeEvents = await readFile(
    new URL('../src/state/runtimeEvents.ts', import.meta.url),
    'utf8'
  )
  const callMedia = await readFile(
    new URL('../src/state/callMedia.ts', import.meta.url),
    'utf8'
  )
  const client = await readFile(
    new URL('../src/api/client.ts', import.meta.url),
    'utf8'
  )
  const serverEvents = await readFile(
    new URL('../../internal/httpapi/runtime_events.go', import.meta.url),
    'utf8'
  )

  assert.match(
    callState,
    /LEASED_PHASES[\s\S]*?'dialing'[\s\S]*?'ringing'[\s\S]*?'connecting'[\s\S]*?'active'/
  )
  assert.match(
    callState,
    /session\.phase !== 'active' \|\| !session\.media_available[\s\S]*?LEASED_MEDIA_STATES\.has\(callMediaState\.status\)/
  )
  assert.match(callState, /gateway[\s\S]*?\.renewCallLease\(callID\)/)
  assert.doesNotMatch(callState, /LEASED_MEDIA_STATES[\s\S]{0,100}'recovering'/)
  assert.match(
    callState,
    /callMediaState\.status[\s\S]*?LEASED_MEDIA_STATES\.has\(status\)[\s\S]*?renewActiveCallLease\(\)/
  )
  assert.doesNotMatch(
    callState,
    /(?:setInterval|setTimeout)\([^)]*renewActiveCallLease/
  )
  assert.match(runtimeEvents, /onOpen:[\s\S]*?renewActiveCallLease\(\)/)
  assert.match(runtimeEvents, /onHeartbeat:[\s\S]*?renewActiveCallLease\(\)/)
  assert.match(
    callMedia,
    /connection\.connectionState === 'disconnected'\) \{[\s\S]*?callMediaState\.status = 'recovering'/
  )
  assert.match(client, /CALL_LEASE_REQUEST_TIMEOUT_MS\s*=\s*4_000/)
  assert.match(
    client,
    /createCallLeasePayload\(callLeaseHolderID\)[\s\S]*?CALL_LEASE_REQUEST_TIMEOUT_MS/
  )
  assert.match(serverEvents, /runtimeHeartbeatInterval\s*=\s*5\s*\*\s*time\.Second/)
})
