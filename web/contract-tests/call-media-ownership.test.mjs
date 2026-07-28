import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const callMedia = await readFile(
  new URL('../src/state/callMedia.ts', import.meta.url),
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
    /gateway\.exchangeCallMedia\([\s\S]*?ownership\.ownerToken[\s\S]*?offerSDP/
  )
  assert.match(
    callMedia,
    /gateway\.releaseCallMedia\(callID, ownership\.ownerToken\)/
  )
})

test('browser media uses one fixed recovery window', () => {
  assert.match(callMedia, /const MEDIA_RECOVERY_TIMEOUT_MS = 15_000/)
  assert.match(
    callMedia,
    /connection\.connectionState === 'disconnected'[\s\S]*?beginRecoveryWindow\(callID, token\)/
  )
  assert.match(
    callMedia,
    /connection\.connectionState === 'failed'[\s\S]*?beginRecoveryWindow\(callID, token\)/
  )
  assert.match(
    callMedia,
    /connection\.connectionState === 'connected'[\s\S]*?clearRecoveryWindow\(\)/
  )
  assert.match(
    callMedia,
    /connection\.connectionState === 'closed'[\s\S]*?failConnection\(/
  )
  assert.doesNotMatch(callMedia, /restartIce|iceRestart/)
})
