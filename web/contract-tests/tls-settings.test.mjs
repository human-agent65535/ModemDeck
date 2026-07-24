import assert from 'node:assert/strict'
import test from 'node:test'
import { gateway, setClientCSRFToken } from '../src/api/client.ts'
import {
  createTLSSettingsPayload,
  parseTLSSettingsResponse,
  tlsCAPath,
  tlsSettingsContract,
  tlsSettingsPath
} from '../src/api/contract.ts'

const canonicalTLS = {
  mode: 'user',
  subject: 'CN=modemdeck.example',
  issuer: 'CN=Example Internal CA',
  dns_names: ['modemdeck.example', 'gateway.modemdeck.example'],
  ip_addresses: ['192.0.2.10', '2001:db8::10'],
  not_before: '2026-07-01T00:00:00Z',
  not_after: '2027-07-01T00:00:00Z',
  fingerprint_sha256:
    '28:7A:58:03:5A:96:3E:CB:5A:FA:8A:71:F9:21:BF:32:58:D4:98:B5:E1:A9:81:D5:0B:7A:A0:DB:10:72:C2:09',
  expired: false,
  renews_automatically: false
}

const certificatePEM = [
  '-----BEGIN CERTIFICATE-----',
  'fixture-certificate',
  '-----END CERTIFICATE-----',
  ''
].join('\n')
const privateKeyPEM = [
  '-----BEGIN PRIVATE KEY-----',
  'fixture-private-key',
  '-----END PRIVATE KEY-----',
  ''
].join('\n')

test('TLS settings endpoint uses the fixed GET and PUT contract', () => {
  assert.equal(tlsSettingsPath, '/api/v1/settings/tls')
  assert.equal(tlsCAPath, '/api/v1/settings/tls/ca')
  assert.deepEqual(tlsSettingsContract, {
    get: {
      method: 'GET',
      path: '/api/v1/settings/tls',
      successStatus: 200
    },
    update: {
      method: 'PUT',
      path: '/api/v1/settings/tls',
      successStatus: 200
    },
    downloadCA: {
      method: 'GET',
      path: '/api/v1/settings/tls/ca',
      successStatus: 200
    }
  })
})

test('TLS settings payloads keep only fields for the selected operation', () => {
  assert.deepEqual(
    createTLSSettingsPayload({
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    }),
    {
      operation: 'install_user',
      certificate_pem: certificatePEM,
      private_key_pem: privateKeyPEM
    }
  )
  assert.deepEqual(
    createTLSSettingsPayload({
      operation: 'use_automatic',
      certificate_pem: 'ignored'
    }),
    { operation: 'use_automatic' }
  )
  assert.throws(
    () =>
      createTLSSettingsPayload({
        operation: 'install_user',
        certificate_pem: ' ',
        private_key_pem: privateKeyPEM
      }),
    /certificate_pem/
  )
  assert.throws(
    () =>
      createTLSSettingsPayload({
        operation: 'install_user',
        certificate_pem: certificatePEM,
        private_key_pem: ''
      }),
    /private_key_pem/
  )
})

test('TLS settings parser requires the complete typed envelope', () => {
  assert.deepEqual(parseTLSSettingsResponse({ tls: canonicalTLS }), canonicalTLS)
  assert.throws(
    () => parseTLSSettingsResponse(canonicalTLS),
    /tls_settings_response\.tls/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, mode: 'managed' }
      }),
    /mode/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, dns_names: 'modemdeck.example' }
      }),
    /dns_names/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, not_after: 'not-a-time' }
      }),
    /not_after/
  )
  assert.throws(
    () =>
      parseTLSSettingsResponse({
        tls: { ...canonicalTLS, renews_automatically: null }
      }),
    /renews_automatically/
  )
})

test('gateway sends TLS requests with the contract path, method, payload, and CSRF', async () => {
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async (input, init = {}) => {
    requests.push({
      path: String(input),
      method: init.method,
      headers: new Headers(init.headers),
      body: init.body === undefined ? undefined : JSON.parse(String(init.body))
    })
    return new Response(JSON.stringify({ tls: canonicalTLS }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    })
  }
  setClientCSRFToken('tls-test-csrf')

  try {
    assert.deepEqual(await gateway.getTLSSettings(), canonicalTLS)
    assert.deepEqual(
      await gateway.updateTLSSettings({
        operation: 'install_user',
        certificate_pem: certificatePEM,
        private_key_pem: privateKeyPEM
      }),
      canonicalTLS
    )
    assert.deepEqual(
      await gateway.updateTLSSettings({ operation: 'use_automatic' }),
      canonicalTLS
    )
  } finally {
    setClientCSRFToken()
    globalThis.fetch = originalFetch
  }

  assert.deepEqual(
    requests.map(request => [request.path, request.method]),
    [
      ['/api/v1/settings/tls', 'GET'],
      ['/api/v1/settings/tls', 'PUT'],
      ['/api/v1/settings/tls', 'PUT']
    ]
  )
  assert.equal(requests[0].headers.get('X-ModemDeck-CSRF'), null)
  assert.equal(requests[1].headers.get('X-ModemDeck-CSRF'), 'tls-test-csrf')
  assert.deepEqual(requests[1].body, {
    operation: 'install_user',
    certificate_pem: certificatePEM,
    private_key_pem: privateKeyPEM
  })
  assert.deepEqual(requests[2].body, { operation: 'use_automatic' })
})
