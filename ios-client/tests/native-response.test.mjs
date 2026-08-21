import assert from 'node:assert/strict'
import test from 'node:test'

import { nativeResponseBody } from '../assets/nativeResponse.ts'

test('native conditional responses use a null Fetch body', () => {
  const body = nativeResponseBody(304, 'GET', '')

  assert.equal(body, null)
  assert.doesNotThrow(() => new Response(body, { status: 304 }))
})

test('native no-content and HEAD responses use a null Fetch body', () => {
  for (const [status, method] of [
    [204, 'DELETE'],
    [205, 'POST'],
    [200, 'HEAD']
  ]) {
    assert.equal(nativeResponseBody(status, method, ''), null)
  }
})

test('native content responses preserve text and binary bodies', () => {
  const textBody = '{"ok":true}'
  const binaryBody = new Uint8Array([1, 2, 3]).buffer

  assert.equal(nativeResponseBody(200, 'GET', textBody), textBody)
  assert.equal(nativeResponseBody(206, 'GET', binaryBody), binaryBody)
})
