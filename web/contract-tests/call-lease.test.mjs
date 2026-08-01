import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

test('call ownership uses independent control and server media liveness', async () => {
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
  assert.match(callState, /CALL_LEASE_HEARTBEAT_MS\s*=\s*5_000/)
  assert.match(callState, /gateway[\s\S]*?\.renewCallLease\(callID\)/)
  assert.match(
    callState,
    /callLeaseHeartbeatTimer\s*=\s*window\.setInterval\([\s\S]*?renewActiveCallLease\(\)[\s\S]*?CALL_LEASE_HEARTBEAT_MS/
  )
  assert.match(
    callState,
    /window\.addEventListener\('online', resumeCallRuntime\)[\s\S]*?window\.addEventListener\('pageshow', resumeCallRuntime\)[\s\S]*?document\.addEventListener\('visibilitychange', resumeVisibleCallRuntime\)/
  )
  assert.match(
    callState,
    /function resumeCallRuntime\(\)[\s\S]*?renewActiveCallLease\(\)[\s\S]*?requestActiveCallRefresh\(\)/
  )
  assert.doesNotMatch(
    callState,
    /LEASED_MEDIA_STATES|callMediaState\.status/
  )
  assert.doesNotMatch(runtimeEvents, /renewActiveCallLease/)
  assert.match(runtimeEvents, /onOpen:[\s\S]*?enqueue\(\['calls'\]\)/)
  assert.match(runtimeEvents, /onHeartbeat:[\s\S]*?lastHeartbeatAt/)
  assert.match(
    callMedia,
    /connection\.connectionState === 'disconnected'\) \{[\s\S]*?callMediaState\.status = 'recovering'/
  )
  assert.match(
    callState,
    /retryActiveCallMedia[\s\S]*?await gateway\.renewCallLease\(session\.id\)[\s\S]*?retryCallMedia\(session\)/
  )
  assert.match(client, /CALL_LEASE_REQUEST_TIMEOUT_MS\s*=\s*4_000/)
  assert.match(
    client,
    /createCallLeasePayload\(\)[\s\S]*?CALL_LEASE_REQUEST_TIMEOUT_MS/
  )
  assert.match(
    client,
    /get\(communicationContracts\.activeCalls\.path\)/
  )
  assert.doesNotMatch(client, /callLeaseHolderID|holder_id/)
  assert.match(callState, /const owned = session\.control_state === 'owned'/)
	assert.match(
		callState,
		/catch \(error\) \{[\s\S]*?runtime\.dialFailed[\s\S]*?requestActiveCallRefresh\(\)/
	)
	assert.match(
		callState,
		/async function act[\s\S]*?catch \(error\) \{[\s\S]*?runtime\.callActionFailed[\s\S]*?requestActiveCallRefresh\(\)/
	)
  assert.match(
    callState,
    /session\.control_state === 'available'[\s\S]*?syncCallMedia\(owned \? session : null\)/
  )
  assert.match(serverEvents, /runtimeHeartbeatInterval\s*=\s*5\s*\*\s*time\.Second/)
})

test('occupied calls are visible but cannot consume browser controls or media', async () => {
  const surface = await readFile(
    new URL('../src/components/CallSurface.vue', import.meta.url),
    'utf8'
  )
  assert.match(
    surface,
    /session\.value\?\.control_state === 'occupied'[\s\S]*?calls\.answeredElsewhere/
  )
  assert.match(surface, /v-else-if="canHangup && callState\.owned"/)
  assert.match(surface, /active\.value &&[\s\S]*?callState\.owned/)
})

test('line cards expose authoritative call and reservation occupancy', async () => {
  const moduleCard = await readFile(
    new URL('../src/components/ModuleCard.vue', import.meta.url),
    'utf8'
  )
  assert.match(
    moduleCard,
    /lineIsOccupied\(lineKey\(props\.line\)\)/
  )
  assert.match(moduleCard, /v-if="!recovering && lineBusy"[\s\S]*?calls\.lineInUse/)
})
