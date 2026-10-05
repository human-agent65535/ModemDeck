import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callMedia = await readFile(
  new URL('../src/state/callMedia.ts', import.meta.url),
  'utf8'
)
const session = await readFile(
  new URL('../src/state/session.ts', import.meta.url),
  'utf8'
)
const client = await readFile(
  new URL('../src/api/client.ts', import.meta.url),
  'utf8'
)

test('one tab exclusively owns browser call media', () => {
  assert.match(callMedia, /const MEDIA_LOCK_PREFIX = 'modemdeck-call-media:'/)
  assert.match(
    callMedia,
    /navigator\.locks\.request\([\s\S]*?\{ mode: 'exclusive', signal: controller\.signal \}/
  )
  assert.match(callMedia, /globalThis\.crypto\.randomUUID\(\)/)
  assert.match(
    callMedia,
    /connection\.send\(JSON\.stringify\(\{ type: \'start\',[\s\S]*?owner_token: ownership\.ownerToken/
  )
  assert.match(
    callMedia,
    /gateway[\s\S]*?\.releaseCallMedia\(callID, ownership\.ownerToken\)/
  )
})

test('browser socket recovery has one fixed deadline and owner-scoped cleanup', () => {
  assert.match(callMedia, /const MEDIA_RECOVERY_TIMEOUT_MS = 15_000/)
  assert.match(callMedia, /if \(recoveryTimeoutID !== undefined\) return/)
  assert.match(callMedia, /function recoverConnection[\s\S]*?clearSocketResources\(\)[\s\S]*?beginRecoveryWindow\(callID, token\)[\s\S]*?callMediaState.status = 'recovering'/)
  assert.match(callMedia, /releaseCallMedia\(callID, ownership.ownerToken\)[\s\S]*?isCurrent\(callID, token\)[\s\S]*?openAudioSocket/)
  assert.match(callMedia, /function settleMediaRecovery[\s\S]*?runtime.sentFrames < MEDIA_STABLE_FRAMES[\s\S]*?runtime.receivedFrames < MEDIA_STABLE_FRAMES[\s\S]*?clearRecoveryWindow\(\)/)
  assert.match(callMedia, /event.code === 1008[\s\S]*?failConnection/)
  assert.doesNotMatch(callMedia, /RTCPeerConnection|exchangeCallMedia|getCallMediaICEConfiguration/)
})

test('session changes release media before credentials are revoked', () => {
  assert.match(
    callMedia,
    /releaseCallMediaForSessionEnd[\s\S]*?shutdownCallMedia\(\)[\s\S]*?ownership\?\.claimed[\s\S]*?await ownership\.cleanup/
  )
  assert.match(
    session,
    /async function terminateSession[\s\S]*?await releaseCallMediaForSessionEnd\(\)[\s\S]*?await operation\(\)[\s\S]*?clearSession\(\)/
  )
  assert.match(
    session,
    /logout[\s\S]*?terminateSession\(\(\) => gateway\.logout\(\)\)/
  )
  assert.match(
    session,
    /changePassword[\s\S]*?terminateSession\(\(\) => gateway\.changePassword\(input\)\)/
  )
  assert.doesNotMatch(client, /callLeaseHolderID|rotateCallLeaseHolder|holder_id/)
})
