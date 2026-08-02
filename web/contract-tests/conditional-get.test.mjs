import assert from 'node:assert/strict'
import test from 'node:test'

import {
  gateway,
  rotateAuthenticationRequestScope
} from '../src/api/client.ts'

const emptyContacts = {
  contacts: [],
  meta: { limit: 50, next_cursor: '', has_more: false }
}

test('conditional collection GET reuses only the current authentication memory cache', async () => {
  const originalFetch = globalThis.fetch
  const requests = []

  try {
    globalThis.fetch = async (input, init) => {
      const headers = new Headers(init?.headers)
      requests.push({ input: String(input), headers })
      if (requests.length === 2) {
        assert.equal(headers.get('If-None-Match'), 'W/"contacts-1"')
        return new Response(null, {
          status: 304,
          headers: { ETag: 'W/"contacts-1"' }
        })
      }
      assert.equal(headers.get('If-None-Match'), null)
      return new Response(JSON.stringify(emptyContacts), {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          ETag: requests.length === 1 ? 'W/"contacts-1"' : 'W/"contacts-2"'
        }
      })
    }

    assert.deepEqual((await gateway.listContacts()).items, [])
    assert.deepEqual((await gateway.listContacts()).items, [])

    rotateAuthenticationRequestScope()
    assert.deepEqual((await gateway.listContacts()).items, [])
    assert.equal(requests.length, 3)
  } finally {
    globalThis.fetch = originalFetch
    rotateAuthenticationRequestScope()
  }
})
